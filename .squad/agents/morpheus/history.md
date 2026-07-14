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

## Learnings

Initial setup complete. Architecture review pending — package interfaces in README are the ground truth.
