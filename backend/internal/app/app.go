package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/resend/resend-go/v3"
)

const sessionCookie = "rrt_session"

type App struct {
	cfg    Config
	db     *sql.DB
	logger *slog.Logger
	email  EmailSender
	mux    *http.ServeMux
}

type user struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type contextKey string

const userContextKey contextKey = "user"

func New(cfg Config, logger *slog.Logger) (*App, error) {
	db, err := openDatabase(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	var sender EmailSender = logEmailSender{logger: logger}
	if cfg.EmailMode == "resend" {
		sender = resendEmailSender{client: resend.NewClient(cfg.ResendAPIKey), from: cfg.EmailFrom}
	}
	application := &App{cfg: cfg, db: db, logger: logger, email: sender, mux: http.NewServeMux()}
	application.routes()
	return application, nil
}

func (app *App) Close() error { return app.db.Close() }

func (app *App) Handler() http.Handler {
	return app.recoverPanic(app.securityHeaders(app.logRequests(app.mux)))
}

func (app *App) routes() {
	app.mux.HandleFunc("GET /api/health", app.health)
	app.mux.HandleFunc("GET /api/releases", app.releases)
	app.mux.Handle("GET /api/statistics/linear", app.requireUser(http.HandlerFunc(app.linearStatistics)))
	app.mux.Handle("GET /api/statistics/topics", app.requireUser(http.HandlerFunc(app.topicStatistics)))
	app.mux.HandleFunc("POST /api/auth/request-code", app.requestCode)
	app.mux.HandleFunc("POST /api/auth/verify-code", app.verifyCode)
	app.mux.Handle("GET /api/auth/me", app.requireUser(http.HandlerFunc(app.me)))
	app.mux.Handle("POST /api/auth/logout", app.requireUser(http.HandlerFunc(app.logout)))
	app.mux.Handle("GET /api/questions/progress", app.requireUser(http.HandlerFunc(app.questionKnowledge)))
	app.mux.Handle("GET /api/topics/progress", app.requireUser(http.HandlerFunc(app.topicSummaries)))
	app.mux.Handle("GET /api/topics/{topicID}", app.requireUser(http.HandlerFunc(app.topic)))
	app.mux.Handle("POST /api/topics/{topicID}/answers", app.requireUser(http.HandlerFunc(app.answerTopic)))
	app.mux.Handle("POST /api/topics/{topicID}/new", app.requireUser(http.HandlerFunc(app.startNewTopicQuestions)))
	app.mux.Handle("POST /api/topics/{topicID}/error-reviews", app.requireUser(http.HandlerFunc(app.startTopicErrorReview)))
	app.mux.Handle("POST /api/topics/{topicID}/reviews/{runID}", app.requireUser(http.HandlerFunc(app.startTopicReview)))
	app.mux.Handle("POST /api/topics/{topicID}/reset", app.requireUser(http.HandlerFunc(app.resetTopic)))

	app.mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		app.writeError(w, http.StatusNotFound, "Endpoint not found")
	})

	static := app.cfg.StaticDir
	if info, err := os.Stat(static); err == nil && info.IsDir() {
		files := http.FileServer(http.Dir(static))
		app.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(static, filepath.Clean(r.URL.Path))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join(static, "index.html"))
		})
	}
}

func (app *App) health(w http.ResponseWriter, _ *http.Request) {
	if err := app.db.Ping(); err != nil {
		app.writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	app.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (app *App) requestCode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if !app.decodeJSON(w, r, &input) {
		return
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		app.writeError(w, http.StatusUnprocessableEntity, "Введите корректный email")
		return
	}
	now := time.Now().UTC()
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	var recent int
	if err := app.db.QueryRow(`SELECT count(*) FROM login_codes WHERE email=? AND created_at>?`, email, now.Add(-time.Hour).Format(time.RFC3339Nano)).Scan(&recent); err != nil {
		app.serverError(w, err)
		return
	}
	if recent >= 5 {
		app.writeError(w, http.StatusTooManyRequests, "Слишком много запросов. Попробуйте позже")
		return
	}
	if err := app.db.QueryRow(`SELECT count(*) FROM login_codes WHERE request_ip=? AND created_at>?`, ip, now.Add(-time.Hour).Format(time.RFC3339Nano)).Scan(&recent); err != nil {
		app.serverError(w, err)
		return
	}
	if recent >= 20 {
		app.writeError(w, http.StatusTooManyRequests, "Слишком много запросов. Попробуйте позже")
		return
	}
	var lastCreated string
	err = app.db.QueryRow(`SELECT created_at FROM login_codes WHERE email=? ORDER BY created_at DESC LIMIT 1`, email).Scan(&lastCreated)
	if err == nil {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, lastCreated); parseErr == nil && now.Sub(parsed) < time.Minute {
			app.writeError(w, http.StatusTooManyRequests, "Новый код можно запросить через минуту")
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		app.serverError(w, err)
		return
	}

	code, err := randomDigits(6)
	if err != nil {
		app.serverError(w, err)
		return
	}
	id, _ := randomToken(18)
	if _, err := app.db.Exec(`INSERT INTO login_codes(id,email,code_hash,request_ip,expires_at,created_at) VALUES(?,?,?,?,?,?)`, id, email, app.codeHash(email, code), ip, now.Add(10*time.Minute).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		app.serverError(w, err)
		return
	}
	if err := app.email.SendLoginCode(r.Context(), email, code); err != nil {
		_, _ = app.db.Exec(`UPDATE login_codes SET consumed_at=? WHERE id=?`, now.Format(time.RFC3339Nano), id)
		app.logger.Error("send login code", "email", email, "error", err)
		app.writeError(w, http.StatusBadGateway, "Не удалось отправить письмо")
		return
	}
	app.writeJSON(w, http.StatusAccepted, map[string]string{"message": "Код отправлен"})
}

func (app *App) verifyCode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !app.decodeJSON(w, r, &input) {
		return
	}
	email, err := normalizeEmail(input.Email)
	if err != nil || len(input.Code) != 6 {
		app.writeError(w, http.StatusUnauthorized, "Неверный или просроченный код")
		return
	}
	var id, hash, expires string
	var attempts int
	err = app.db.QueryRow(`SELECT id,code_hash,expires_at,attempts FROM login_codes WHERE email=? AND consumed_at IS NULL ORDER BY created_at DESC LIMIT 1`, email).Scan(&id, &hash, &expires, &attempts)
	if err != nil {
		app.writeError(w, http.StatusUnauthorized, "Неверный или просроченный код")
		return
	}
	expiresAt, parseErr := time.Parse(time.RFC3339Nano, expires)
	if parseErr != nil || time.Now().UTC().After(expiresAt) || attempts >= 5 || !hmac.Equal([]byte(hash), []byte(app.codeHash(email, input.Code))) {
		_, _ = app.db.Exec(`UPDATE login_codes SET attempts=attempts+1 WHERE id=?`, id)
		app.writeError(w, http.StatusUnauthorized, "Неверный или просроченный код")
		return
	}

	now := time.Now().UTC()
	userID, _ := randomToken(18)
	tx, err := app.db.Begin()
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO users(id,email,created_at) VALUES(?,?,?) ON CONFLICT(email) DO NOTHING`, userID, email, now.Format(time.RFC3339Nano)); err != nil {
		app.serverError(w, err)
		return
	}
	var current user
	if err = tx.QueryRow(`SELECT id,email FROM users WHERE email=?`, email).Scan(&current.ID, &current.Email); err != nil {
		app.serverError(w, err)
		return
	}
	token, _ := randomToken(32)
	if _, err = tx.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`, tokenHash(token), current.ID, now.Add(30*24*time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		app.serverError(w, err)
		return
	}
	if _, err = tx.Exec(`UPDATE login_codes SET consumed_at=? WHERE id=?`, now.Format(time.RFC3339Nano), id); err != nil {
		app.serverError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.serverError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: app.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 60 * 60})
	app.writeJSON(w, http.StatusOK, current)
}

func (app *App) me(w http.ResponseWriter, r *http.Request) {
	app.writeJSON(w, http.StatusOK, r.Context().Value(userContextKey).(user))
}

func (app *App) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_, _ = app.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: app.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (app *App) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			app.writeError(w, http.StatusUnauthorized, "Требуется вход")
			return
		}
		var current user
		var expires string
		err = app.db.QueryRow(`SELECT users.id,users.email,sessions.expires_at FROM sessions JOIN users ON users.id=sessions.user_id WHERE sessions.token_hash=?`, tokenHash(cookie.Value)).Scan(&current.ID, &current.Email, &expires)
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, expires)
		if err != nil || parseErr != nil || time.Now().UTC().After(expiresAt) {
			app.writeError(w, http.StatusUnauthorized, "Сессия истекла")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, current)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 254 {
		return "", errors.New("invalid email")
	}
	return value, nil
}

func randomDigits(length int) (string, error) {
	digits := make([]byte, length)
	for index := range digits {
		value, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[index] = '0' + byte(value.Int64())
	}
	return string(digits), nil
}

func randomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (app *App) codeHash(email, code string) string {
	mac := hmac.New(sha256.New, []byte(app.cfg.SessionSecret))
	mac.Write([]byte(email + ":" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (app *App) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		app.writeError(w, http.StatusUnsupportedMediaType, "Требуется JSON")
		return false
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		app.writeError(w, http.StatusBadRequest, "Некорректный JSON")
		return false
	}
	return true
}
func (app *App) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (app *App) writeError(w http.ResponseWriter, status int, message string) {
	app.writeJSON(w, status, map[string]string{"error": message})
}
func (app *App) serverError(w http.ResponseWriter, err error) {
	app.logger.Error("request failed", "error", err)
	app.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
}

func (app *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
func (app *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		app.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started).String())
	})
}
func (app *App) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				app.logger.Error("panic", "value", fmt.Sprint(value))
				app.writeError(w, 500, "Внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
