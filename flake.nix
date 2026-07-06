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
      in
      {
        packages.default = vid-streamer;
        packages.vid-streamer = vid-streamer;

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
