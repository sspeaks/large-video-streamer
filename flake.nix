{
  inputs = {
    nixpkgs.url = "nixpkgs/nixos-25.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        vid-streamer = pkgs.buildGoModule {
          pname = "vid-streamer";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-XYCq8AdFSuu2k4DR8cgtxmK6TFTRq1WuRpo4AZDTF6U=";
          # The Go module path's last element is "large-video-streamer", so the
          # compiled binary is named that. Rename it to the friendlier
          # "vid-streamer" expected by meta.mainProgram, the dev app, and the
          # NixOS module (lib.getExe cfg.package).
          postInstall = ''
            mv "$out/bin/large-video-streamer" "$out/bin/vid-streamer"
          '';
          meta.mainProgram = "vid-streamer";
        };
        devScript = pkgs.writeShellApplication {
          name = "vid-streamer-dev";
          runtimeInputs = [ vid-streamer pkgs.ffmpeg pkgs.mkvtoolnix ];
          text = ''
            set -euo pipefail
            VIDEO_DIR="''${1:-$PWD/videos}"
            if [ -n "''${VIDSTREAMER_DEV_STATE_DIR:-}" ]; then
              # Persistent state dir (opt-in): segments are cached across runs.
              STATE="$VIDSTREAMER_DEV_STATE_DIR"
            else
              # Ephemeral dev output removed on exit; generated HLS can be as
              # large as the source videos, so we don't leave it lying around.
              STATE="$(mktemp -d -t vid-streamer-dev.XXXXXX)"
              trap 'rm -rf "$STATE"' EXIT INT TERM
            fi
            mkdir -p "$STATE/hls"
            export VIDSTREAMER_DEV_NOAUTH=1
            export VIDSTREAMER_SEGMENT_ON_START=1
            export VIDEO_DIR HLS_DIR="$STATE/hls" LISTEN_ADDR="127.0.0.1:8080"
            echo "vid-streamer DEV (NO AUTH) on http://127.0.0.1:8080  (video_dir=$VIDEO_DIR)"
            echo "temporary HLS output: $STATE/hls (removed on exit; set VIDSTREAMER_DEV_STATE_DIR to keep)"
            # Not exec: keep this shell alive so the cleanup trap runs on exit.
            vid-streamer
          '';
        };
        apps = {
          dev = {
            type = "app";
            program = "${devScript}/bin/vid-streamer-dev";
            meta.description = "Run a temporary passwordless local dev server (no auth) serving VIDEO_DIR.";
          };
          default = apps.dev;
        };
      in
      {
        packages.default = vid-streamer;
        packages.vid-streamer = vid-streamer;

        inherit apps;

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
            pkgs.ffmpeg
            pkgs.mkvtoolnix
          ];
        };

        formatter = pkgs.nixpkgs-fmt;
      }) // {
      nixosModules.vidStreamer = import ./nix/module.nix;
      nixosModules.default = import ./nix/module.nix;
    };
}
