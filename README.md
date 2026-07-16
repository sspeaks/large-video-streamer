# vid-streamer

`vid-streamer` is a self-hosted, auth-gated HLS video-streaming website packaged as a standalone Go + Nix flake. It will expose a reusable NixOS module at `services.vidStreamer` for use from another `nixos-config` flake.

This repository is currently a compiling skeleton. Package interfaces are fixed so downstream agents can implement internals in parallel without editing `main.go`.

## Build and development

```sh
nix build
nix develop
nix develop -c go build ./...
nix develop -c go vet ./...
```

The dev shell includes Go, `gopls`, `gotools`, `ffmpeg`, and `mkvtoolnix`.

### Optional auto-detect benchmark

`internal/labels` includes a skipped-by-default benchmark for evaluating boundary
suggestions against a local sample folder. The folder is expected to contain
`timestamps.txt`, `semi-finals order.txt`, and `semi_finals.mkv`; no sample media
or real event names should be committed.

```sh
VIDSTREAMER_AUTODETECT_SAMPLE_DIR=/path/to/sample \
  go test ./internal/labels -run TestAutodetectSampleBenchmark -v -count=1
```

The benchmark anonymizes report rows as `group-01`, `group-02`, etc., caches raw
signal output under the user cache directory, and reports precision/recall
against a ±20 second tolerance.

## NixOS module configuration

The recommended deployment method is via the NixOS module `services.vidStreamer`, which handles systemd service setup, state directories, file permissions, and secret management.

### Minimal example

```nix
{
  inputs.vid-streamer.url = "github:sspeaks/large-video-streamer";

  outputs = { nixpkgs, vid-streamer, ... }: {
    nixosConfigurations.host = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        vid-streamer.nixosModules.vidStreamer
        {
          services.vidStreamer = {
            enable = true;
            package = vid-streamer.packages.x86_64-linux.default;
            videoDir = "/srv/videos";
            loginUserFile = "/run/secrets/vid-streamer-user";
            loginPassFile = "/run/secrets/vid-streamer-pass";
          };
        }
      ];
    };
  };
}
```

### Required options

- **`enable`** — set to `true` to activate the service.
- **`package`** — must provide the `vid-streamer` binary (e.g., from `vid-streamer.packages.x86_64-linux.default`).
- **`videoDir`** — path to a directory containing `.mkv` files (read-only source, must be readable by the service).
- **`loginUserFile`** — path to a file containing the login username; **required unless `noAuth = true`**. Do not set if running with `noAuth = true`.
- **`loginPassFile`** — path to a file containing the login password; **required unless `noAuth = true`**. Do not set if running with `noAuth = true`.

### Optional options with defaults

| Option | Type | Default | Purpose |
|--------|------|---------|---------|
| `hlsDir` | path | `/var/lib/vid-streamer/hls` | Writable directory for generated HLS playlists and segments |
| `listenAddr` | string | `127.0.0.1:8080` | HTTP server bind address and port |
| `openFirewall` | bool | `false` | Open the TCP port from `listenAddr` in the local firewall |
| `user` | string | `vid-streamer` | UNIX user running the service |
| `group` | string | `vid-streamer` | UNIX group running the service |
| `supplementaryGroups` | list | `[]` | Additional groups for the service process (e.g., `["users"]` if videoDir is group-readable by another group) |
| `videoAccessGroup` | string or `null` | `null` | If set, the module grants this group read access to videos in videoDir via ACLs and adds it to `supplementaryGroups` |
| `noAuth` | bool | `false` | Run without authentication (trusted networks only); disables login credential requirements |
| `segmentOnStart` | bool | `true` | Segment all videos in videoDir into HLS when the service starts |
| `legacyFlatFileState` | bool | `false` | Use legacy flat-file state (shares.json and labels/*.labels.json) instead of SQLite; intended only for rollback after migration |
| `cookieSecretFile` | path or `null` | `null` | Optional path to a file containing a base64-encoded, ≥32-byte cookie-signing secret. If left unset, the server auto-generates one on first start and persists it in the systemd state directory. |

### Conditional requirements

- When `noAuth = false` (the default), `loginUserFile` and `loginPassFile` are **required**; the module asserts their presence.
- When `noAuth = true`, authentication is disabled entirely and login credentials are ignored.
- `cookieSecretFile` is always optional. When unset and `noAuth = false`, the server auto-generates and persists a cookie secret in its state directory (no manual management needed).

### Advanced configuration

```nix
{
  services.vidStreamer = {
    enable = true;
    package = vid-streamer.packages.x86_64-linux.default;
    videoDir = "/srv/videos";
    videoAccessGroup = "users";  # Grant the 'users' group read access to videos
    listenAddr = "0.0.0.0:8080";  # Listen on all interfaces
    openFirewall = true;
    loginUserFile = "/run/secrets/vid-streamer-user";
    loginPassFile = "/run/secrets/vid-streamer-pass";
    cookieSecretFile = "/run/secrets/vid-streamer-cookie-secret";  # Optional; omit for auto-generation
    segmentOnStart = false;  # Do not segment on startup; segment manually on demand
  };
}
```

### State management

- **SQLite state** (default) — The service stores labels, shares, and internal state in SQLite under the systemd `StateDirectory` (`/var/lib/vid-streamer/` by default).
- **HLS output** — Generated HLS playlists and segments are written to `hlsDir`, created with service ownership before the service starts.
- **Legacy flat-file rollback** — If you previously used flat-file state and need to roll back after migration to SQLite, set `legacyFlatFileState = true`. The flat-file imports remain in place so rollback is always possible.
- **Stale temporary directories** — On each service start, the module removes any stale `.*.tmp` HLS directories.

## Development and non-NixOS environments

For local development or non-NixOS environments, the Go binary reads environment variables directly via `internal/config.Load`:

- **`VIDEO_DIR`** (required) — Read-only source folder of videos.
- **`LISTEN_ADDR`** (optional, defaults to `127.0.0.1:8080`) — HTTP server bind address and port.
- **`HLS_DIR`** (optional, defaults to `$STATE_DIRECTORY/hls` or `state/hls`) — Writable HLS output folder.
- **`LOGIN_USER`** or **`LOGIN_USER_FILE`** — Login username or path to file containing it (file variant takes precedence).
- **`LOGIN_PASS`** or **`LOGIN_PASS_FILE`** — Login password or path to file containing it (file variant takes precedence).
- **`COOKIE_SECRET`** or **`COOKIE_SECRET_FILE`** — Base64-encoded cookie secret (≥32 bytes when decoded) or path to file; file variant takes precedence. If omitted, auto-generated and persisted.
- **`DB_PATH`** (optional) — SQLite database path; defaults to `<StateDir>/app.db`.
- **`VIDSTREAMER_FLAT_FILE_STATE`** (optional, set to `1` or `true`) — Use legacy flat-file state instead of SQLite.
- **`VIDSTREAMER_SEGMENT_ON_START`** (optional, set to `1` or `true`) — Segment all videos on startup.
- **`VIDSTREAMER_DEV_NOAUTH`** (optional, set to `1` or `true`) — Run without authentication (development only).

On startup, the server opens SQLite, applies schema migrations, and idempotently imports legacy `shares.json` and label sidecars without deleting them. To revert to flat-file state, set `VIDSTREAMER_FLAT_FILE_STATE=1`; the `cookie-secret` file persists separately.
