{
  description = "Personal file sharing server";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "fileshare";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-vj6rssVFgZVg8YmDZlnZ4NbSGyu6jVkDSViYJwN3QcU=";
          subPackages = [ "cmd/fileshare" ];
          ldflags = [
            "-s"
            "-w"
          ];
          meta = with pkgs.lib; {
            description = "Personal file sharing server with a web UI and WebDAV";
            license = licenses.mit;
            mainProgram = "fileshare";
          };
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gotools
            pkgs.golangci-lint
            pkgs.nodejs_22
            pkgs.git
          ];
        };
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.system}.default}/bin/fileshare";
        };
      });
    };
}
