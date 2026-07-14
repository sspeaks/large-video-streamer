# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** Quality & Performance Engineer

## Core Context

Go + Nix HLS streaming app. Testing responsibilities span: `go test ./...` correctness, integration tests for detect→label→share→playback flow, labeling accuracy evaluation, share link validation, and performance benchmarks.

**Key constraint:** No sample media or real names in testdata. Fixtures use `group-01`, `group-02` etc. Tests use real SQLite (modernc.org/sqlite), not mocks.

**Benchmark:** `internal/labels.TestAutodetectSampleBenchmark` — precision/recall at ±20s tolerance. Cache under user cache dir, not repo.

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Mouse cast as Quality & Performance Engineer from The Matrix universe.

## Learnings

Initial setup complete. Focus: test coverage, labeling accuracy validation, share link correctness.
