package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCurrentTopicErrorsMatchReviewAndSuccessfulAnswersClearThem(t *testing.T) {
	app, sender := newTestApp(t)
	uid, cookie := signInProgressUser(t, app, sender, "current-errors@example.com")
	const topic = "numbers.dimensions"
	const path = "/api/topics/" + topic
	_, members, _, err := topicMembers(topic)
	if err != nil {
		t.Fatal(err)
	}
	// Five errors originate in different topics/batches, not only the latest run.
	for i, ref := range members {
		putLegacyKnowledge(t, app, uid, fmt.Sprintf("previous-topic-%d", i), ref, i >= 5, "2026-10-01T10:00:00Z")
	}
	summary := func(wrong int) {
		t.Helper()
		res := request(t, app.Handler(), "GET", "/api/topics/progress", "", cookie)
		var data map[string]struct {
			Wrong   int             `json:"wrong"`
			Correct int             `json:"correct"`
			Results map[string]bool `json:"results"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if res.Code != 200 || data[topic].Wrong != wrong || data[topic].Correct != len(members)-wrong {
			t.Fatalf("current counters disagree: %+v", data[topic])
		}
	}
	start := func() int64 {
		t.Helper()
		res := request(t, app.Handler(), "POST", path+"/error-reviews", "", cookie)
		var v struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &v); err != nil || res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		return v.ID
	}
	load := func(id int64) []topicQuestion {
		t.Helper()
		res := request(t, app.Handler(), "GET", fmt.Sprintf("%s?review=%d", path, id), "", cookie)
		var v struct {
			Questions []topicQuestion `json:"questions"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &v); err != nil || res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		return v.Questions
	}
	answer := func(id int64, q topicQuestion, correct bool) {
		t.Helper()
		letter := ""
		for _, ref := range members {
			if ref.CanonicalID == q.ID {
				loaded, err := app.loadTopicQuestion(ref)
				if err != nil {
					t.Fatal(err)
				}
				q = loaded
				break
			}
		}
		for _, a := range q.Answers {
			if a.Correct == correct {
				letter = a.Letter
				break
			}
		}
		res := request(t, app.Handler(), "POST", path+"/answers", fmt.Sprintf(`{"question_id":%q,"selected_letter":%q,"review_id":%d}`, q.ID, letter, id), cookie)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
	}
	summary(5)
	id := start()
	questions := load(id)
	if len(questions) != 5 {
		t.Fatalf("expected five review questions, got %d", len(questions))
	}
	answer(id, questions[0], true)
	summary(4)
	if got := start(); got != id {
		t.Fatal("did not resume current session")
	}
	questions = load(id)
	if len(questions) != 5 || questions[0].Progress == nil {
		t.Fatal("resume lost frozen membership/progress")
	}
	for i, q := range questions[1:] {
		answer(id, q, i != 0)
	}
	summary(1) // A repeated wrong answer remains an actual error.
	next := start()
	if next == id {
		t.Fatal("reopened completed session")
	}
	remaining := load(next)
	if len(remaining) != 1 || remaining[0].ID != questions[1].ID {
		t.Fatal("retry does not match remaining error")
	}
	answer(next, remaining[0], true)
	summary(0)
	summary(0)
	history, err := app.topicHistory(uid, topic)
	if err != nil || len(history) == 0 || len(history[0].ErrorReviews) != 2 {
		t.Fatalf("topic review history missing: %+v %v", history, err)
	}
	if res := request(t, app.Handler(), "POST", path+"/error-reviews", "", cookie); res.Code != 409 {
		t.Fatal("offered review with zero errors")
	}
	// Refreshing a completed session can still show its saved results.
	if len(load(next)) != 1 {
		t.Fatal("completed review disappeared")
	}
	var originals int
	if err = app.db.QueryRow(`SELECT COUNT(*) FROM topic_answers WHERE user_id=? AND correct=0`, uid).Scan(&originals); err != nil || originals != 5 {
		t.Fatal("original answers were overwritten")
	}
	_, other := signInProgressUser(t, app, sender, "isolated-current-errors@example.com")
	if res := request(t, app.Handler(), "GET", fmt.Sprintf("%s?review=%d", path, next), "", other); res.Code != 404 {
		t.Fatal("review leaked between users")
	}
}
