# Squad Team

> vid-streamer — self-hosted, auth-gated HLS video-streaming web app with autonomous performance detection and labeling

## Coordinator

| Name | Role | Notes |
|------|------|-------|
| Squad | Coordinator | Routes work, enforces handoffs and reviewer gates. |

## Members

| Name | Role | Charter | Status |
|------|------|---------|--------|
| Morpheus | Product & Systems Lead | [charter](agents/morpheus/charter.md) | 🏗️ Active |
| Neo | Detection & Labeling Engineer | [charter](agents/neo/charter.md) | ⚛️ Active |
| Trinity | Streaming & Platform Engineer | [charter](agents/trinity/charter.md) | 🔧 Active |
| Tank | Application Engineer | [charter](agents/tank/charter.md) | 💻 Active |
| Mouse | Quality & Performance Engineer | [charter](agents/mouse/charter.md) | 🧪 Active |
| Scribe | Session Logger | [charter](agents/scribe/charter.md) | 📋 Silent |
| Ralph | Work Monitor | [charter](agents/ralph/charter.md) | 🔄 Monitor |
| Rai | RAI Reviewer | [charter](agents/Rai/charter.md) | 🛡️ Background |
| Fact Checker | Verifier | [charter](agents/fact-checker/charter.md) | 🔍 Background |

## Project Context

- **Project:** vid-streamer (repo: large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Stack:** Go 1.22, Nix/NixOS, FFmpeg/ffprobe, HLS, SQLite (modernc.org/sqlite), net/http
- **Repo:** `github.com/sspeaks/large-video-streamer`
- **Purpose:** Self-hosted, auth-gated website that streams large barbershop-competition videos over HLS. Packaged as a standalone Go + Nix flake with a reusable `nixosModules.vidStreamer` NixOS module.

### Product Priorities

1. **Autonomous performance boundary detection** — precision/recall benchmarks against labeled ground truth at ±20s tolerance
2. **Human-correction UI** — review, adjust, and confirm detected boundaries
3. **Per-performance share links** — stable URLs with multiple share/access options
4. **Auth-gated access** — cookie-based auth, credential files, reliable NixOS deployment

### Stack Details

| Component | Details |
|-----------|---------|
| Language | Go 1.22 |
| Packaging | Nix flake + NixOS module (`services.vidStreamer`) |
| Video pipeline | FFmpeg/ffprobe, HLS segmentation |
| Database | modernc.org/sqlite (no CGO), `app.db` under state directory |
| Auth | Cookie-based; `LOGIN_USER`/`LOGIN_PASS`/`COOKIE_SECRET` from files or env |
| Flat-file fallback | `VIDSTREAMER_FLAT_FILE_STATE=1` restores legacy `shares.json` + label JSON sidecars |

### Privacy & Data Constraints

- Never commit sample media, real event/group/performer names, or personal competition data
- Use anonymized placeholders (`group-01`, `group-02`, etc.) in all tests and documentation
- No PII in logs, API responses, or committed files
- Source video directory (`VIDEO_DIR`) is always read-only
- All writable state belongs under the configured state directory
