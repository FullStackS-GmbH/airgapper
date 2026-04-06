{
  description = "FULLSTACKS Universal Airgapper Dev Env";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShell = pkgs.mkShell {
          buildInputs = [
            pkgs.go_1_23
            pkgs.golangci-lint
            pkgs.goreleaser
            pkgs.gopls
            pkgs.gotools
            pkgs.delve
          ];
          shellHook = ''
            export AIRGAPPER_CONFIG=$(pwd)/config.yaml
            export AIRGAPPER_CREDENTIALS=$(pwd)/creds
            export GOPATH=$HOME/go
            export PATH=$GOPATH/bin:$PATH
          '';
        };
      }
    );
}
