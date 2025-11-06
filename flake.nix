{
  description = "Nefit Easy HomeKit Bridge";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Go version - use latest available
        go = pkgs.go;

      in
      {
        # Development shell
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            golangci-lint
            gotools      # gopls, goimports, etc.
            delve        # debugger
            entr         # file watcher for auto-testing
            git
          ];

          shellHook = ''
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "🏠 Nefit Easy HomeKit Bridge - Development Environment"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "📦 Tools available:"
            echo "  • go version: $(go version | cut -d' ' -f3)"
            echo "  • golangci-lint: $(golangci-lint --version | head -n1)"
            echo ""
            echo "🚀 Quick commands:"
            echo "  nix run .#test       - Run tests with coverage"
            echo "  nix run .#lint       - Run golangci-lint"
            echo "  nix run .#test-race  - Run tests with race detector"
            echo "  nix run .#coverage   - Generate HTML coverage report"
            echo "  nix build            - Build the binary"
            echo ""
            echo "💡 Continuous development:"
            echo "  ls **/*.go | entr -c go test -v ./..."
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
          '';
        };

        # Custom apps for common tasks
        apps = {
          test = {
            type = "app";
            program = toString (pkgs.writeShellScript "test" ''
              set -e
              echo "🧪 Running tests with coverage..."
              ${go}/bin/go test -v -cover -coverprofile=coverage.out ./...
              echo ""
              echo "📊 Coverage summary:"
              ${go}/bin/go tool cover -func=coverage.out | tail -n 1
            '');
          };

          lint = {
            type = "app";
            program = toString (pkgs.writeShellScript "lint" ''
              set -e
              echo "🔍 Running golangci-lint..."
              ${pkgs.golangci-lint}/bin/golangci-lint run ./...
              echo "✅ Lint passed!"
            '');
          };

          test-race = {
            type = "app";
            program = toString (pkgs.writeShellScript "test-race" ''
              set -e
              echo "🏃 Running tests with race detector..."
              ${go}/bin/go test -race ./...
              echo "✅ No race conditions detected!"
            '');
          };

          coverage = {
            type = "app";
            program = toString (pkgs.writeShellScript "coverage" ''
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
            '');
          };
        };

        # Package output
        packages.default = pkgs.buildGoModule {
          pname = "nefit-homekit";
          version = "0.1.0";
          src = ./.;

          vendorHash = "sha256-KULIwmIp/IyzoK9vwCpKAr6hr9qx+mpMSXJddfqncFs=";

          # Allow Go to auto-download the required toolchain version
          proxyVendor = true;
          allowGoReference = true;

          preBuild = ''
            export GOTOOLCHAIN=auto
          '';

          ldflags = [
            "-s"
            "-w"
            "-X main.version=0.1.0"
          ];

          meta = with pkgs.lib; {
            description = "HomeKit bridge for Nefit Easy thermostat";
            homepage = "https://github.com/kradalby/nefit-homekit";
            license = licenses.mit;
            maintainers = [ ];
          };
        };

        packages.nefit-homekit = self.packages.${system}.default;

        # NixOS tests
        checks = {
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
    ) // {
      # NixOS module
      nixosModules.default = import ./nix/module.nix;

      # Overlay for adding the package to nixpkgs
      overlays.default = final: prev: {
        nefit-homekit = self.packages.${final.system}.default;
      };
    };
}
