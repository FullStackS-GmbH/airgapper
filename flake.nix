{
  description = "FULLSTACKS Universal Airgapper Dev Env";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
  };

  outputs =
    { nixpkgs, ... }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems =
        f:
        builtins.listToAttrs (
          map (system: {
            name = system;
            value = f system;
          }) supportedSystems
        );
    in
    {
      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            buildInputs = [
              pkgs.go_1_23
              pkgs.golangci-lint
              pkgs.goreleaser
            ];
            shellHook = ''
              export AIRGAPPER_CONFIG=$(pwd)/config.yaml
              export AIRGAPPER_CREDENTIALS=$(pwd)/creds
              export GOPATH=$HOME/go
              export PATH=$GOPATH/bin:$PATH
              echo "go: $(go version)"
              echo "golangci-lint: $(golangci-lint version --short 2>&1)"
              echo "goreleaser: $(goreleaser --version | grep GitVersion | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+')"
            '';
          };
        }
      );
    };
}
