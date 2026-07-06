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

## Runtime configuration

`internal/config.Load` reads:

- `LISTEN_ADDR`: optional, defaults to `127.0.0.1:8080`.
- `VIDEO_DIR`: required, read-only source folder of videos.
- `HLS_DIR`: optional writable HLS output folder, defaults to `$STATE_DIRECTORY/hls` or `state/hls`.
- `LOGIN_USER` or `LOGIN_USER_FILE`: required login username; file variant wins.
- `LOGIN_PASS` or `LOGIN_PASS_FILE`: required login password; file variant wins.
- `COOKIE_SECRET` or `COOKIE_SECRET_FILE`: required base64 string that decodes to at least 32 bytes; file variant wins.

Secret file values are read with trailing newlines removed.

## Package layout and interface contract

- `internal/config`: owns environment loading.
  - `type Config struct { ListenAddr string; VideoDir string; HLSDir string; LoginUser string; LoginPass string; CookieSecret []byte }`
  - `func Load() (Config, error)`
- `internal/auth`: owns login/logout routes and auth gates.
  - `type Authenticator struct`
  - `func New(cfg config.Config) *Authenticator`
  - `func (a *Authenticator) RegisterRoutes(mux *http.ServeMux)`
  - `func (a *Authenticator) RequirePage(next http.Handler) http.Handler`
  - `func (a *Authenticator) RequireMedia(next http.Handler) http.Handler`
- `internal/hls`: owns generated HLS serving.
  - `type Server struct`
  - `func New(cfg config.Config) *Server`
  - `func (s *Server) Handler() http.Handler`
- `internal/labels`: owns label sidecars, UI/API routes, and timestamp import/export.
  - `type Boundary struct { Name string; Start float64 }`
  - `type Candidate struct { Time float64; Duration float64; Status string }`
  - `type VideoLabels struct { Video string; Boundaries []Boundary; Candidates []Candidate }`
  - `type Store struct`
  - `func New(cfg config.Config) *Store`
  - `func (s *Store) RegisterRoutes(mux *http.ServeMux, a *auth.Authenticator)`
  - `func (s *Store) Load(video string) (VideoLabels, error)`
  - `func (s *Store) Save(labels VideoLabels) error`
  - `func (s *Store) ToWebVTT(labels VideoLabels) string`
  - `func (s *Store) ImportTimestamps(r io.Reader) (VideoLabels, error)`
  - `func (s *Store) ExportTimestamps(labels VideoLabels) string`
- `internal/segment`: owns ffprobe/ffmpeg HLS generation.
  - `func Segment(cfg config.Config, videoName string) error`
- `internal/detect`: owns ffmpeg silence detection.
  - `func DetectSilence(path string, noiseDB float64, minDur float64) ([]labels.Candidate, error)`
- `internal/web`: owns embedded assets.
  - `var Assets embed.FS`
  - `func Handler() http.Handler`
  - `func Index() http.Handler`
  - `func Player() http.Handler`

## NixOS module

The flake exports `nixosModules.vidStreamer` and `nixosModules.default`. Add it to a NixOS configuration flake and wire the package from the same input:

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
            listenAddr = "127.0.0.1:8080";
            loginUserFile = "/run/secrets/vid-streamer-user";
            loginPassFile = "/run/secrets/vid-streamer-pass";
            cookieSecretFile = "/run/secrets/vid-streamer-cookie-secret";
          };
        }
      ];
    };
  };
}
```

Set `services.vidStreamer.noAuth = true` only for trusted/local deployments; it disables the credential file requirements. For local development, `nix run .#dev` starts the dev server.
