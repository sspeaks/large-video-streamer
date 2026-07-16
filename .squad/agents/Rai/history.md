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

## 2026-07-16 — PR #12 audit (issue #11: diagnostic redaction)

**Verdict:** 🟢 Green — merged-safe.

**Scope:** Detection job status API redaction. Verified no raw diagnostics reach browser-visible JSON. No credentials, injection, path traversal, or PII introduced.

**Key findings:**
- Public status `error` field now hardcoded to a stable safe message; raw diagnostic emitted server-side only after path/filename/show redaction.
- Redaction logic: longest-first sort, three slash variants, per-show and per-configured-root coverage.
- Full test suite passes on PR branch.

**Advisory (🟡, pre-existing, not blocking):**
1. Several non-detection HTTP handlers in `routes.go` pass raw store errors to `http.Error()` — potential path exposure to authenticated clients. Flagged for a future issue.
2. `safeDiagnosticRoot()` skips relative configured paths — edge case, low risk.

**No lockout triggered.**

## 2026-07-16 — PR #16 audit (issue #14: label API error sanitization)

**Verdict:** 🟢 Green — merged-safe.

**Scope:** All 9 store/filesystem 5xx error paths in label/MKV handlers. Verified all now use `safeHTTPError` helper with bounded public constants; internal diagnostics logged server-side via `redactDetectionDiagnostic`.

**Key findings:**
- All 9 `http.Error(w, err.Error(), 5xx)` calls replaced across `handleLabelsGet`, `handleLabelsPost`, `handleLabelsImport`, `handleLabelsExport`, `handleMKVImport`, `handleMKVEmbed`.
- Remaining `http.Error(w, err.Error(), ...)` calls are all 400 validation — echo user-supplied input or static strings only; no internal paths. Intentional per design decision d35790c1.
- Remaining 5xx call (`handleLabelsPage`) uses hardcoded literal "failed to render labels page" — safe.
- `logger` field defaults to `log.Default()` in `NewServer`; nil panic not possible via normal construction path.
- No credentials, PII, path data, or injection surface introduced in tests or comments.
- All 10 test packages pass. No CI workflows configured (squad-management only).

**Advisory (🟡, pre-existing, not blocking):**
1. `safeDiagnosticRoot` skips empty/relative roots — pre-existing, flagged in PR #12 review.
2. MKV tool-failure tests discard log buffer — server-side logging not asserted for those 2 paths; critical response-body property IS tested.

**No lockout triggered.**

## 2026-07-16 — PR #15 audit (issue #13: normalize config paths to absolute)

**Verdict:** 🟢 Green — merged-safe.

**Scope:** `internal/config/config.go` path normalization (`filepath.Abs` on VideoDir, HLSDir, StateDir) and 6-case table-driven tests.

**Key findings:**
- `filepath.Abs()` applied only to operator-configured env vars, not to user URL input — no new traversal surface.
- `filepath.Abs` error messages ("VideoDir: resolve absolute path: <os error>") contain no configured path content, credentials, or PII; errors surface at startup only.
- VIDEO_DIR read-only contract unchanged; downstream usage not modified.
- StateDir/HLSDir write semantics correct; `dbPath` now also benefits from absolute `stateDir` (positive side effect).
- Directly closes the `safeDiagnosticRoot` relative-path gap flagged in PR #12 review.
- No credentials, PII, or real paths in tests (`VIDSTREAMER_DEV_NOAUTH=1`, placeholder names only).
- All 10 test packages pass.

**Advisory (🟡, acknowledged in PR, not blocking):**
1. `dbPath` not explicitly normalized — SQLite handles relative paths; dbPath not used in redaction; acceptable.

**No lockout triggered.**
