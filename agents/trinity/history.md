# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** Streaming & Platform Engineer

## Core Context

Self-hosted HLS video streamer packaged as a standalone Go + Nix flake (`github.com/sspeaks/large-video-streamer`). Exports `nixosModules.vidStreamer`. Dev shell includes Go, gopls, gotools, ffmpeg, mkvtoolnix.

**Key components:** `internal/segment` (ffmpeg HLS generation), `internal/hls` (serving), `internal/config` (env loading, state dir). NixOS module manages `hlsDir`, stale `.*.tmp` cleanup, `supplementaryGroups`, and optional `videoAccessGroup` ACLs.

**Current state:** Compiling skeleton — package interfaces are fixed. HLS and segmentation internals can be implemented.

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Trinity cast as Streaming & Platform Engineer from The Matrix universe.

## Learnings

Initial setup complete. Focus: HLS pipeline correctness and NixOS packaging reliability.

## 2026-07-16 — Issue #13: normalize relative diagnostic roots

Resolved by adding `filepath.Abs()` normalization in `config.Load()` for VideoDir, HLSDir, and StateDir immediately after path derivation and before dbPath computation. Errors surface with field context; no silent fallback. Added 6-case table-driven test covering relative inputs, default derived paths, and absolute-path invariants. All tests and Nix build green. PR #15 merged-ready.

**Key pattern:** normalize at config-load time, not at call sites — keeps redaction logic in detection.go simpler and guarantees all consumers see absolute paths.


📌 Team update (2026-07-16T12:38:13.560-07:00): Issue #13 (path normalization) completed. Config.Load() now normalizes VideoDir/StateDir/HLSDir to absolute paths via filepath.Abs(). All 16 config tests passing; full suite green (10 packages); Nix build clean. Merge 594ea645; issue #13 closed.
