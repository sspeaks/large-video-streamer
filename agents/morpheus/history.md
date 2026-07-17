# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** Product & Systems Lead

## Core Context

Self-hosted, auth-gated HLS video-streaming web app built with Go 1.22, Nix/NixOS, FFmpeg/ffprobe, SQLite, and net/http. Streams large barbershop-competition videos.

**Architecture:** The repo is a compiling skeleton with fixed package interfaces. Internals can be implemented in parallel. Source videos are always read-only; all writable state lives under the configured state directory.

**Primary focus:** Autonomous performance boundary detection (precision/recall benchmarks) + per-performance share links.

**Key packages:** `internal/config`, `internal/auth`, `internal/hls`, `internal/labels`, `internal/segment`, `internal/detect`, `internal/web`.

**Privacy constraint:** No real event/group/performer names in tests or docs. Use `group-01`, `group-02`, etc.

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Morpheus cast as Product & Systems Lead from The Matrix universe.

📌 Issue triage completed on 2026-07-16 — triaged #2 (Tank), #3 (Neo), #4 (Tank). Verified routing logic against ralph-triage.js template.

## Learnings

Initial setup complete. Architecture review pending — package interfaces in README are the ground truth.

**GitHub issue triage:** Issue inbox mechanism depends on base `squad` label presence, not on documentation. Ralph triage template is the authoritative routing logic. All three routed issues verified correct by Fact Checker.


📌 Team update (2026-07-16T12:38:13.560-07:00): PR #12 approved and merged. Issues #13–#14 routed per domain expertise (Trinity, Tank). Product roadmap decision (Q3 2026 priorities) merged to shared decisions; team consensus needed on implementation order and threshold-tuning semantics.


📌 Team update (2026-07-16T12:38:13.560-07:00): Design Review completed for #13–#14; contract approved. Both implementations merged (PR #15: Trinity path normalization 594ea645; PR #16: Tank safe HTTP errors 48b38c0). Parallel work model validated (zero conflicts). NixOS production-ready; security audit cleared.


## Issue #27 Triage — 2026-07-16

**Triaged:** Remove "Review priority" sort option from candidate-review UI (labeling page).

**Finding:** The issue title "candidates order of appearence lsit" refers to the `review-priority` sort option (added in commit 42937bc) which sorts candidates by composite priority (conflicts, low-confidence, multi-source, suggested-name). Users report it's confusing and doesn't add value.

**Decision:**
- **Implementation owner:** Tank (Application Engineer)
- **Reason:** Web-UX only, isolated to labeling page `candidateItems()` logic in `internal/labels/routes.go`. No detection algorithm, detection contract, or data-model changes.
- **Scope:** Remove `review-priority` option from sort dropdown + remove corresponding sort logic. Preserve candidate review/promote/reject/filter workflow.
- **Cross-domain impact:** None. Sort is pure UI-side JavaScript.

**Labels corrected:** `squad` kept, `squad:morpheus` removed, `squad:tank` added, `go:needs-research` removed.

**Triage comment:** https://github.com/sspeaks/large-video-streamer/issues/27#issuecomment-4998401448
