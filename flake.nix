{
  description = "Nefit Easy HomeKit Bridge";

  inputs = {
    nixpkgs.url = "nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    flake-checks.url = "github:kradalby/flake-checks";
    flake-checks.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      flake-checks,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            (_: prev: {
              # goimports/gofumpt ship wrapped with a `go` on PATH. That `go`
              # must be at least the go.mod directive, or GOTOOLCHAIN=auto
              # tries to fetch a toolchain from inside the network-less
              # treefmt sandbox. golangci-lint already tracks go_latest
              # upstream, so it needs no override.
              gotools = prev.gotools.override {
                buildGoModule = prev.buildGoLatestModule;
                go = prev.go_latest;
              };
              # gofumpt takes no `go` argument; buildGoModule carries it.
              gofumpt = prev.gofumpt.override {
                buildGoModule = prev.buildGoLatestModule;
              };
            })
          ];
        };
        fc = flake-checks.lib;

        # Go version - track the latest release (go.mod says 1.27.0).
        go = pkgs.go_latest;

        common = {
          inherit pkgs;
          root = ./.;
          pname = "nefit-homekit";
          version = "0.1.0";
          vendorHash = "sha256-7BNHHdNmUQdZw9m6HurRAjYgh/HEECfovG/L82hnLu0=";
          goPkg = go;
          # web/server.go embeds web/static/app.js.
          embedDirs = [ (./. + "/web/static") ];
        };

      in
      {
        # Development shell
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            golangci-lint
            gotools # gopls, goimports, etc.
            delve # debugger
            entr # file watcher for auto-testing
            git
            prek # pre-commit hooks
            nixfmt # Nix formatter (RFC 166)
            prettier
          ];
        };

        # Custom apps for common tasks
        apps = {
          test = {
            type = "app";
            program = toString (
              pkgs.writeShellScript "test" ''
                set -e
                ${go}/bin/go test -v -cover -coverprofile=coverage.out ./...
                ${go}/bin/go tool cover -func=coverage.out | tail -n 1
              ''
            );
          };

          lint = {
            type = "app";
            program = toString (
              pkgs.writeShellScript "lint" ''
                set -e
                echo "🔍 Running golangci-lint..."
                ${pkgs.golangci-lint}/bin/golangci-lint run ./...
              ''
            );
          };

          test-race = {
            type = "app";
            program = toString (
              pkgs.writeShellScript "test-race" ''
                set -e
                echo "🏃 Running tests with race detector..."
                ${go}/bin/go test -race ./...
                echo "✅ No race conditions detected!"
              ''
            );
          };

          coverage = {
            type = "app";
            program = toString (
              pkgs.writeShellScript "coverage" ''
                set -e
                echo "📊 Generating coverage report..."
                ${go}/bin/go test -coverprofile=coverage.out ./...
                ${go}/bin/go tool cover -html=coverage.out -o coverage.html
                echo "✅ Coverage report generated: coverage.html"

                # Try to open in browser if available
                if command -v xdg-open > /dev/null; then
                  xdg-open coverage.html
                elif command -v open > /dev/null; then
                  open coverage.html
                fi
              ''
            );
          };
        };

        # Package output
        packages.default = fc.goBuild common;

        packages.nefit-homekit = self.packages.${system}.default;

        formatter = fc.formatter common;

        # Gate checks via the shared flake-checks library, merged with the
        # NixOS VM tests (gated to manual dispatch in CI).
        checks = {
          build = fc.goBuild common;
          gotest = fc.goTest common;
          golangci-lint = fc.goLint common;
          formatting = fc.goFormat common;
        }
        // {
          module-test = import ./nix/test.nix {
            inherit pkgs system;
            inherit self;
          };

          integration-test = import ./nix/integration-test.nix {
            inherit pkgs system;
            inherit self;
          };
        };
      }
    )
    // {
      # NixOS module
      nixosModules.default = import ./nix/module.nix;

      # Overlay for adding the package to nixpkgs
      overlays.default = final: prev: {
        nefit-homekit = self.packages.${final.system}.default;
      };
    };
}
