package app

import (
	"database/sql"
	"net/http"
	"time"
)

// Dashboard reviews freeze the current topic-wide error set, independently of
// the batches that originally recorded or inherited each answer.
func (app *App) startTopicErrorReview(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userContextKey).(user).ID
	topicID := r.PathValue("topicID")
	_, members, _, err := topicMembers(topicID)
	if err != nil {
		app.writeError(w, 404, "Тема не найдена")
		return
	}
	fullID, err := app.currentTopicRun(userID, topicID, members)
	if err != nil {
		app.serverError(w, err)
		return
	}
	tx, err := app.db.Begin()
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer tx.Rollback()
	// Resume an unfinished frozen session, including its already answered items.
	var id int64
	err = tx.QueryRow(`SELECT r.id FROM topic_runs r JOIN topic_error_reviews e ON e.run_id=r.id WHERE r.user_id=? AND r.topic_id=? AND r.completed_at IS NULL AND r.id>? ORDER BY r.id DESC LIMIT 1`, userID, topicID, fullID).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		app.serverError(w, err)
		return
	}
	if id == 0 {
		ids := []string{}
		for _, ref := range members {
			var wrong bool
			if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM question_current_progress WHERE user_id=? AND content_id=? AND correct=0)`, userID, ref.CanonicalID).Scan(&wrong); err != nil {
				app.serverError(w, err)
				return
			}
			if wrong {
				ids = append(ids, ref.CanonicalID)
			}
		}
		if len(ids) == 0 {
			app.writeError(w, 409, "В теме нет вопросов с ошибками")
			return
		}
		// Keep the existing one-child history constraint and all earlier reviews.
		parent := fullID
		for {
			var child int64
			err = tx.QueryRow(`SELECT id FROM topic_runs WHERE parent_id=?`, parent).Scan(&child)
			if err == sql.ErrNoRows {
				break
			}
			if err != nil {
				app.serverError(w, err)
				return
			}
			parent = child
		}
		result, err := tx.Exec(`INSERT INTO topic_runs(user_id,topic_id,parent_id,total,started_at) VALUES(?,?,?,?,?)`, userID, topicID, parent, len(ids), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			app.serverError(w, err)
			return
		}
		id, err = result.LastInsertId()
		if err != nil {
			app.serverError(w, err)
			return
		}
		if _, err = tx.Exec(`INSERT INTO topic_error_reviews VALUES(?)`, id); err != nil {
			app.serverError(w, err)
			return
		}
		for _, content := range ids {
			if _, err = tx.Exec(`INSERT INTO topic_run_members VALUES(?,?)`, id, content); err != nil {
				app.serverError(w, err)
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		app.serverError(w, err)
		return
	}
	app.writeJSON(w, 200, map[string]int64{"id": id})
}
