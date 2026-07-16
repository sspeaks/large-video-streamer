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

## 2026-07-16 — Issue #14: Sanitize label API errors

**PR:** https://github.com/sspeaks/large-video-streamer/pull/16

**Commit:** f3bc6c9

**What:** Added `safeHTTPError` method to `Server` that logs redacted internal diagnostics and returns stable bounded public messages. Replaced 9 raw `err.Error()` → 5xx leaks across all label/MKV handlers. Added `logger detectionLogger` field (default `log.Default()`) to `Server` for testability.

**Learned:** Reusing the existing `detectionLogger` interface + `redactDetectionDiagnostic` kept the change surgical — no new interfaces or packages needed. `errLabelStore` test double with injected path sentinels gives fast, deterministic coverage of every affected handler without requiring real filesystem failures.


📌 Team update (2026-07-16T12:38:13.560-07:00): Issue #14 (safe HTTP error handling) completed. Routes now use safeHTTPError helper (no cross-package expansion). Logger field injected for PII masking in tests. All Go tests passed. Merge 48b38c0; issue #14 closed. Design contract validated.
