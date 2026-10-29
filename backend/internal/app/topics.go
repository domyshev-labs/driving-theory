package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/domyshev-labs/driving-theory/backend/seed"
)

type topicReference struct {
	CanonicalID string   `json:"canonical_id"`
	QuestionRU  string   `json:"question_ru,omitempty"`
	Verified    bool     `json:"verified"`
	TopicIDs    []string `json:"topic_ids"`
}
type topicLibrary struct {
	Nodes []struct {
		ID     string  `json:"id"`
		Label  string  `json:"label_ru"`
		Parent *string `json:"parent_id"`
	} `json:"nodes"`
	Questions []topicReference `json:"questions"`
}
type topicAnswer struct {
	Letter  string `json:"letter"`
	Text    string `json:"text"`
	Correct bool   `json:"-"`
}
type topicProgress struct {
	SelectedLetter string `json:"selected_letter"`
	CorrectLetter  string `json:"correct_letter"`
	Correct        bool   `json:"correct"`
}
type topicQuestion struct {
	ID         string            `json:"id"`
	Number     int               `json:"number"`
	Question   string            `json:"question"`
	QuestionRU string            `json:"question_ru,omitempty"`
	Image      string            `json:"image_url"`
	Help       map[string]string `json:"help"`
	Answers    []topicAnswer     `json:"answers"`
	Progress   *topicProgress    `json:"progress,omitempty"`
}

func topicMembers(id string) (string, []topicReference, []topicReference, error) {
	data, err := seed.Files.ReadFile("library.json")
	if err != nil {
		return "", nil, nil, err
	}
	var library topicLibrary
	if err := json.Unmarshal(data, &library); err != nil {
		return "", nil, nil, err
	}
	title := ""
	for _, node := range library.Nodes {
		if node.ID == id {
			title = node.Label
		}
		if node.Parent != nil && *node.Parent == id {
			return "", nil, nil, fmt.Errorf("topic must be a leaf")
		}
	}
	if title == "" {
		return "", nil, nil, fmt.Errorf("unknown topic")
	}
	seen := map[string]bool{}
	members := []topicReference{}
	for _, q := range library.Questions {
		if !q.Verified || seen[q.CanonicalID] {
			continue
		}
		for _, tag := range q.TopicIDs {
			if tag == id {
				members = append(members, q)
				seen[q.CanonicalID] = true
				break
			}
		}
	}
	// IDs provide a deterministic order independent of topic membership.
	sort.Slice(members, func(i, j int) bool { return members[i].CanonicalID < members[j].CanonicalID })
	return title, members, library.Questions, nil
}

func (app *App) loadTopicQuestion(ref topicReference) (topicQuestion, error) {
	q := topicQuestion{ID: ref.CanonicalID, QuestionRU: ref.QuestionRU, Help: map[string]string{}, Answers: []topicAnswer{}}
	var en, es, ru string
	err := app.db.QueryRow(`SELECT prompt,help_en,help_es,help_ru,image_path FROM question_contents WHERE id=?`, ref.CanonicalID).Scan(&q.Question, &en, &es, &ru, &q.Image)
	if err != nil {
		return q, err
	}
	q.Help = map[string]string{"en": en, "es": es, "ru": ru}
	rows, err := app.db.Query(`SELECT text,correct FROM content_answers WHERE content_id=?`, ref.CanonicalID)
	if err != nil {
		return q, err
	}
	defer rows.Close()
	for rows.Next() {
		var a topicAnswer
		if err := rows.Scan(&a.Text, &a.Correct); err != nil {
			return q, err
		}
		q.Answers = append(q.Answers, a)
	}
	if err := rows.Err(); err != nil {
		return q, err
	}
	// Choices use a stable text-based order.
	sort.Slice(q.Answers, func(i, j int) bool {
		return normalizeTopicText(q.Answers[i].Text) < normalizeTopicText(q.Answers[j].Text)
	})
	for i := range q.Answers {
		q.Answers[i].Letter = string(rune('A' + i))
	}
	return q, nil
}
func normalizeTopicText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
func makeTopicProgress(q topicQuestion, selectedText string, correct bool) *topicProgress {
	p := &topicProgress{Correct: correct}
	for _, a := range q.Answers {
		if a.Correct {
			p.CorrectLetter = a.Letter
		}
		if normalizeTopicText(a.Text) == normalizeTopicText(selectedText) {
			p.SelectedLetter = a.Letter
		}
	}
	return p
}
func (app *App) topic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("topicID")
	title, members, _, err := topicMembers(id)
	if err != nil {
		app.writeError(w, http.StatusNotFound, "Подтема не найдена")
		return
	}
	numbers := map[string]int{}
	for index, ref := range members {
		numbers[ref.CanonicalID] = index + 1
	}
	current := r.Context().Value(userContextKey).(user)
	reviewID, runID := int64(0), int64(0)
	kind := "full"
	if value := r.URL.Query().Get("review"); value != "" {
		reviewID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || reviewID <= 0 {
			app.writeError(w, 422, "Некорректная работа над ошибками")
			return
		}
		members, err = app.reviewMembers(current.ID, id, reviewID, members)
		if err != nil {
			app.writeError(w, 404, "Работа над ошибками не найдена")
			return
		}
	} else {
		if value := r.URL.Query().Get("run"); value != "" {
			runID, err = strconv.ParseInt(value, 10, 64)
			if err != nil || runID <= 0 {
				app.writeError(w, 422, "Некорректное прохождение")
				return
			}
		} else {
			runID, err = app.currentTopicRun(current.ID, id, members)
			if err != nil {
				app.serverError(w, err)
				return
			}
		}
		members, kind, err = app.runMembers(current.ID, id, runID, members)
		if err != nil {
			app.writeError(w, 404, "Прохождение не найдено")
			return
		}
	}
	if reviewID > 0 || kind == "new" {
		numbers = map[string]int{}
		for index, ref := range members {
			numbers[ref.CanonicalID] = index + 1
		}
	}
	if reviewID == 0 {
		tx, e := app.db.Begin()
		if e != nil {
			app.serverError(w, e)
			return
		}
		if e = inheritTopicProgress(tx, runID); e != nil {
			tx.Rollback()
			app.serverError(w, e)
			return
		}
		if e = tx.Commit(); e != nil {
			app.serverError(w, e)
			return
		}
	}
	questions := make([]topicQuestion, 0, len(members))
	for _, ref := range members {
		q, err := app.loadTopicQuestion(ref)
		if err != nil {
			app.serverError(w, err)
			return
		}
		q.Number = numbers[ref.CanonicalID]
		if q.Number == 0 {
			q.Number = len(numbers) + 1
			numbers[ref.CanonicalID] = q.Number
		}
		var selected string
		var correct bool
		if reviewID > 0 {
			err = app.db.QueryRow(`SELECT selected_text,correct FROM topic_run_progress WHERE run_id=? AND content_id=?`, reviewID, q.ID).Scan(&selected, &correct)
		} else {
			err = app.db.QueryRow(`SELECT selected_text,correct FROM topic_run_progress WHERE run_id=? AND content_id=?`, runID, q.ID).Scan(&selected, &correct)
		}
		if err == nil {
			q.Progress = makeTopicProgress(q, selected, correct)
		} else if err != sql.ErrNoRows {
			app.serverError(w, err)
			return
		}
		questions = append(questions, q)
	}
	app.writeJSON(w, http.StatusOK, map[string]any{"id": id, "title": title, "questions": questions, "run_id": runID, "kind": kind, "numbering_total": len(numbers)})
}
func (app *App) answerTopic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("topicID")
	var input struct {
		QuestionID     string `json:"question_id"`
		SelectedLetter string `json:"selected_letter"`
		ReviewID       int64  `json:"review_id"`
		RunID          int64  `json:"run_id"`
	}
	if !app.decodeJSON(w, r, &input) {
		return
	}
	_, members, _, err := topicMembers(id)
	if err != nil {
		app.writeError(w, http.StatusNotFound, "Подтема не найдена")
		return
	}
	current := r.Context().Value(userContextKey).(user)
	if input.ReviewID < 0 {
		app.writeError(w, 422, "Некорректная работа над ошибками")
		return
	}
	if input.RunID < 0 || (input.RunID > 0 && input.ReviewID > 0) {
		app.writeError(w, 422, "Некорректное прохождение")
		return
	}
	if input.ReviewID == 0 {
		if input.RunID == 0 {
			input.RunID, err = app.currentTopicRun(current.ID, id, members)
			if err != nil {
				app.serverError(w, err)
				return
			}
		}
		members, _, err = app.runMembers(current.ID, id, input.RunID, members)
		if err != nil {
			app.writeError(w, 404, "Прохождение не найдено")
			return
		}
	}
	if input.ReviewID > 0 {
		members, err = app.reviewMembers(current.ID, id, input.ReviewID, members)
		if err != nil {
			app.writeError(w, 404, "Работа над ошибками не найдена")
			return
		}
	}
	var ref *topicReference
	for i := range members {
		if members[i].CanonicalID == input.QuestionID {
			ref = &members[i]
			break
		}
	}
	if ref == nil {
		app.writeError(w, http.StatusUnprocessableEntity, "Вопрос не входит в подтему")
		return
	}
	q, err := app.loadTopicQuestion(*ref)
	if err != nil {
		app.serverError(w, err)
		return
	}
	var choice *topicAnswer
	for i := range q.Answers {
		if q.Answers[i].Letter == input.SelectedLetter {
			choice = &q.Answers[i]
			break
		}
	}
	if choice == nil {
		app.writeError(w, http.StatusUnprocessableEntity, "Некорректный вариант ответа")
		return
	}
	tx, err := app.db.Begin()
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer tx.Rollback()
	runID := input.ReviewID
	if runID == 0 {
		runID = input.RunID
		var currentCycle bool
		err = tx.QueryRow(`SELECT ? >= COALESCE(MAX(id),0) FROM topic_runs WHERE user_id=? AND topic_id=? AND kind='full' AND parent_id IS NULL`, runID, current.ID, id).Scan(&currentCycle)
		if err != nil {
			app.serverError(w, err)
			return
		}
		if !currentCycle {
			app.writeError(w, 409, "Уже начато новое прохождение. Вернитесь к списку тем.")
			return
		}
		_, err = tx.Exec(`INSERT INTO topic_answers(user_id,topic_id,content_id,selected_text,correct,answered_at) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id,topic_id,content_id) DO NOTHING`, current.ID, id, q.ID, choice.Text, choice.Correct, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			app.serverError(w, err)
			return
		}
	}
	_, err = tx.Exec(`INSERT INTO topic_run_answers(run_id,content_id,selected_text,correct,answered_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, runID, q.ID, choice.Text, choice.Correct, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		app.serverError(w, err)
		return
	}
	var selected string
	var correct bool
	err = tx.QueryRow(`SELECT selected_text,correct FROM topic_run_progress WHERE run_id=? AND content_id=?`, runID, q.ID).Scan(&selected, &correct)
	if err != nil {
		app.serverError(w, err)
		return
	}
	if err = completeTopicRun(tx, runID); err != nil {
		app.serverError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.serverError(w, err)
		return
	}
	app.writeJSON(w, http.StatusOK, makeTopicProgress(q, selected, correct))
}
func (app *App) resetTopic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("topicID")
	_, members, _, err := topicMembers(id)
	if err != nil {
		app.writeError(w, 404, "Подтема не найдена")
		return
	}
	current := r.Context().Value(userContextKey).(user)
	tx, err := app.db.Begin()
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer tx.Rollback()
	runID, err := ensureTopicRun(tx, current.ID, id, members)
	if err != nil {
		app.serverError(w, err)
		return
	}
	var completed sql.NullString
	if err = tx.QueryRow(`SELECT completed_at FROM topic_runs WHERE id=?`, runID).Scan(&completed); err != nil {
		app.serverError(w, err)
		return
	}
	if !completed.Valid {
		app.writeError(w, 409, "Сначала завершите текущее прохождение")
		return
	}
	for _, ref := range members {
		var answered bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM question_progress WHERE user_id=? AND content_id=?)`, current.ID, ref.CanonicalID).Scan(&answered); err != nil {
			app.serverError(w, err)
			return
		}
		if !answered {
			app.writeError(w, 409, "Сначала ответьте на оставшиеся вопросы темы")
			return
		}
	}
	result, err := tx.Exec(`INSERT INTO topic_runs(user_id,topic_id,total,started_at) VALUES(?,?,?,?)`, current.ID, id, len(members), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		app.serverError(w, err)
		return
	}
	nextID, err := result.LastInsertId()
	if err != nil {
		app.serverError(w, err)
		return
	}
	if err = saveRunMembers(tx, nextID, members); err != nil {
		app.serverError(w, err)
		return
	}
	if _, err = tx.Exec(`DELETE FROM topic_answers WHERE user_id=? AND topic_id=?`, current.ID, id); err != nil {
		app.serverError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// topicSummaries counts only current verified memberships and the signed-in user's answers.
func (app *App) topicSummaries(w http.ResponseWriter, r *http.Request) {
	data, err := seed.Files.ReadFile("library.json")
	if err != nil {
		app.serverError(w, err)
		return
	}
	var library topicLibrary
	if err = json.Unmarshal(data, &library); err != nil {
		app.serverError(w, err)
		return
	}
	parents := map[string]bool{}
	for _, node := range library.Nodes {
		if node.Parent != nil {
			parents[*node.Parent] = true
		}
	}
	members := map[string]map[string]bool{}
	refsByTopic := map[string][]topicReference{}
	for _, node := range library.Nodes {
		if !parents[node.ID] {
			members[node.ID] = map[string]bool{}
		}
	}
	for _, q := range library.Questions {
		if !q.Verified {
			continue
		}
		for _, tag := range q.TopicIDs {
			if set, ok := members[tag]; ok {
				if !set[q.CanonicalID] {
					refsByTopic[tag] = append(refsByTopic[tag], q)
				}
				set[q.CanonicalID] = true
			}
		}
	}
	type summary struct {
		Total            int               `json:"total"`
		Answered         int               `json:"answered"`
		Correct          int               `json:"correct"`
		Wrong            int               `json:"wrong"`
		NextQuestion     int               `json:"next_question"`
		Remaining        int               `json:"remaining"`
		Results          map[string]bool   `json:"results"`
		Runs             []topicRunSummary `json:"runs"`
		NewCount         int               `json:"new_count"`
		NewQuestionIDs   []string          `json:"new_question_ids"`
		QuestionRuns     map[string]int64  `json:"question_runs"`
		Completed        bool              `json:"completed"`
		RepeatInProgress bool              `json:"repeat_in_progress"`
	}
	result := map[string]*summary{}
	for id, set := range members {
		result[id] = &summary{Total: len(set), Results: map[string]bool{}}
	}
	current := r.Context().Value(userContextKey).(user)
	rows, err := app.db.Query(`SELECT content_id,correct FROM question_current_progress WHERE user_id=?`, current.ID)
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		var correct bool
		if err = rows.Scan(&content, &correct); err != nil {
			app.serverError(w, err)
			return
		}
		for id, item := range result {
			if !members[id][content] {
				continue
			}
			item.Results[content] = correct
			item.Answered++
			if correct {
				item.Correct++
			} else {
				item.Wrong++
			}
		}
	}
	if err = rows.Err(); err != nil {
		app.serverError(w, err)
		return
	}
	rows.Close()
	for id, item := range result {
		var legacy bool
		if err = app.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM topic_answers WHERE user_id=? AND topic_id=?) OR EXISTS(SELECT 1 FROM topic_runs WHERE user_id=? AND topic_id=? AND parent_id IS NULL)`, current.ID, id, current.ID, id).Scan(&legacy); err != nil {
			app.serverError(w, err)
			return
		}
		if legacy {
			if _, err = app.currentTopicRun(current.ID, id, refsByTopic[id]); err != nil {
				app.serverError(w, err)
				return
			}
		}
		item.Runs, err = app.topicHistory(current.ID, id)
		if err != nil {
			app.serverError(w, err)
			return
		}
		item.NewQuestionIDs, item.QuestionRuns, err = app.newTopicQuestions(current.ID, id, refsByTopic[id])
		if err != nil {
			app.serverError(w, err)
			return
		}
		item.NewCount = len(item.NewQuestionIDs)
		item.Completed = item.Total > 0 && item.Answered == item.Total
		ids := make([]string, 0, len(members[id]))
		for content := range members[id] {
			ids = append(ids, content)
		}
		sort.Strings(ids)
		fullID := int64(0)
		for _, run := range item.Runs {
			if run.Kind == "full" {
				fullID = run.ID
				break
			}
		}
		fullRuns := 0
		for _, run := range item.Runs {
			if run.Kind == "full" {
				fullRuns++
			}
		}
		if fullRuns > 1 && len(item.Runs) > 0 {
			for _, run := range item.Runs {
				if run.Kind == "full" {
					item.RepeatInProgress = run.CompletedAt == nil
					break
				}
			}
		}
		numbers := map[string]int{}
		for index, content := range ids {
			numbers[content] = index + 1
		}
		answered := map[string]bool{}
		baseline := map[string]bool{}
		if fullID > 0 {
			progressRows, e := app.db.Query(`SELECT m.content_id,EXISTS(SELECT 1 FROM topic_run_progress p WHERE p.run_id=m.run_id AND p.content_id=m.content_id) FROM topic_run_members m WHERE m.run_id=?`, fullID)
			if e != nil {
				app.serverError(w, e)
				return
			}
			for progressRows.Next() {
				var content string
				var done bool
				if e = progressRows.Scan(&content, &done); e != nil {
					progressRows.Close()
					app.serverError(w, e)
					return
				}
				baseline[content] = true
				answered[content] = done
			}
			e = progressRows.Err()
			progressRows.Close()
			if e != nil {
				app.serverError(w, e)
				return
			}
			// Use exactly the same frozen membership and order as the trainer.
			frozen, _, e := app.runMembers(current.ID, id, fullID, refsByTopic[id])
			if e != nil {
				app.serverError(w, e)
				return
			}
			ids = ids[:0]
			for _, ref := range frozen {
				ids = append(ids, ref.CanonicalID)
				if numbers[ref.CanonicalID] == 0 {
					numbers[ref.CanonicalID] = len(numbers) + 1
				}
			}
		} else {
			for content := range item.Results {
				answered[content] = true
			}
		}
		newIDs := map[string]bool{}
		for _, content := range item.NewQuestionIDs {
			newIDs[content] = true
		}
		for _, content := range ids {
			if newIDs[content] || (fullID > 0 && !baseline[content]) {
				continue
			}
			if !answered[content] {
				item.Remaining++
				if item.NextQuestion == 0 {
					item.NextQuestion = numbers[content]
				}
			}
		}
	}
	app.writeJSON(w, http.StatusOK, result)
}
