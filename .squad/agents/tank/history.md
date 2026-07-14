# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** Application Engineer

## Core Context

Go + Nix self-hosted HLS streaming app. Key app-layer responsibilities: auth (cookie-based, credential files), SQLite state (`modernc.org/sqlite`, no CGO), share links for individual performances, web UX (`internal/web` with embedded assets).

**Legacy migration:** Startup imports `shares.json` and `labels/*.labels.json` into SQLite idempotently. Rollback via `VIDSTREAMER_FLAT_FILE_STATE=1`. Cookie secret persisted under state directory.

**Current state:** Skeleton — package interfaces fixed. Auth and share link internals to be implemented.

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Tank cast as Application Engineer from The Matrix universe.

## Learnings

Initial setup complete. Focus: share links, auth hardening, SQLite migrations.
