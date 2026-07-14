# Work Routing

How to decide who handles what.

## Routing Table

| Work Type | Route To | Examples |
|-----------|----------|----------|
| Architecture, feature scope, cross-domain design | Morpheus | API contract changes, package boundary decisions, RFC review, PR review spanning domains |
| Autonomous detection & labeling | Neo | Silence detection tuning, signal fusion, boundary algorithm, candidate accuracy, `internal/detect`, `internal/labels` |
| Streaming, HLS, FFmpeg, Nix/NixOS | Trinity | HLS segmentation, playlist correctness, `nix build`, NixOS module, `internal/segment`, `internal/hls`, `internal/config` |
| Application features, auth, sharing, SQLite, web UX | Tank | Share link generation, access tiers, login/logout, cookie sessions, SQLite schema/migrations, `internal/auth`, `internal/web` |
| Tests, benchmarks, labeling accuracy evaluation | Mouse | `go test ./...`, integration tests, precision/recall validation, edge cases, playback smoke tests |
| Session logging, decision merging | Scribe | Automatic — always background, never needs routing |
| Work queue, GitHub issues & PRs | Ralph | Triage untriaged `squad` issues, drive assigned work, track PR pipeline |
| RAI review, content safety, credential audit | Rai | Pre-ship RAI review, secret detection, SQL injection checks, PII in logs |
| Claim verification, pre-mortems | Fact Checker | Benchmark result verification, API existence checks, architectural devil's advocate |

## Issue Routing

| Label | Action | Who |
|-------|--------|-----|
| `squad` | Triage: analyze issue, assign `squad:{member}` label | Morpheus |
| `squad:morpheus` | Architecture, product priorities, cross-domain review | Morpheus |
| `squad:neo` | Detection algorithm, labeling, benchmark, `internal/detect`/`internal/labels` | Neo |
| `squad:trinity` | HLS pipeline, FFmpeg, Nix/NixOS, `internal/segment`/`internal/hls`/`internal/config` | Trinity |
| `squad:tank` | Auth, share links, SQLite, web UX, `internal/auth`/`internal/web` | Tank |
| `squad:mouse` | Tests, benchmarks, coverage, edge cases | Mouse |
| `squad:scribe` | Memory and log operations | Scribe |
| `squad:ralph` | Work queue management | Ralph |
| `squad:rai` | RAI/safety review | Rai |
| `squad:fact-checker` | Verification, fact-checking, pre-mortems | Fact Checker |

### How Issue Assignment Works

1. When a GitHub issue gets the `squad` label, **Morpheus** triages it — analyzing content, assigning the right `squad:{member}` label, and commenting with triage notes.
2. When a `squad:{member}` label is applied, that member picks up the issue in their next session.
3. Members can reassign by removing their label and adding another member's label.
4. The `squad` label is the "inbox" — untriaged issues waiting for Morpheus to triage.

## Domain Boundaries Quick Reference

```
                   Morpheus (lead)
                        |
          +-------------+-------------+-------------+
          |             |             |             |
        Neo           Trinity       Tank          Mouse
    (detection)     (streaming)   (app/auth)    (quality)
    internal/       internal/     internal/     go test
    detect          segment       auth          ./...
    internal/       internal/     internal/
    labels          hls           web
                    internal/
                    config
```

**Cross-domain work (needs Morpheus sign-off before implementing):**
- `internal/config.Config` struct changes — used by all packages
- `internal/labels` interface changes that affect `internal/auth` or `internal/web`
- New NixOS module options that change the runtime contract

## Rules

1. **Eager by default** — spawn all agents who could usefully start work in parallel.
2. **Scribe always runs** after substantial work, always as `mode: "background"`. Never blocks.
3. **Quick facts → coordinator answers directly.** Don't spawn an agent for "what port does the server run on?"
4. **When two agents could handle it**, pick the one whose domain is the primary concern.
5. **"Team, ..." → fan-out.** Spawn all relevant agents in parallel as `mode: "background"`.
6. **Anticipate downstream work.** If Neo ships a detection change, spawn Mouse to update accuracy tests simultaneously.
7. **Issue-labeled work** — when a `squad:{member}` label is applied, route to that member. Morpheus handles all `squad` (base label) triage.
8. **Pre-ship review** — Rai and Fact Checker run before any feature lands in `main`. Rai checks credentials/injection/PII; Fact Checker verifies benchmark claims.
