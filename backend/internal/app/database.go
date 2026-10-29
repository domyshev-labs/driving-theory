package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/domyshev-labs/driving-theory/backend/seed"
	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS login_codes (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  code_hash TEXT NOT NULL,
  request_ip TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_login_codes_email_created ON login_codes(email, created_at DESC);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE TABLE IF NOT EXISTS question_contents (
  id TEXT PRIMARY KEY,
  prompt TEXT NOT NULL,
  help_en TEXT NOT NULL,
  help_es TEXT NOT NULL,
  help_ru TEXT NOT NULL,
  image_path TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS topic_answers (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 topic_id TEXT NOT NULL,
 content_id TEXT NOT NULL REFERENCES question_contents(id),
 selected_text TEXT NOT NULL,
 correct INTEGER NOT NULL CHECK(correct IN (0,1)),
 answered_at TEXT NOT NULL,
 PRIMARY KEY(user_id,topic_id,content_id)
);
CREATE TABLE IF NOT EXISTS topic_runs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 topic_id TEXT NOT NULL,
 parent_id INTEGER REFERENCES topic_runs(id),
 total INTEGER NOT NULL,
 started_at TEXT NOT NULL,
 completed_at TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_topic_review_parent ON topic_runs(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_topic_runs_user ON topic_runs(user_id,topic_id,id);
CREATE TABLE IF NOT EXISTS topic_run_members (
 run_id INTEGER NOT NULL REFERENCES topic_runs(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL REFERENCES question_contents(id),
 PRIMARY KEY(run_id,content_id)
);
CREATE TABLE IF NOT EXISTS topic_run_answers (
 run_id INTEGER NOT NULL REFERENCES topic_runs(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL REFERENCES question_contents(id),
 selected_text TEXT NOT NULL,
 correct INTEGER NOT NULL CHECK(correct IN (0,1)),
 answered_at TEXT NOT NULL,
 PRIMARY KEY(run_id,content_id)
);
CREATE TABLE IF NOT EXISTS content_answers (
 content_id TEXT NOT NULL REFERENCES question_contents(id),
 letter TEXT NOT NULL, text TEXT NOT NULL, correct INTEGER NOT NULL CHECK(correct IN (0,1)),
 PRIMARY KEY(content_id,letter)
);
CREATE TABLE IF NOT EXISTS imported_answers (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id),
 content_id TEXT NOT NULL REFERENCES question_contents(id), topic_id TEXT NOT NULL,
 selected_text TEXT NOT NULL, correct INTEGER NOT NULL CHECK(correct IN (0,1)),
 answered_at TEXT NOT NULL, mode TEXT NOT NULL CHECK(mode IN ('training','mistakes'))
);
`

type seedAnswer struct {
	Letter  string `json:"letter"`
	Text    string `json:"text"`
	Correct bool   `json:"correct"`
}
type seedQuestion struct {
	ID       string            `json:"id"`
	Question string            `json:"question"`
	Help     map[string]string `json:"help"`
	Image    string            `json:"image"`
	Answers  []seedAnswer      `json:"answers"`
}

func openDatabase(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, error) { db.Close(); return nil, err }
	// Refuse incompatible data before applying schema or importing content.
	rows, err := db.Query(`PRAGMA table_info(question_contents)`)
	if err != nil {
		return fail(err)
	}
	columns, hasImage := 0, false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var value sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &value, &pk); err != nil {
			rows.Close()
			return fail(err)
		}
		columns++
		hasImage = hasImage || name == "image_path"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	if columns > 0 && !hasImage {
		return fail(fmt.Errorf("incompatible database layout: migrate a verified backup before upgrading"))
	}
	if _, err := db.Exec(schema); err != nil {
		return fail(fmt.Errorf("apply schema: %w", err))
	}
	if err := migrateTopicRunKinds(db); err != nil {
		return fail(err)
	}
	if err := seedDatabase(db); err != nil {
		return fail(err)
	}
	if err := migrateGlobalProgress(db); err != nil {
		return fail(err)
	}
	if err := reconcileTopicMemberships(db); err != nil {
		return fail(err)
	}
	return db, nil
}

func seedDatabase(db *sql.DB) error {
	data, err := seed.Files.ReadFile("questions.json")
	if err != nil {
		return err
	}
	var questions []seedQuestion
	if err := json.Unmarshal(data, &questions); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	validID := regexp.MustCompile(`^[a-f0-9]{24}$`)
	for _, q := range questions {
		if !validID.MatchString(q.ID) || seen[q.ID] {
			return fmt.Errorf("invalid or duplicate question ID: %q", q.ID)
		}
		seen[q.ID] = true
		if q.Image != "" && q.Image != "/questions/"+q.ID+"_1.jpg" {
			return fmt.Errorf("invalid question image: %s", q.Image)
		}
		correct := 0
		letters, texts := map[string]bool{}, map[string]bool{}
		for _, a := range q.Answers {
			text := normalizeTopicText(a.Text)
			if strings.TrimSpace(a.Letter) == "" || text == "" || letters[a.Letter] || texts[text] {
				return fmt.Errorf("invalid choices for %s", q.ID)
			}
			letters[a.Letter] = true
			texts[text] = true
			if a.Correct {
				correct++
			}
		}
		if correct != 1 {
			return fmt.Errorf("question %s must have one correct answer", q.ID)
		}
		if _, err := tx.Exec(`INSERT INTO question_contents(id,prompt,help_en,help_es,help_ru,image_path) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET prompt=excluded.prompt,help_en=excluded.help_en,help_es=excluded.help_es,help_ru=excluded.help_ru,image_path=excluded.image_path`, q.ID, q.Question, q.Help["en"], q.Help["es"], q.Help["ru"], q.Image); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM content_answers WHERE content_id=?`, q.ID); err != nil {
			return err
		}
		for _, a := range q.Answers {
			if _, err := tx.Exec(`INSERT INTO content_answers VALUES(?,?,?,?)`, q.ID, a.Letter, a.Text, a.Correct); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
