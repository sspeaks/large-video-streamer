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
          vendorHash = null;
          meta.mainProgram = "vid-streamer";
        };
        devScript = pkgs.writeShellApplication {
          name = "vid-streamer-dev";
          runtimeInputs = [ vid-streamer pkgs.ffmpeg pkgs.mkvtoolnix ];
          text = ''
            set -euo pipefail
            VIDEO_DIR="''${1:-$PWD/videos}"
            STATE="''${VIDSTREAMER_DEV_STATE_DIR:-$PWD/.gotmp/vid-streamer-dev-state-$$}"
            mkdir -p "$STATE/hls"
            export VIDSTREAMER_DEV_NOAUTH=1
            export VIDSTREAMER_SEGMENT_ON_START=1
            export VIDEO_DIR HLS_DIR="$STATE/hls" LISTEN_ADDR="127.0.0.1:8080"
            echo "vid-streamer DEV (NO AUTH) on http://127.0.0.1:8080  (video_dir=$VIDEO_DIR)"
            exec vid-streamer
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
