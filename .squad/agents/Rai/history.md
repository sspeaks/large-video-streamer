# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** RAI Reviewer

## Core Context

Self-hosted, auth-gated HLS streaming app. Key RAI concerns for this project:

- **Credentials:** `LOGIN_USER`, `LOGIN_PASS`, `COOKIE_SECRET` — must come from files/env, never hardcoded
- **Path traversal:** `VIDEO_DIR` and `HLS_DIR` paths must be validated — user-controlled input must not escape configured directories
- **PII:** No real performer names, event names, or competition data in logs, API responses, or committed files
- **SQLite injection:** All label/share/auth queries must use parameterized statements
- **Automation transparency:** Autonomous detection labels must be distinguishable from human-confirmed labels

This is a web application (not an AI/ML project by RAI classification). Check suite: Security + privacy + content (web application tier).

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Rai activated as RAI Reviewer.

## Learnings

Initial setup complete. Project-specific RAI concerns documented above.
