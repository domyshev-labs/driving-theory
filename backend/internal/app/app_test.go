package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/domyshev-labs/driving-theory/backend/seed"
)

type capturedEmail struct {
	email string
	code  string
}

func (sender *capturedEmail) SendLoginCode(_ context.Context, email, code string) error {
	sender.email = email
	sender.code = code
	return nil
}

func newTestApp(t *testing.T) (*App, *capturedEmail) {
	t.Helper()
	application, err := New(Config{
		Address:       ":0",
		DatabasePath:  t.TempDir() + "/trainer.db",
		StaticDir:     t.TempDir(),
		SessionSecret: "test-secret-with-more-than-24-characters",
		EmailMode:     "log",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	sender := &capturedEmail{}
	application.email = sender
	return application, sender
}

func request(t *testing.T, handler http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestPasswordlessTopicFlow(t *testing.T) {
	application, sender := newTestApp(t)
	_, cookie := signInProgressUser(t, application, sender, "practice@example.com")
	response := request(t, application.Handler(), http.MethodGet, "/api/topics/routine.drugs", "", cookie)
	if response.Code != 200 || strings.Contains(response.Body.String(), `"correct"`) {
		t.Fatalf("question response: %s", response.Body.String())
	}
	var record struct {
		Questions []topicQuestion `json:"questions"`
	}
	if json.Unmarshal(response.Body.Bytes(), &record) != nil || len(record.Questions) == 0 {
		t.Fatal(response.Body.String())
	}
	q := record.Questions[0]
	response = request(t, application.Handler(), http.MethodPost, "/api/topics/routine.drugs/answers", `{"question_id":"`+q.ID+`","selected_letter":"A"}`, cookie)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"correct_letter"`) {
		t.Fatal(response.Body.String())
	}
	response = request(t, application.Handler(), http.MethodPost, "/api/auth/logout", "", cookie)
	if response.Code != 204 {
		t.Fatal(response.Body.String())
	}
	response = request(t, application.Handler(), http.MethodGet, "/api/auth/me", "", cookie)
	if response.Code != 401 {
		t.Fatalf("session was not revoked: %d", response.Code)
	}
}

func TestProtectedRoutesRequireSession(t *testing.T) {
	application, _ := newTestApp(t)
	response := request(t, application.Handler(), http.MethodGet, "/api/topics/progress", "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", response.Code)
	}
}

func TestIncompatibleDatabaseIsRejectedBeforeImport(t *testing.T) {
	path := t.TempDir() + "/older.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE question_contents(id TEXT PRIMARY KEY,prompt TEXT); INSERT INTO question_contents VALUES('saved','unchanged')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if upgraded, err := openDatabase(path); err == nil {
		upgraded.Close()
		t.Fatal("expected incompatible-layout error")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var prompt string
	if err = db.QueryRow(`SELECT prompt FROM question_contents WHERE id='saved'`).Scan(&prompt); err != nil || prompt != "unchanged" {
		t.Fatalf("data changed: %s %v", prompt, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='users'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("schema changed before refusal: %d %v", count, err)
	}
}

func TestQuestionBankIdentityAndImages(t *testing.T) {
	application, _ := newTestApp(t)
	raw, err := seed.Files.ReadFile("questions.json")
	if err != nil {
		t.Fatal(err)
	}
	var bank []seedQuestion
	if err = json.Unmarshal(raw, &bank); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = seedDatabase(application.db); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = application.db.QueryRow(`SELECT count(*) FROM question_contents`).Scan(&count); err != nil || count != len(bank) {
		t.Fatalf("content duplicated: %d %v", count, err)
	}
	for _, q := range bank {
		var image string
		if err = application.db.QueryRow(`SELECT image_path FROM question_contents WHERE id=?`, q.ID).Scan(&image); err != nil || image != q.Image {
			t.Fatalf("image mismatch: %s %v", q.ID, err)
		}
	}
	if err = application.db.QueryRow(`SELECT count(*) FROM question_contents q WHERE (SELECT count(*) FROM content_answers a WHERE a.content_id=q.id AND a.correct=1)<>1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid answer keys: %d %v", count, err)
	}
}

func TestTopicTrainingUsesUniqueMembersAndSeparateProgress(t *testing.T) {
	application, sender := newTestApp(t)
	handler := application.Handler()
	request(t, handler, http.MethodPost, "/api/auth/request-code", `{"email":"topic@example.com"}`, nil)
	login := request(t, handler, http.MethodPost, "/api/auth/verify-code", `{"email":"topic@example.com","code":"`+sender.code+`"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	response := request(t, handler, http.MethodGet, "/api/topics/routine.drugs", "", cookie)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"correct"`) {
		t.Fatalf("topic response leaked answer keys or failed: %s", response.Body.String())
	}
	var topic struct {
		Questions []topicQuestion `json:"questions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &topic); err != nil {
		t.Fatal(err)
	}
	if len(topic.Questions) < 2 {
		t.Fatal("expected multiple drug questions")
	}
	seen := map[string]bool{}
	for i, q := range topic.Questions {
		if q.QuestionRU == "" && q.Question == "" {
			t.Fatal("missing question list title and question wording")
		}
		if q.Number != i+1 || seen[q.ID] {
			t.Fatalf("invalid topic numbering or duplicated content: %+v", q)
		}
		seen[q.ID] = true
	}
	_, members, _, err := topicMembers("routine.drugs")
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.loadTopicQuestion(members[0])
	if err != nil {
		t.Fatal(err)
	}
	letter := ""
	for _, answer := range first.Answers {
		if answer.Correct {
			letter = answer.Letter
		}
	}
	body := `{"question_id":"` + first.ID + `","selected_letter":"` + letter + `"}`
	for i := 0; i < 2; i++ {
		response = request(t, handler, http.MethodPost, "/api/topics/routine.drugs/answers", body, cookie)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"correct":true`) {
			t.Fatalf("topic answer failed: %s", response.Body.String())
		}
	}
	var count int
	if err := application.db.QueryRow(`SELECT count(*) FROM topic_answers`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("topic answer is not idempotent: %d %v", count, err)
	}
	if err := application.db.QueryRow(`SELECT count(*) FROM imported_answers`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("topic changed imported answer history: %d %v", count, err)
	}
	response = request(t, handler, http.MethodGet, "/api/topics/routine.drugs", "", cookie)
	if !strings.Contains(response.Body.String(), `"selected_letter":"`+letter+`"`) {
		t.Fatal("topic progress was not restored")
	}
	response = request(t, handler, http.MethodPost, "/api/topics/routine.drugs/answers", `{"question_id":"not-a-member","selected_letter":"A"}`, cookie)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatal("accepted a question outside the topic")
	}
	response = request(t, handler, http.MethodGet, "/api/topics/routine", "", cookie)
	if response.Code != http.StatusNotFound {
		t.Fatal("parent topic should not create a separate training collection")
	}

	second, err := application.loadTopicQuestion(members[1])
	if err != nil {
		t.Fatal(err)
	}
	wrong := ""
	for _, answer := range second.Answers {
		if !answer.Correct {
			wrong = answer.Letter
			break
		}
	}
	response = request(t, handler, http.MethodPost, "/api/topics/routine.drugs/answers", `{"question_id":"`+second.ID+`","selected_letter":"`+wrong+`"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	response = request(t, handler, http.MethodGet, "/api/topics/progress", "", cookie)
	var summaries map[string]struct {
		Total    int             `json:"total"`
		Answered int             `json:"answered"`
		Correct  int             `json:"correct"`
		Wrong    int             `json:"wrong"`
		Next     int             `json:"next_question"`
		Results  map[string]bool `json:"results"`
	}
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	stats := summaries["routine.drugs"]
	if stats.Total != len(members) || stats.Answered != 2 || stats.Correct != 1 || stats.Wrong != 1 || stats.Next != 3 || !stats.Results[first.ID] || stats.Results[second.ID] {
		t.Fatalf("incorrect summary or resume position: %+v", stats)
	}
	_, alcoholMembers, _, e := topicMembers("routine.alcohol")
	if e != nil {
		t.Fatal(e)
	}
	expectedAlcohol := 0
	for _, ref := range alcoholMembers {
		if ref.CanonicalID == first.ID || ref.CanonicalID == second.ID {
			expectedAlcohol++
		}
	}
	if summaries["routine.alcohol"].Answered != expectedAlcohol {
		t.Fatal("shared-question progress did not follow canonical membership")
	}
	request(t, handler, http.MethodPost, "/api/auth/request-code", `{"email":"other-topic@example.com"}`, nil)
	other := request(t, handler, http.MethodPost, "/api/auth/verify-code", `{"email":"other-topic@example.com","code":"`+sender.code+`"}`, nil)
	isolated := request(t, handler, http.MethodGet, "/api/topics/progress", "", other.Result().Cookies()[0])
	if strings.Contains(isolated.Body.String(), first.ID) {
		t.Fatal("summary leaked another user's progress")
	}
	response = request(t, handler, http.MethodPost, "/api/topics/routine.drugs/reset", "", cookie)
	if response.Code != http.StatusConflict {
		t.Fatalf("unfinished topic must not lose progress: %s", response.Body.String())
	}
	if err := application.db.QueryRow(`SELECT count(*) FROM topic_answers`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("unfinished topic progress changed: %d %v", count, err)
	}
}

func TestRussianQuestionListTitlesAreConsistentWhenPresent(t *testing.T) {
	data, err := seed.Files.ReadFile("library.json")
	if err != nil {
		t.Fatal(err)
	}
	var library topicLibrary
	if err = json.Unmarshal(data, &library); err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, q := range library.Questions {
		// New questions may retain their question wording until a translation is requested.
		if strings.TrimSpace(q.QuestionRU) == "" {
			continue
		}
		if previous, ok := titles[q.CanonicalID]; ok && previous != q.QuestionRU {
			t.Fatalf("inconsistent shared translation: %s", q.CanonicalID)
		}
		titles[q.CanonicalID] = q.QuestionRU
	}
	if len(titles) == 0 {
		t.Fatal("existing Russian title metadata was lost")
	}
}
