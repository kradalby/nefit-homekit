# Nefit Easy HomeKit Bridge

A HomeKit-enabled server that bridges the Nefit Easy thermostat to Apple HomeKit, with a web interface for additional control and monitoring.

## Features

- 🏠 **HomeKit Integration**: Control your Nefit Easy thermostat from any Apple device
- 🌐 **Web Interface**: Simple web UI accessible over Tailscale for monitoring and control
- ⚡ **Event-Driven**: Reactive architecture using Tailscale eventbus for real-time updates
- 🔄 **Persistent Connection**: Single XMPP connection kept alive for optimal performance
- 📊 **Prometheus Metrics**: Built-in metrics for monitoring
- 🔒 **Secure**: Runs as unprivileged user with minimal permissions on NixOS

## Architecture

The application uses an event-driven architecture with the following components:

- **EventBus**: Central pub/sub system for decoupled communication
- **Nefit Client**: Persistent XMPP connection to Nefit Easy thermostat
- **HomeKit Server**: HAP server for Apple HomeKit integration
- **Web Server**: HTTP server with SSE for real-time updates

All components communicate via typed events through the eventbus, ensuring clean separation and easy testing.

## Usage

### Running the Application

```bash
# Build
go build ./cmd/nefit-homekit

# Run with environment variables
export NEFITHK_NEFIT_SERIAL="your-serial"
export NEFITHK_NEFIT_ACCESS_KEY="your-access-key"
export NEFITHK_NEFIT_PASSWORD="your-password"
./nefit-homekit
```

The application will start:
- **HomeKit Server** on port 12345 (default) - Pair using PIN `00102003`
- **Web Interface** on port 8080 (default) - http://localhost:8080

### Configuration

All configuration via environment variables with `NEFITHK_` prefix:

```bash
# Required
export NEFITHK_NEFIT_SERIAL="your-serial"
export NEFITHK_NEFIT_ACCESS_KEY="your-key"
export NEFITHK_NEFIT_PASSWORD="your-password"

# Optional (with defaults)
export NEFITHK_HAP_PIN="00102003"
export NEFITHK_HAP_PORT="12345"
export NEFITHK_WEB_PORT="8080"
export NEFITHK_LOG_LEVEL="info"
export NEFITHK_LOG_FORMAT="json"

# Tailscale (optional)
export NEFITHK_TAILSCALE_ENABLED="false"
export NEFITHK_TAILSCALE_AUTHKEY="your-authkey"
export NEFITHK_TAILSCALE_HOSTNAME="nefit-homekit"
```

See [NEFIT_IMPLEMENTATION.md](NEFIT_IMPLEMENTATION.md) for full configuration options.

## NixOS Deployment

### Using the Flake

Add to your `flake.nix`:

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

Then in your `configuration.nix`:

```nix
{ config, pkgs, ... }:

{
  # Create environment file first:
  # /etc/nefit-homekit/env should contain:
  #   NEFITHK_NEFIT_SERIAL=your-serial
  #   NEFITHK_NEFIT_ACCESS_KEY=your-key
  #   NEFITHK_NEFIT_PASSWORD=your-password
  #   NEFITHK_HAP_PIN=12345678
  # chmod 600 /etc/nefit-homekit/env

  services.nefit-homekit = {
    enable = true;
    environmentFile = "/etc/nefit-homekit/env";

    # Optional: Override or add environment variables
    environment = {
      NEFITHK_LOG_LEVEL = "debug";
      NEFITHK_WEB_PORT = "8080";
    };
  };
}
```

### Available Options

```
services.nefit-homekit.enable                   # Enable the service
services.nefit-homekit.package                  # Package to use (default: pkgs.nefit-homekit)
services.nefit-homekit.environmentFile          # Path to file with NEFITHK_* env vars
services.nefit-homekit.environment              # Attribute set of environment variables
services.nefit-homekit.user                     # Service user (default: "nefit-homekit")
services.nefit-homekit.group                    # Service group (default: "nefit-homekit")
```

All application configuration is done via environment variables with the `NEFITHK_` prefix.
See the Configuration section above for available environment variables.

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
go test -v ./...    # Test immediately ✅
golangci-lint run   # Lint immediately ✅

# Before commit:
nix run .#test      # All tests
nix run .#lint      # All linters
nix run .#test-race # Race detector
```

## Implementation Status

### ✅ Phase 1: Foundation (COMPLETE)
- golangci-lint configuration with 25+ linters
- Nix flake for development environment
- Configuration management with go-env
- Structured logging with zap
- 100% test coverage on core packages

### ✅ Phase 2: EventBus Setup (COMPLETE)
- Tailscale eventbus integration
- Event type definitions (State, Command, ConnectionStatus)
- Named clients for each component
- Graceful shutdown support
- 95% test coverage with race detector

### ✅ Phase 3: Nefit Integration (COMPLETE)
- Persistent XMPP connection management
- Event subscription and push notifications
- Command handling from eventbus
- Automatic reconnection with exponential backoff
- Status polling for keepalive
- 100% test coverage with race detector

### ✅ Phase 4: HomeKit Integration (COMPLETE)
- HAP server setup
- Thermostat accessory implementation
- EventBus integration
- 100% test coverage with race detector

### ✅ Phase 5: Web Interface (COMPLETE)
- HTTP server with elem-go templates
- SSE for real-time state updates
- HTMX endpoints for dynamic updates
- EventBus debugger interface
- Prometheus metrics endpoint
- 100% test coverage with race detector

### ✅ Application Integration (COMPLETE)
- All components wired together in main.go
- Graceful shutdown with signal handling
- Comprehensive logging and error handling
- Application builds and runs successfully

### ✅ Phase 6: Optimization (COMPLETE)
- Event deduplication implemented (skips duplicate state updates)
- Persistent XMPP connection (no reconnection overhead)
- Efficient SSE for real-time web updates
- Note: Request coalescing and connection tuning will be done during hardware testing

### ✅ Phase 7: NixOS Module (COMPLETE)
- Full NixOS module with all configuration options
- Systemd service with security hardening
- DynamicUser for unprivileged execution
- Firewall integration
- Example configurations
- Flake-based deployment

### ⏳ Phase 8: Hardware Testing & Final Polish (PENDING)
- Test with real Nefit Easy thermostat
- Verify HomeKit pairing and control
- Test web interface functionality
- Performance tuning if needed
- Final documentation polish

## Project Structure

```
nefit-homekit/
├── cmd/nefit-homekit/     # Main application
├── config/                # ✅ Configuration management
├── events/                # ✅ EventBus wrapper and types
├── nefit/                 # ✅ Nefit Easy XMPP client
├── homekit/               # ✅ HomeKit HAP server
├── web/                   # ✅ Web interface
├── logging/               # ✅ Structured logging
├── nix/                   # ✅ NixOS module
├── flake.nix              # ✅ Development environment
└── .golangci.yml          # ✅ Linter configuration
```

## Testing

```bash
# Unit tests
go test ./...

# With coverage
go test -cover ./...

# With race detector
go test -race ./...

# Benchmarks
go test -bench=. ./...
```

Current coverage:
- `config`: 100.0%
- `events`: 95.2%
- `nefit`: All tests passing with race detector
- `homekit`: All tests passing with race detector
- `web`: All tests passing with race detector
- `logging`: 95.0%

## License

MIT

## Contributing

See [NEFIT_IMPLEMENTATION.md](NEFIT_IMPLEMENTATION.md) for the detailed implementation plan and architectural decisions.
