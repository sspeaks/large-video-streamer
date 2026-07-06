#!/usr/bin/env bash
# Temporary passwordless local dev server. Usage: ./dev.sh [VIDEO_DIR]
exec nix run "$(dirname "$0")"#dev -- "$@"
