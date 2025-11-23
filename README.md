# Nefit Easy HomeKit Bridge

A HomeKit-enabled server that bridges the Nefit Easy thermostat to Apple HomeKit, with a web interface for additional control and monitoring.

## Features

- **HomeKit integration**: Control a Nefit Easy thermostat from any Apple device
- **Web interface**: Tailscale-aware UI for monitoring, control, and HomeKit pairing
- **Event-driven runtime**: Typed events and eventbus keep HomeKit, the web app, and the Nefit client in sync
- **Long-lived connection**: Maintains a single authenticated XMPP connection to the boiler
- **Metrics and health checks**: Prometheus metrics, SSE diagnostics, and JSON health summaries
- **Lifecycle table**: `/debug/eventbus` shows Web/HomeKit/Nefit component state so outages are obvious without grepping logs.
- **Hardened NixOS service**: Runs as an unprivileged user with strict systemd sandboxes

## Architecture

The application uses an event-driven architecture with the following components:

- **EventBus**: Central pub/sub system for decoupled communication
- **Nefit Client**: Persistent XMPP connection to Nefit Easy thermostat
- **HomeKit Server**: HAP server for Apple HomeKit integration
- **Web Server**: HTTP server with SSE for real-time updates

All components communicate via typed events through the eventbus, ensuring clean separation and easy testing.

## Usage

### Development shell and helper apps

Always enter the flake shell before working:

```bash
nix develop
```

Helper apps wrap the standard Go tooling and keep outputs consistent with CI:

```bash
nix run .#test       # go test -v -cover ./...
nix run .#test-race  # go test -race ./...
nix run .#lint       # golangci-lint run ./...
nix run .#coverage   # Generate coverage.html
nix flake check --all-systems  # NixOS module + integration tests
nix build .#nefit-homekit      # Build the release binary
```

### Local execution

Provide the required environment variables (directly or via an environment file) and run the packaged binary:

```bash
export NEFITHK_NEFIT_SERIAL="your-serial"
export NEFITHK_NEFIT_ACCESS_KEY="your-access-key"
export NEFITHK_NEFIT_PASSWORD="your-password"
nix run .#nefit-homekit
```

The bridge exposes the HomeKit accessory server (default `:12345`) and the web interface (default `:8080`). Pair using PIN `00102003` unless overridden.

### Configuration

All runtime configuration uses `NEFITHK_` environment variables. Create an environment file for deployment and point the service at it via `services.nefit-homekit.environmentFile`:

```bash
cat >/etc/nefit-homekit/env <<'EOF'
NEFITHK_NEFIT_SERIAL=your-serial
NEFITHK_NEFIT_ACCESS_KEY=your-access-key
NEFITHK_NEFIT_PASSWORD=your-password
NEFITHK_HAP_PIN=00102003
NEFITHK_HAP_ADDR=0.0.0.0:12345
NEFITHK_WEB_ADDR=0.0.0.0:8080
NEFITHK_LOG_LEVEL=info
NEFITHK_LOG_FORMAT=json
# Optional: enable tailscale/kra listener
# NEFITHK_TAILSCALE_AUTHKEY=tskey-abc
# NEFITHK_TAILSCALE_HOSTNAME=nefit-homekit
# NEFITHK_TAILSCALE_STATE_DIR=/var/lib/nefit-homekit/tailscale
EOF
chmod 600 /etc/nefit-homekit/env
```

`NEFITHK_HAP_ADDR` and `NEFITHK_WEB_ADDR` accept Go-style `addr:port` bindings (IPv4, IPv6 in `[::]:1234`, etc.). If omitted, the application composes them from `NEFITHK_*_BIND_ADDRESS` (default `0.0.0.0`) and `NEFITHK_*_PORT` (defaults `12345`/`8080`).

Set `NEFITHK_HAP_STORAGE_PATH` when the default `dataDir/hap` is not suitable (for example on hosts with dedicated persistent storage). The NixOS module maps `services.nefit-homekit.dataDir` to both this HomeKit directory (`dataDir/hap`) and the Tailscale state directory (`dataDir/tailscale`, wired via `NEFITHK_TAILSCALE_STATE_DIR`).

See [NEFIT_IMPLEMENTATION.md](NEFIT_IMPLEMENTATION.md) for a detailed description of each option and the default values.

### Web Interface & Endpoints

The kra-powered web server listens on `NEFITHK_WEB_BIND_ADDRESS:NEFITHK_WEB_PORT` and exposes the same endpoints locally and over Tailscale:

- `/` – Thermostat dashboard rendered with elem-go.
- `/events` – SSE stream emitting `StateUpdateEvent` JSON.
- `/api/temperature` & `/api/mode` – HTMX form handlers for user input.
- `/health` – JSON health summary (used by monitoring).
- `/metrics` – Prometheus metrics endpoint.
- `/qrcode` – Plain-text QR + PIN for headless pairing.
- `/debug/eventbus` – Diagnostic page for eventbus traffic.

Provide `NEFITHK_TAILSCALE_AUTHKEY`, `NEFITHK_TAILSCALE_HOSTNAME`, and (optionally) `NEFITHK_TAILSCALE_STATE_DIR` to expose the same endpoints over Tailscale; kra now reads the auth key directly (no temp files required). When unset, tailscale state falls back to the default OS config directory; on NixOS the module automatically points it at `services.nefit-homekit.dataDir + /tailscale`.

## NixOS Deployment

### Using the Flake

Add the module to your `flake.nix`:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    nefit-homekit.url = "github:kradalby/nefit-homekit";
  };

  outputs = { self, nixpkgs, nefit-homekit }: {
    nixosConfigurations.your-host = nixpkgs.lib.nixosSystem {
      modules = [
        nefit-homekit.nixosModules.default
        ./configuration.nix
      ];
    };
  };
}
```

Example host configuration:

```nix
{ config, pkgs, ... }:

{
  # Provide credentials via an env file or agenix secret.
  environment.etc."nefit-homekit/env".text = ''
    NEFITHK_NEFIT_SERIAL=your-serial
    NEFITHK_NEFIT_ACCESS_KEY=your-access-key
    NEFITHK_NEFIT_PASSWORD=your-password
    NEFITHK_HAP_PIN=12345678
  '';

  services.nefit-homekit = {
    enable = true;
    package = pkgs.nefit-homekit;
    environmentFile = "/etc/nefit-homekit/env";
    dataDir = "/var/lib/nefit-homekit";

    ports = {
      hap = 51826;
      web = 51827;
    };

    tailscale = {
      hostname = "nefit-homekit";
      authKeyFile = "/run/secrets/tailscale-authkey";
    };

    log.level = "debug";

    # Extra NEFITHK_* overrides if needed
    environment = {
      NEFITHK_WEB_BIND_ADDRESS = "0.0.0.0";
    };

    openFirewall = true;
  };
}
```

### Available Options

```
services.nefit-homekit.enable             # Enable the service
services.nefit-homekit.package            # Package derivation (defaults to pkgs.nefit-homekit)
services.nefit-homekit.environmentFile    # Path to file with NEFITHK_* values
services.nefit-homekit.environment        # Attrset of extra NEFITHK_* overrides
services.nefit-homekit.ports.hap          # HAP port (default 12345)
services.nefit-homekit.ports.web          # Web port (default 8080)
services.nefit-homekit.bindAddresses.hap  # IP address for HAP listener (default 0.0.0.0)
services.nefit-homekit.bindAddresses.web  # IP address for web listener (default 0.0.0.0)
services.nefit-homekit.hapPin             # HomeKit PIN (8 digits)
services.nefit-homekit.dataDir            # Root directory for HomeKit + Tailscale state (hap/tailscale subdirs)
services.nefit-homekit.tailscale.hostname # Tailnet hostname when enabled
services.nefit-homekit.tailscale.authKeyFile # Credential used for Tailscale auth
services.nefit-homekit.log.level          # slog level (debug/info/warn/error)
services.nefit-homekit.log.format         # slog format (json/console)
services.nefit-homekit.openFirewall       # Open HAP/web/mDNS ports automatically
services.nefit-homekit.user               # Service user (default nefit-homekit)
services.nefit-homekit.group              # Service group (default nefit-homekit)
```

All application configuration is driven by `NEFITHK_` variables. Use the module options above to control how the service receives those values.

### Security Features

The NixOS module includes comprehensive security hardening:

- Runs as unprivileged user `nefit-homekit`
- Filesystem isolation with `ProtectSystem=strict`
- System call filtering
- No new privileges
- Private `/tmp`
- Restricted address families (only `AF_UNIX`, `AF_INET`, `AF_INET6`)
- Memory write-execute protection
- And many more systemd hardening options

## Development

### Requirements

- Nix with flakes enabled
- direnv (recommended)

### Quick Start

```bash
# Enter development shell (or use direnv)
nix develop

# Run tests
go test ./...

# Run tests with coverage
nix run .#test

# Run linter
nix run .#lint

# Run with race detector
nix run .#test-race

# Build
nix build
```

### Development Workflow

We follow a **continuous testing and linting** approach:

```bash
# For each component:
vim foo.go      # Write code
vim foo_test.go # Write test
go test -v ./...    # Test immediately
golangci-lint run   # Lint immediately

# Before commit:
nix run .#test           # Coverage build
nix run .#lint           # Linters
nix run .#test-race      # Race detector
nix flake check --all-systems
```

## Continuous Integration

`.github/workflows/ci.yml` mirrors the local workflow. Every push and pull request runs:

- `go test -v ./...` with coverage on Linux and macOS
- `go test -race ./...`
- `golangci-lint run ./...`
- `nix build .#nefit-homekit`
- `nix flake check --all-systems` followed by the VM-based module and integration tests

Only push when the flake apps succeed locally so GitHub Actions stays green.

## Implementation Status

### Phase 1: Foundation (COMPLETE)

- golangci-lint configuration with 25+ linters
- Nix flake for development environment
- Configuration management with go-env
- Structured logging with slog
- 100% test coverage on core packages

### Phase 2: EventBus Setup (COMPLETE)

- Tailscale eventbus integration
- Event type definitions (State, Command, ConnectionStatus)
- Named clients for each component
- Graceful shutdown support
- 95% test coverage with race detector

### Phase 3: Nefit Integration (COMPLETE)

- Persistent XMPP connection management
- Event subscription and push notifications
- Command handling from eventbus
- Automatic reconnection with exponential backoff
- Status polling for keepalive
- 100% test coverage with race detector

### Phase 4: HomeKit Integration (COMPLETE)

- HAP server setup
- Thermostat accessory implementation
- EventBus integration
- 100% test coverage with race detector

### Phase 5: Web Interface (COMPLETE)

- HTTP server with elem-go templates
- SSE for real-time state updates
- HTMX endpoints for dynamic updates
- EventBus debugger interface
- Prometheus metrics endpoint
- 100% test coverage with race detector

### Application Integration (COMPLETE)

- All components wired together in main.go
- Graceful shutdown with signal handling
- Comprehensive logging and error handling
- Application builds and runs successfully

### Phase 6: Optimization (COMPLETE)

- Event deduplication implemented (skips duplicate state updates)
- Persistent XMPP connection (no reconnection overhead)
- Efficient SSE for real-time web updates
- Note: Request coalescing and connection tuning will be done during hardware testing

### Phase 7: NixOS Module (COMPLETE)

- Full NixOS module with all configuration options
- Systemd service with security hardening
- DynamicUser for unprivileged execution
- Firewall integration
- Example configurations
- Flake-based deployment

### Phase 8: Hardware Testing & Final Polish (PENDING)

- Test with real Nefit Easy thermostat
- Verify HomeKit pairing and control
- Test web interface functionality
- Performance tuning if needed
- Final documentation polish

## Project Structure

```
nefit-homekit/
├── cmd/nefit-homekit/     # Main application
├── config/                # Configuration management
├── events/                # EventBus wrapper and types
├── nefit/                 # Nefit Easy XMPP client
├── homekit/               # HomeKit HAP server
├── web/                   # Web interface
├── logging/               # Structured logging
├── nix/                   # NixOS module and tests
├── flake.nix              # Development environment
└── .golangci.yml          # Linter configuration
```

## Testing

### Unit Tests

```bash
# Unit tests
go test ./...

# With coverage
go test -cover ./...

# With race detector
go test -race ./...

# Using Nix
nix run .#test        # Run tests with coverage
nix run .#test-race   # Run with race detector
```

Current coverage:

- `config`: 100.0%
- `events`: 95.2%
- `nefit`: All tests passing with race detector
- `homekit`: All tests passing with race detector
- `web`: All tests passing with race detector
- `logging`: 95.0%

### NixOS Integration Tests

Automated tests validate the NixOS module:

```bash
# Run all checks
nix flake check

# Run specific test
nix build .#checks.x86_64-linux.module-test
nix build .#checks.x86_64-linux.integration-test

# Interactive testing
nix build .#checks.x86_64-linux.module-test.driverInteractive
./result/bin/nixos-test-driver
```

Tests cover:

- Service startup and lifecycle
- Port accessibility (HAP 12345, Web 8080)
- Environment variable configuration
- Environment file loading
- Security hardening validation
- Multi-node configurations

See [nix/README.md](nix/README.md) for detailed test documentation.

## License

MIT

## Contributing

See [NEFIT_IMPLEMENTATION.md](NEFIT_IMPLEMENTATION.md) for the detailed implementation plan and architectural decisions.
