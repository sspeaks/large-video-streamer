# Squad Decisions

## Active Decisions

### 2026-07-16T12:38:13.560-07:00: Design contract — #13/#14 parallel path normalization and safe HTTP error handling
**By:** Morpheus (Product & Systems Lead)

**What:** Design Review contract for issues #13 (path normalization) and #14 (safe HTTP error handling).

**Decisions:**
- **D1:** `Config.Load()` normalizes VideoDir, StateDir, HLSDir to absolute paths via `filepath.Abs()`. Relative paths accepted and resolved against CWD. NixOS production mode unaffected (Nix `types.path` already absolute).
- **D2:** Routes use package-internal `safeHTTPError` helper in `routes.go` (Tank). Reuses existing `redactDetectionDiagnostic` function. Writes bounded public message to HTTP response, logs redacted error server-side.
- **D3:** Server struct gains `logger` field (detectionLogger interface, defaults to `log.Default()`). Tests inject buffer logger to verify no sensitive data in logs.
- **D4:** No cross-package API expansion. Both issues stay within file boundaries. No new shared utility package.
- **D5:** Zero file conflicts: Trinity edits only `internal/config/`; Tank edits only `internal/labels/` (routes, labels.go).

**Why:** Enables #13 and #14 to proceed in parallel without conflicts. Path normalization ensures diagnostic root safety. Safe HTTP error handling prevents information disclosure. Logger injection enables test coverage for sensitive data masking.

**File Ownership:**
- Trinity: `internal/config/config.go`, `internal/config/config_test.go` (path normalization)
- Tank: `internal/labels/routes.go`, `internal/labels/routes_test.go`, `internal/labels/labels.go` (safe errors + logger)
- Shared read-only: `internal/labels/detection.go` (redaction function)

**Test Matrix:** Relative→absolute normalization, missing directory rejection, safe 500 responses (no paths), safe 400 responses (user-input validation). All tests use anonymized show names; no .env files read.

**Validation:** `go test ./internal/config/ -v -run TestLoad`, `go test ./internal/labels/ -v -run TestRoutes`, `go test ./...`

**Non-Goals:** No renaming redaction function, no exported helpers, no new packages, no detection algorithm changes.

**Status:** Approved by Design Review participants (Tank, Trinity, Neo, Mouse, Morpheus). Implementation may proceed on separate branches.

## Governance

- All meaningful changes require team consensus
- Document architectural decisions here
- Keep history focused on work, decisions focused on direction
