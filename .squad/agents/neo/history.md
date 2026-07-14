# Project Context

- **Project:** vid-streamer (large-video-streamer)
- **User:** Seth Speaks
- **Created:** 2026-07-14
- **Role:** Detection & Labeling Engineer

## Core Context

Self-hosted HLS video streamer focused on autonomously detecting barbershop-competition performance boundaries within large video files. Primary signal: ffmpeg silence detection (`internal/detect`). Candidate boundaries stored in SQLite via `internal/labels`. Human-correction UI allows users to confirm or adjust detected boundaries.

**Benchmark:** `internal/labels.TestAutodetectSampleBenchmark` — evaluates precision/recall at ±20s tolerance against a local sample folder. Anonymizes rows as `group-01`, `group-02`. Never commits sample media or real names.

**Current state:** Skeleton implementation. Detection algorithm and accuracy improvements are the primary initial focus.

## Recent Updates

📌 Team initialized on 2026-07-14T14:49:22.465-07:00 — Neo cast as Detection & Labeling Engineer from The Matrix universe.

## Learnings

Initial setup complete. Focus: autonomous performance boundary detection accuracy.
