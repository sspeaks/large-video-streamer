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
