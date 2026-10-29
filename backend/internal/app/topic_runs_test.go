package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestTopicRunHistoryAndIsolatedReviews(t *testing.T) {
	application, sender := newTestApp(t)
	handler := application.Handler()
	request(t, handler, "POST", "/api/auth/request-code", `{"email":"runs@example.com"}`, nil)
	login := request(t, handler, "POST", "/api/auth/verify-code", `{"email":"runs@example.com","code":"`+sender.code+`"}`, nil)
	cookie := login.Result().Cookies()[0]
	const path = "/api/topics/routine.drugs"
	var uid string
	if err := application.db.QueryRow(`SELECT id FROM users WHERE email='runs@example.com'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	_, members, _, err := topicMembers("routine.drugs")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an answer from before history existed; migrate it exactly once.
	first, err := application.loadTopicQuestion(members[0])
	if err != nil {
		t.Fatal(err)
	}
	var wrongLetter, correctLetter string
	var wrongText string
	for _, choice := range first.Answers {
		if choice.Correct {
			correctLetter = choice.Letter
		} else {
			wrongLetter = choice.Letter
			wrongText = choice.Text
		}
	}
	if _, err = application.db.Exec(`INSERT INTO topic_answers VALUES(?,?,?,?,?,?)`, uid, "routine.drugs", first.ID, wrongText, false, "2026-10-01T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res := request(t, handler, "GET", path, "", cookie)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
	}
	history, err := application.topicHistory(uid, "routine.drugs")
	if err != nil || len(history) != 1 || history[0].Answered != 1 || history[0].StartedAt != "2026-10-01T10:00:00Z" {
		t.Fatalf("legacy migration: %+v %v", history, err)
	}
	parent := history[0].ID
	reviewPath := fmt.Sprintf("%s/reviews/%d", path, parent)
	if res := request(t, handler, "POST", reviewPath, "", cookie); res.Code != 409 {
		t.Fatal("accepted unfinished review")
	}
	for _, ref := range members[1:] {
		q, e := application.loadTopicQuestion(ref)
		if e != nil {
			t.Fatal(e)
		}
		letter := ""
		for _, a := range q.Answers {
			if a.Correct {
				letter = a.Letter
			}
		}
		res := request(t, handler, "POST", path+"/answers", fmt.Sprintf(`{"question_id":%q,"selected_letter":%q}`, q.ID, letter), cookie)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
	}
	var reviewID struct {
		ID int64 `json:"id"`
	}
	for i := 0; i < 2; i++ {
		res := request(t, handler, "POST", reviewPath, "", cookie)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		var got struct {
			ID int64 `json:"id"`
		}
		json.Unmarshal(res.Body.Bytes(), &got)
		if i > 0 && got.ID != reviewID.ID {
			t.Fatal("duplicate review created")
		}
		reviewID = got
	}
	res := request(t, handler, "GET", fmt.Sprintf("%s?review=%d", path, reviewID.ID), "", cookie)
	var review struct {
		Questions []topicQuestion `json:"questions"`
	}
	json.Unmarshal(res.Body.Bytes(), &review)
	if res.Code != 200 || len(review.Questions) != 1 || review.Questions[0].ID != first.ID || review.Questions[0].Progress != nil {
		t.Fatalf("review membership: %s", res.Body.String())
	}
	invalid := request(t, handler, "POST", path+"/answers", fmt.Sprintf(`{"question_id":%q,"selected_letter":"A","review_id":%d}`, members[1].CanonicalID, reviewID.ID), cookie)
	if invalid.Code != 422 {
		t.Fatal("accepted a non-error question in review")
	}
	for _, letter := range []string{correctLetter, wrongLetter} {
		res = request(t, handler, "POST", path+"/answers", fmt.Sprintf(`{"question_id":%q,"selected_letter":%q,"review_id":%d}`, first.ID, letter, reviewID.ID), cookie)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
	}
	history, err = application.topicHistory(uid, "routine.drugs")
	if err != nil || len(history) != 1 || history[0].Correct != len(members)-1 || history[0].Wrong != 1 || history[0].CompletedAt == nil || history[0].Review == nil || history[0].Review.Correct != 1 || history[0].Review.CompletedAt == nil {
		t.Fatalf("history changed or review incomplete: %+v %v", history, err)
	}
	res = request(t, handler, "GET", "/api/topics/progress", "", cookie)
	var summaries map[string]struct {
		Correct int               `json:"correct"`
		Wrong   int               `json:"wrong"`
		Runs    []topicRunSummary `json:"runs"`
	}
	json.Unmarshal(res.Body.Bytes(), &summaries)
	if summaries["routine.drugs"].Wrong != 0 || summaries["routine.drugs"].Correct != len(members) || len(summaries["routine.drugs"].Runs) != 1 {
		t.Fatal("corrected current results or preserved history are incorrect")
	}
	// Repeat the full topic while keeping the prior run and its review available.
	res = request(t, handler, "POST", path+"/reset", "", cookie)
	if res.Code != 204 {
		t.Fatal(res.Body.String())
	}
	history, err = application.topicHistory(uid, "routine.drugs")
	if err != nil || len(history) != 2 || history[0].Answered != 0 || history[1].Wrong != 1 || history[1].Review == nil {
		t.Fatalf("repeat lost history: %+v %v", history, err)
	}
	res = request(t, handler, "GET", fmt.Sprintf("%s?review=%d", path, reviewID.ID), "", cookie)
	json.Unmarshal(res.Body.Bytes(), &review)
	if len(review.Questions) != 1 || review.Questions[0].Progress == nil || !review.Questions[0].Progress.Correct {
		t.Fatal("old review not restored after full repeat")
	}
	request(t, handler, "POST", "/api/auth/request-code", `{"email":"isolated-runs@example.com"}`, nil)
	other := request(t, handler, "POST", "/api/auth/verify-code", `{"email":"isolated-runs@example.com","code":"`+sender.code+`"}`, nil).Result().Cookies()[0]
	if res = request(t, handler, "POST", reviewPath, "", other); res.Code != http.StatusNotFound {
		t.Fatal("review leaked across users")
	}
	if res = request(t, handler, "GET", fmt.Sprintf("%s?review=%d", path, reviewID.ID), "", other); res.Code != http.StatusNotFound {
		t.Fatal("review answers leaked across users")
	}
}

// New-batch answers are also inherited by the full run. Reviewing that event
// once must cover both copies, without fabricating another review answer.
func TestReviewCoversInheritedDuplicateButNotLaterMistakes(t *testing.T) {
	for _, fullErrorCount := range []int{1, 2} {
		t.Run(fmt.Sprintf("full-errors-%d", fullErrorCount), func(t *testing.T) {
			app, sender := newTestApp(t)
			uid, cookie := signInProgressUser(t, app, sender, "duplicate-review@example.com")
			const topic = "routine.drugs"
			const path = "/api/topics/" + topic
			_, members, _, err := topicMembers(topic)
			if err != nil {
				t.Fatal(err)
			}
			createRun := func(kind string, at string, refs []topicReference, inherited bool) int64 {
				t.Helper()
				result, err := app.db.Exec(`INSERT INTO topic_runs(user_id,topic_id,kind,total,started_at,completed_at) VALUES(?,?,?,?,?,?)`, uid, topic, kind, len(refs), at, at)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := result.LastInsertId()
				table := "topic_run_answers"
				if inherited {
					table = "topic_run_inherited"
				}
				for _, ref := range refs {
					if _, err = app.db.Exec(`INSERT INTO topic_run_members VALUES(?,?)`, id, ref.CanonicalID); err != nil {
						t.Fatal(err)
					}
					if _, err = app.db.Exec(`INSERT INTO `+table+` VALUES(?,?,?,?,?)`, id, ref.CanonicalID, "wrong", false, at); err != nil {
						t.Fatal(err)
					}
				}
				return id
			}
			startReview := func(parent int64) int64 {
				t.Helper()
				res := request(t, app.Handler(), "POST", fmt.Sprintf("%s/reviews/%d", path, parent), "", cookie)
				var value struct {
					ID int64 `json:"id"`
				}
				if res.Code != 200 {
					t.Fatal(res.Body.String())
				}
				if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				return value.ID
			}
			answer := func(review int64, ref topicReference) {
				t.Helper()
				q, err := app.loadTopicQuestion(ref)
				if err != nil {
					t.Fatal(err)
				}
				letter := ""
				for _, a := range q.Answers {
					if a.Correct {
						letter = a.Letter
					}
				}
				res := request(t, app.Handler(), "POST", path+"/answers", fmt.Sprintf(`{"question_id":%q,"selected_letter":%q,"review_id":%d}`, q.ID, letter, review), cookie)
				if res.Code != 200 {
					t.Fatal(res.Body.String())
				}
			}
			full := createRun("full", "2026-10-01T10:00:00Z", members[:fullErrorCount], true)
			batch := createRun("new", "2026-10-01T10:00:00Z", members[:1], false)
			review := startReview(batch)
			answer(review, members[0])
			for i := 0; i < 2; i++ { // Repeated dashboard loads must not lose the coverage.
				history, err := app.topicHistory(uid, topic)
				if err != nil || len(history) != 2 || history[0].ReviewPending != 0 || history[1].ReviewPending != fullErrorCount-1 || history[1].Wrong != fullErrorCount {
					t.Fatalf("duplicate review not covered, or original history changed: %+v %v", history, err)
				}
			}
			if fullErrorCount > 1 {
				// The full review contains only its other, genuinely unreviewed error.
				otherReview := startReview(full)
				res := request(t, app.Handler(), "GET", fmt.Sprintf("%s?review=%d", path, otherReview), "", cookie)
				var loaded struct {
					Questions []topicQuestion `json:"questions"`
				}
				if err = json.Unmarshal(res.Body.Bytes(), &loaded); err != nil {
					t.Fatal(err)
				}
				if res.Code != 200 || len(loaded.Questions) != 1 || loaded.Questions[0].ID != members[1].CanonicalID {
					t.Fatal(res.Body.String())
				}
				answer(otherReview, members[1])
				history, err := app.topicHistory(uid, topic)
				if err != nil || history[0].ReviewPending != 0 || history[1].ReviewPending != 0 || history[1].Review.Total != 1 || history[1].Review.CompletedAt == nil {
					t.Fatalf("review incomplete: %+v %v", history, err)
				}
				// No synthetic answer was inserted for the inherited duplicate.
				var count int
				app.db.QueryRow(`SELECT COUNT(*) FROM topic_run_answers WHERE run_id=?`, otherReview).Scan(&count)
				if count != 1 {
					t.Fatalf("fabricated review answers: %d", count)
				}
			} else {
				res := request(t, app.Handler(), "POST", fmt.Sprintf("%s/reviews/%d", path, full), "", cookie)
				if res.Code != 409 {
					t.Fatal("offered duplicate review", res.Body.String())
				}
			}
			// A new erroneous answer after the review is a new event, even for the same question.
			later := createRun("new", "2099-10-01T10:00:00Z", members[:1], false)
			history, err := app.topicHistory(uid, topic)
			if err != nil || history[0].ID != later || history[0].ReviewPending != 1 {
				t.Fatalf("later mistake hidden: %+v %v", history, err)
			}
			// A fresh full repeat must not inherit review coverage from an earlier cycle.
			fresh := createRun("full", "2026-10-01T10:00:00Z", members[:1], false)
			history, err = app.topicHistory(uid, topic)
			if err != nil || history[0].ID != fresh || history[0].ReviewPending != 1 {
				t.Fatalf("fresh repeat hidden: %+v %v", history, err)
			}

		})
	}
}
