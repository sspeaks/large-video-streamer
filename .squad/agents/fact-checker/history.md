# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** Devil's Advocate & Verification Agent

## Core Context

Self-hosted HLS streaming app. Key claims to verify in this project:

- Detection accuracy benchmarks (precision/recall numbers must cite real benchmark runs)
- Package version claims (Go 1.22, modernc.org/sqlite v1.36.1 — verify against go.mod)
- NixOS module option claims (verify against flake.nix)
- Share link stability claims (verify against actual implementation)

**Hard rule:** Never fabricate detection benchmark results. All accuracy figures must cite a real `TestAutodetectSampleBenchmark` run.

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Fact Checker activated as Verifier.

📌 Issue triage verification completed on 2026-07-16 — verified Morpheus routing claims against live GitHub state, workflow definitions, and Ralph triage template. Discovered definitive root cause: base `squad` label presence (not documentation) controls inbox membership.

## Learnings

Initial setup complete. Key project claims identified above.

**GitHub issue verification:** No automatic intake workflow exists in `.github/workflows/*`. Issue inbox filtering depends on base `squad` label. The `## Issue Source` documentation convention does not affect functional triage behavior. All three routed issues (#2 Tank, #3 Neo, #4 Tank) verified correct per domain expertise.


📌 Team update (2026-07-16T12:38:13.560-07:00): PR #12 verification complete (no blocking contradictions). Issue routing verified against domain expertise. GitHub issue triage workflow mechanics documented (base squad label controls inbox membership, not documentation conventions).


📌 Team update (2026-07-16T12:38:13.560-07:00): Security audit cleared for both PR #15 (path normalization) and PR #16 (safe HTTP errors). Path handling in Config.Load() verified; safeHTTPError prevents information disclosure. Logger injection testing prevents PII leaks. No credential handling issues.
