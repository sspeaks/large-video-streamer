{ config, lib, pkgs, ... }:

let
  cfg = config.services.vidStreamer;
  types = lib.types;
  defaultUser = "vid-streamer";
  defaultGroup = "vid-streamer";
  listenPort = lib.toInt (lib.last (lib.splitString ":" cfg.listenAddr));
in
{
  options.services.vidStreamer = {
    enable = lib.mkEnableOption "vid-streamer authenticated HLS video server";

    package = lib.mkOption {
      type = types.nullOr types.package;
      default = null;
      description = "Package providing the vid-streamer binary.";
    };

    videoDir = lib.mkOption {
      type = types.path;
      description = "Source folder of .mkv files.";
    };

    hlsDir = lib.mkOption {
      type = types.path;
      default = "/var/lib/vid-streamer/hls";
      description = "Writable directory for generated HLS playlists and segments.";
    };

    listenAddr = lib.mkOption {
      type = types.str;
      default = "127.0.0.1:8080";
      description = "Address and port for the vid-streamer HTTP server.";
    };

    openFirewall = lib.mkOption {
      type = types.bool;
      default = false;
      description = "Open the TCP port from listenAddr in the local firewall.";
    };

    user = lib.mkOption {
      type = types.str;
      default = defaultUser;
      description = "User account that runs the vid-streamer service.";
    };

    group = lib.mkOption {
      type = types.str;
      default = defaultGroup;
      description = "Group account that runs the vid-streamer service.";
    };

    noAuth = lib.mkOption {
      type = types.bool;
      default = false;
      description = "Run WITHOUT authentication (trusted networks only).";
    };

    segmentOnStart = lib.mkOption {
      type = types.bool;
      default = true;
      description = "Segment all videos in videoDir into HLS when the service starts.";
    };

    loginUserFile = lib.mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to a file (e.g. a sops-nix secret path) containing the value.";
    };

    loginPassFile = lib.mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to a file (e.g. a sops-nix secret path) containing the value.";
    };

    cookieSecretFile = lib.mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Optional. Path to a file (e.g. a sops-nix secret path) containing a
        base64-encoded, >=32-byte cookie-signing secret. This is internal server
        state, NOT a credential you or your users type. If left null, the server
        auto-generates one on first start and persists it in its state directory
        (StateDirectory), so you only need to manage loginUserFile/loginPassFile.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.package != null;
        message = "services.vidStreamer.package must be set.";
      }
      {
        assertion = cfg.noAuth || (cfg.loginUserFile != null && cfg.loginPassFile != null);
        message = "Set loginUserFile and loginPassFile (or enable services.vidStreamer.noAuth). cookieSecretFile is optional and auto-generated when unset.";
      }
    ];

    users.users = lib.optionalAttrs (cfg.user == defaultUser) {
      ${cfg.user} = {
        isSystemUser = true;
        group = cfg.group;
      };
    };

    users.groups = lib.optionalAttrs (cfg.group == defaultGroup) {
      ${cfg.group} = { };
    };

    systemd.services.vid-streamer = {
      description = "vid-streamer authenticated HLS video server";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      path = [ pkgs.ffmpeg pkgs.mkvtoolnix ];
      environment = {
        VIDEO_DIR = toString cfg.videoDir;
        HLS_DIR = toString cfg.hlsDir;
        LISTEN_ADDR = cfg.listenAddr;
      }
      // lib.optionalAttrs cfg.segmentOnStart {
        VIDSTREAMER_SEGMENT_ON_START = "1";
      }
      // lib.optionalAttrs cfg.noAuth {
        VIDSTREAMER_DEV_NOAUTH = "1";
      }
      // lib.optionalAttrs (!cfg.noAuth) {
        LOGIN_USER_FILE = toString cfg.loginUserFile;
        LOGIN_PASS_FILE = toString cfg.loginPassFile;
      }
      // lib.optionalAttrs (!cfg.noAuth && cfg.cookieSecretFile != null) {
        COOKIE_SECRET_FILE = toString cfg.cookieSecretFile;
      };

      serviceConfig = {
        ExecStart = lib.getExe cfg.package;
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = "vid-streamer";
        Restart = "on-failure";
        RestartSec = "5s";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" ];
        ReadWritePaths = [ cfg.hlsDir ];
        # Ensure the (read-only) source video folder is readable even with
        # ProtectHome/ProtectSystem, wherever the operator places it.
        BindReadOnlyPaths = [ cfg.videoDir ];
      } // lib.optionalAttrs (!cfg.noAuth) {
        ConditionPathExists = cfg.loginPassFile;
      };
    };

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ listenPort ];
  };
}
