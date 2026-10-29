package app

import (
	"encoding/json"
	"testing"
)

func TestImportedHistoryPreservesFeedbackAndUserIsolation(t *testing.T) {
	app, sender := newTestApp(t)
	uid, cookie := signInProgressUser(t, app, sender, "imported-history@example.com")
	_, other := signInProgressUser(t, app, sender, "separate-history@example.com")
	_, members, _, err := topicMembers("routine.drugs")
	if err != nil || len(members) == 0 {
		t.Fatalf("topic membership: %v", err)
	}
	q, err := app.loadTopicQuestion(members[0])
	if err != nil {
		t.Fatal(err)
	}
	var right, wrong string
	for _, answer := range q.Answers {
		if answer.Correct {
			right = answer.Text
		} else {
			wrong = answer.Text
		}
	}
	for _, event := range []struct {
		id, text, at, mode string
		correct            bool
	}{
		{"first", right, "2026-10-01T10:00:00Z", "training", true},
		{"second", wrong, "2026-10-01T11:00:00Z", "mistakes", false},
	} {
		if _, err := app.db.Exec(`INSERT INTO imported_answers VALUES(?,?,?,?,?,?,?,?)`, event.id, uid, q.ID, "routine.drugs", event.text, event.correct, event.at, event.mode); err != nil {
			t.Fatal(err)
		}
	}
	known, err := app.globalAnswers(uid)
	if err != nil || !known[q.ID].Correct || known[q.ID].Text != right {
		t.Fatalf("review replaced normal knowledge: %+v %v", known[q.ID], err)
	}
	response := request(t, app.Handler(), "GET", "/api/statistics/linear?paged=1", "", cookie)
	var history struct {
		Events  []linearEvent `json:"events"`
		Total   int           `json:"total"`
		Correct int           `json:"correct"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &history) != nil || history.Total != 2 || history.Correct != 1 {
		t.Fatal(response.Body.String())
	}
	if history.Events[0].Mode != "mistakes" || history.Events[0].Correct || history.Events[0].At != "2026-10-01T11:00:00Z" {
		t.Fatalf("history changed: %+v", history.Events)
	}
	selected := false
	for _, answer := range history.Events[0].Answers {
		if answer.Selected && answer.Text == wrong && !answer.Correct {
			selected = true
		}
	}
	if !selected {
		t.Fatal("recorded answer selection was lost")
	}
	response = request(t, app.Handler(), "GET", "/api/statistics/linear?paged=1", "", other)
	if json.Unmarshal(response.Body.Bytes(), &history) != nil || history.Total != 0 {
		t.Fatal("answer history crossed users")
	}
}

func TestUnknownAPIPathReturnsJSON(t *testing.T) {
	app, _ := newTestApp(t)
	response := request(t, app.Handler(), "GET", "/api/not-a-route", "", nil)
	if response.Code != 404 || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected fallback: %d %s", response.Code, response.Body.String())
	}
}
