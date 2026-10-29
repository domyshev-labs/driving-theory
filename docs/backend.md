# Backend design

## Authentication

A six-digit email code expires after ten minutes. Codes are stored as HMAC-SHA-256 digests, verification attempts are limited, and requests are rate-limited by email and IP. A successful login creates a 30-day session in an HttpOnly, SameSite cookie; only the session-token hash is stored. The development sender logs codes. Resend delivery requires explicit runtime configuration.

## Content and progress

`question_contents` stores wording, explanations and an optional image path under a stable question ID. `content_answers` stores answer choices. Topic labels and question memberships are defined by the embedded topic library. A question can belong to several topics without duplicating its content.

Full runs freeze their question membership in `topic_run_members`. Answers are immutable within a run. A new full run preserves previous runs, while additional questions use independent new-question batches. Mistake reviews have separate run IDs and do not overwrite normal-training history. Inherited knowledge is stored separately from actual answer events.

`question_progress` derives the latest normal answer per user and question. `question_current_progress` also considers completed review answers. Current coverage follows the question ID across topics. Historical memberships remain available when topic assignments change.

`imported_answers` accepts previously recorded answer events during a separately prepared database upgrade. These events retain their timestamps, selected text, result and topic. Imported review answers appear in history without replacing normal-training knowledge.

## API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| GET | `/api/health` | Database health |
| POST | `/api/auth/request-code` | Request a login code |
| POST | `/api/auth/verify-code` | Start a session |
| GET | `/api/auth/me` | Current user |
| POST | `/api/auth/logout` | Revoke the session |
| GET | `/api/topics/progress` | Topic coverage and run history |
| GET | `/api/topics/{id}` | Questions for a topic or saved run |
| POST | `/api/topics/{id}/answers` | Validate and save an answer |
| POST | `/api/topics/{id}/reset` | Start a fresh full run |
| POST | `/api/topics/{id}/new` | Start or resume a new-question batch |
| POST | `/api/topics/{id}/reviews/{runID}` | Start or resume a mistake review |
| GET | `/api/questions/progress` | Feedback for previously answered questions |
| GET | `/api/statistics/linear` | Paginated answer history |
| GET | `/api/statistics/topics` | Topic statistics with date filters |
| GET | `/api/releases` | Release highlights |

Except for health, release highlights and login requests, all API routes require a session. Topic requests return answer choices without correctness flags. Feedback is exposed only after a user has answered. Authorization scopes answers, runs, reviews and statistics to the current user.

Topic questions use a stable text-based answer order and canonical-ID question order. Display numbers are local to the topic; saved answers use question IDs. Pagination totals cover the full selected period. Timestamps are stored in UTC and displayed in the browser's local timezone.

## Storage safety

SQLite uses WAL mode and foreign keys. Before an upgrade, use the SQLite online backup API or `.backup`; copying only the live `.db` file is unsafe. Verify both `PRAGMA quick_check` and `PRAGMA foreign_key_check`. An incompatible schema is rejected before any content update. Do not run two application instances against different copies of the same user's progress.
