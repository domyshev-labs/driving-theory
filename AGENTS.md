# Repository instructions

- Write code comments and repository documentation in English.
- Never use `git push --force`.
- Keep question IDs stable across wording, image and topic edits.
- Store question images as `public/questions/<id>_<number>.jpg`, starting at one.
- Run content validation, topic audits, frontend checks and backend tests after data or behavior changes.
- Before any production deployment, create a consistent SQLite online backup and verify `PRAGMA quick_check` and `PRAGMA foreign_key_check`. Stop if either check fails.
- Do not copy only a live database file while WAL mode is active.
