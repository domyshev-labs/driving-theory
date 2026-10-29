# Driving Theory

Practice driving theory by subject, with Spanish and English questions and Russian navigation and explanations. The application includes a React frontend and a Go HTTP API backed by SQLite.

## Features

- Subject sections, topic search and stable question identities.
- Shared progress when a question appears in multiple topics.
- Full topic runs, separate batches of new questions and mistake reviews.
- Answer history and topic statistics with date filters.
- Email-code authentication and individual saved progress.
- Configurable bilingual question and answer display.

## Local development

Requires Node.js 22.13+ and Go 1.24+.

```bash
npm ci
npm run dev:api
```

In a second terminal:

```bash
npm run dev
```

Open `http://127.0.0.1:5173`. The development server proxies `/api` to port 8080. Login codes appear in the API console when `EMAIL_MODE=log`. SQLite data is written to `data/road-rules.db`, which Git ignores. See `.env.example` for configuration; environment variables must be exported by your shell or process manager.

## Content

`backend/seed/questions.json` contains one record per unique question. Each record has a stable `id`, wording, explanations, answer choices and an optional image path. IDs are opaque and must not be recomputed when editing content.

Images use `public/questions/<id>_1.jpg`. Further images can use `_2.jpg`, `_3.jpg` and so on; the current interface displays the first image. Questions without an illustration have an empty image path.

`materials/taxonomy.json` defines sections and topics. Editorial assignments are in `materials/topic-overrides.json`, and Russian question-list titles are in `materials/question-titles-ru.json`. Regenerate both topic libraries with:

```bash
python3 scripts/rebuild-library.py
```

## Verification

```bash
python3 scripts/validate-content.py
python3 scripts/audit-topics.py
python3 -m unittest discover -s scripts -p 'test_*.py'
node --experimental-strip-types --test scripts/test-*.mjs
npm run lint
npm run build
go test ./backend/...
go vet ./backend/...
```

## Container

```bash
docker build -t driving-theory .
docker run --rm -p 127.0.0.1:5050:8080 \
  -v driving-theory-data:/app/data \
  -e SESSION_SECRET='replace-with-a-long-random-secret' \
  -e EMAIL_MODE=log \
  -e COOKIE_SECURE=false \
  driving-theory
```

The non-root application serves the frontend and API on port 8080. Persist `/app/data`. For HTTPS installations set `COOKIE_SECURE=true`, configure a strong session secret and select the email delivery settings appropriate for your environment.

Before upgrading an existing database, create and verify a consistent SQLite backup. An incompatible layout is rejected before content is imported.

See [the backend guide](docs/backend.md) for authentication, progress and API details.
