---
updated_at: 2026-07-16T13:33:54.724-07:00
focus_area: Autonomous performance labeling + per-performance sharing
active_issues: []
---

# What We're Focused On

**Current queue:** No open GitHub issues or PRs. Ralph is in idle-watch after PRs #12, #15, and #16 merged.

**Phase 1: Autonomous performance boundary detection**
Neo is leading detection algorithm work in `internal/detect` and `internal/labels`. Goal: measurable precision/recall at ±20s tolerance against ground-truth timestamps. Mouse owns the evaluation harness and coverage.

**Phase 2: Per-performance share links**
Tank is building share link generation with stable URLs and multiple access options. Morpheus signs off on the share link URL design and access tier semantics.

Both tracks run in parallel once the skeleton internals are implemented. Trinity keeps the HLS pipeline and NixOS packaging sound while domain work proceeds.
