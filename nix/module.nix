{ config, lib, pkgs, ... }:

let
  cfg = config.services.vidStreamer;
in
{
  options.services.vidStreamer = {
    enable = lib.mkEnableOption "vid-streamer HLS video streaming service";

    package = lib.mkOption {
      type = lib.types.nullOr lib.types.package;
      default = null;
      description = "Package providing the vid-streamer binary.";
    };

    videoDir = lib.mkOption {
      type = lib.types.path;
      description = "Read-only source directory containing video files.";
    };

    hlsDir = lib.mkOption {
      type = lib.types.path;
      description = "Writable directory for generated HLS playlists and segments.";
    };

    listenAddr = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      description = "Address and port for the vid-streamer HTTP server.";
    };
  };

  config = lib.mkIf cfg.enable {
    # TODO nixos-module: define user, state directories, sops-nix credentials, and systemd service.
  };
}
