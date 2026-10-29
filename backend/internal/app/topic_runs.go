package app

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type topicRunSummary struct {
	Kind          string            `json:"kind"`
	ID            int64             `json:"id"`
	Total         int               `json:"total"`
	Answered      int               `json:"answered"`
	Correct       int               `json:"correct"`
	Wrong         int               `json:"wrong"`
	ReviewPending int               `json:"review_pending"`
	Inherited     int               `json:"inherited"`
	StartedAt     string            `json:"started_at"`
	CompletedAt   *string           `json:"completed_at"`
	Review        *topicRunSummary  `json:"review,omitempty"`
	ErrorReviews  []topicRunSummary `json:"error_reviews,omitempty"`
}

// The legacy current-answer snapshot remains intact; copy it once into history.
func ensureTopicRun(tx *sql.Tx, userID, topicID string, members []topicReference) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM topic_runs WHERE user_id=? AND topic_id=? AND parent_id IS NULL AND kind='full' ORDER BY id DESC LIMIT 1`, userID, topicID).Scan(&id)
	if err == nil {
		if err = snapshotLegacyMembers(tx, id, members); err != nil {
			return 0, err
		}
		id, err = refreshReorganizedRun(tx, userID, topicID, id, members)
		if err != nil {
			return 0, err
		}
		return id, inheritTopicProgress(tx, id)
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	result, err := tx.Exec(`INSERT INTO topic_runs(user_id,topic_id,total,started_at) VALUES(?,?,?,COALESCE((SELECT MIN(answered_at) FROM topic_answers WHERE user_id=? AND topic_id=?),?))`, userID, topicID, len(members), userID, topicID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(`INSERT INTO topic_run_answers SELECT ?,content_id,selected_text,correct,answered_at FROM topic_answers WHERE user_id=? AND topic_id=?`, id, userID, topicID)
	if err != nil {
		return 0, err
	}
	if err = saveRunMembers(tx, id, members); err != nil {
		return 0, err
	}
	return id, inheritTopicProgress(tx, id)
}
func completeTopicRun(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`UPDATE topic_runs SET completed_at=COALESCE(completed_at,CASE WHEN julianday((SELECT MAX(answered_at) FROM topic_run_progress WHERE run_id=?))<julianday(started_at) THEN started_at ELSE (SELECT MAX(answered_at) FROM topic_run_progress WHERE run_id=?) END) WHERE id=? AND total>0 AND total<=(SELECT COUNT(*) FROM topic_run_progress WHERE run_id=?)`, id, id, id, id)
	return err
}
func (app *App) currentTopicRun(userID, topicID string, members []topicReference) (int64, error) {
	tx, err := app.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := ensureTopicRun(tx, userID, topicID, members)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Match the original answer event, not just its question: later mistakes and
// fresh full repeats must still require their own review. Inherited copies of
// the same event are covered by a single review within this topic cycle.
const uncoveredTopicError = `NOT EXISTS (
 SELECT 1 FROM topic_runs review JOIN topic_run_answers fixed ON fixed.run_id=review.id
 WHERE review.user_id=r.user_id AND review.topic_id=r.topic_id AND review.parent_id IS NOT NULL
 AND fixed.content_id=a.content_id
 AND julianday(fixed.answered_at)>=julianday(a.answered_at)
 AND julianday(review.started_at)>=julianday(a.answered_at)
 AND (SELECT MAX(f.id) FROM topic_runs f WHERE f.user_id=r.user_id AND f.topic_id=r.topic_id AND f.parent_id IS NULL AND f.kind='full' AND f.id<=review.parent_id)
 = (SELECT MAX(f.id) FROM topic_runs f WHERE f.user_id=r.user_id AND f.topic_id=r.topic_id AND f.parent_id IS NULL AND f.kind='full' AND f.id<=r.id)
)`

func (app *App) topicHistory(userID, topicID string) ([]topicRunSummary, error) {
	rows, err := app.db.Query(`SELECT r.kind,r.id,r.parent_id,r.total,r.started_at,r.completed_at,COUNT(a.content_id),COALESCE(SUM(a.correct),0),COALESCE(SUM(a.inherited),0),EXISTS(SELECT 1 FROM topic_error_reviews e WHERE e.run_id=r.id),(SELECT COUNT(*) FROM topic_run_progress a WHERE a.run_id=r.id AND a.correct=0 AND `+uncoveredTopicError+`) FROM topic_runs r LEFT JOIN topic_run_progress a ON a.run_id=r.id WHERE r.user_id=? AND r.topic_id=? GROUP BY r.id ORDER BY r.id DESC`, userID, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []topicRunSummary{}
	reviews := map[int64]topicRunSummary{}
	parents := map[int64]int64{}
	errorReviews := []topicRunSummary{}
	for rows.Next() {
		var item topicRunSummary
		var parent sql.NullInt64
		var aggregate bool
		if err := rows.Scan(&item.Kind, &item.ID, &parent, &item.Total, &item.StartedAt, &item.CompletedAt, &item.Answered, &item.Correct, &item.Inherited, &aggregate, &item.ReviewPending); err != nil {
			return nil, err
		}
		item.Wrong = item.Answered - item.Correct
		if parent.Valid {
			parents[item.ID] = parent.Int64
			if aggregate {
				errorReviews = append(errorReviews, item)
				continue
			}
			reviews[parent.Int64] = item
		} else {
			runs = append(runs, item)
		}
	}
	for i := range runs {
		for _, review := range errorReviews {
			root := review.ID
			for parents[root] != 0 {
				root = parents[root]
			}
			if root == runs[i].ID {
				runs[i].ErrorReviews = append(runs[i].ErrorReviews, review)
			}
		}
		if review, ok := reviews[runs[i].ID]; ok {
			runs[i].Review = &review
		}
	}
	return runs, rows.Err()
}
func (app *App) startTopicReview(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userContextKey).(user).ID
	topicID := r.PathValue("topicID")
	parent, err := strconv.ParseInt(r.PathValue("runID"), 10, 64)
	if err != nil {
		app.writeError(w, 422, "Некорректное прохождение")
		return
	}
	tx, err := app.db.Begin()
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer tx.Rollback()
	var completed sql.NullString
	var wrong int
	err = tx.QueryRow(`SELECT completed_at,(SELECT COUNT(*) FROM topic_run_progress WHERE run_id=r.id AND correct=0) FROM topic_runs r WHERE id=? AND user_id=? AND topic_id=? AND parent_id IS NULL`, parent, userID, topicID).Scan(&completed, &wrong)
	if err == sql.ErrNoRows {
		app.writeError(w, 404, "Прохождение не найдено")
		return
	}
	if err != nil {
		app.serverError(w, err)
		return
	}
	if !completed.Valid || wrong == 0 {
		app.writeError(w, 409, "Работа над ошибками доступна после завершения темы с ошибками")
		return
	}
	var existing int64
	err = tx.QueryRow(`SELECT id FROM topic_runs WHERE parent_id=?`, parent).Scan(&existing)
	if err != nil && err != sql.ErrNoRows {
		app.serverError(w, err)
		return
	}
	if existing == 0 {
		if err = tx.QueryRow(`SELECT COUNT(*) FROM topic_runs r JOIN topic_run_progress a ON a.run_id=r.id WHERE r.id=? AND a.correct=0 AND `+uncoveredTopicError, parent).Scan(&wrong); err != nil {
			app.serverError(w, err)
			return
		}
		if wrong == 0 {
			app.writeError(w, 409, "Работа над этими ошибками уже выполнена")
			return
		}
	}
	_, err = tx.Exec(`INSERT INTO topic_runs(user_id,topic_id,parent_id,total,started_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, userID, topicID, parent, wrong, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		app.serverError(w, err)
		return
	}
	var id int64
	err = tx.QueryRow(`SELECT id FROM topic_runs WHERE parent_id=?`, parent).Scan(&id)
	if err != nil {
		app.serverError(w, err)
		return
	}
	if existing == 0 {
		_, err = tx.Exec(`INSERT INTO topic_run_members SELECT ?,a.content_id FROM topic_runs r JOIN topic_run_progress a ON a.run_id=r.id WHERE r.id=? AND a.correct=0 AND `+uncoveredTopicError, id, parent)
		if err != nil {
			app.serverError(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		app.serverError(w, err)
		return
	}
	app.writeJSON(w, 200, map[string]int64{"id": id})
}
func (app *App) reviewMembers(userID, topicID string, id int64, members []topicReference) ([]topicReference, error) {
	rows, err := app.db.Query(`SELECT m.content_id FROM topic_runs r JOIN topic_error_reviews e ON e.run_id=r.id JOIN topic_run_members m ON m.run_id=r.id WHERE r.id=? AND r.user_id=? AND r.topic_id=?
 UNION ALL
 SELECT a.content_id FROM topic_runs r JOIN topic_run_progress a ON a.run_id=r.parent_id WHERE r.id=? AND r.user_id=? AND r.topic_id=? AND NOT EXISTS(SELECT 1 FROM topic_error_reviews e WHERE e.run_id=r.id) AND a.correct=0
 AND (NOT EXISTS(SELECT 1 FROM topic_run_members WHERE run_id=r.id)
 OR EXISTS(SELECT 1 FROM topic_run_members m WHERE m.run_id=r.id AND m.content_id=a.content_id))`, id, userID, topicID, id, userID, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var content string
		if err = rows.Scan(&content); err != nil {
			return nil, err
		}
		ids[content] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("review not found")
	}
	return historicalTopicMembers(ids, members)
}
