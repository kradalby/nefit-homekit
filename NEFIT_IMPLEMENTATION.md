# Nefit Easy HomeKit – Implementation Notes

This document captures the implementation decisions that sit behind the `README`. It mirrors the structure of `../tasmota-homekit/TASMOTA_IMPLEMENTATION.md` so both HomeKit services stay aligned.

## Shared workflow (nefit-homekit ⇄ tasmota-homekit)

- Always work inside `nix develop`; all tooling (Go 1.25, golangci-lint, prek, kra helpers) is pinned there.
- Use the flake apps instead of bespoke scripts: `nix run .#test`, `.#test-race`, `.#lint`, `.#coverage`, and `nix build .#nefit-homekit`.
- Run `nix flake check --all-systems` before every push; it exercises the package, overlay, module evaluation, and the VM-based module/integration tests (`.#checks.x86_64-linux.*`).
- GitHub Actions runs the exact commands above on Linux and macOS matrices, so local runs must stay green.
- Vendor hashes are pinned (`sha256-bK/N3j3vjRHuvo16I/B8B/iPcf6tZxpEzRFIh+a0SgY=`). When Go dependencies change, update the hash and immediately re-run `nix flake check`. Switch to `modSha256` once upstream nixpkgs exposes proxy-less module support.

## Architecture at a glance

Component parity with `tasmota-homekit` keeps maintenance simple:

- `config`: type-safe loader for `NEFITHK_*` values (env + env files).
- `events`: fans out typed events between components (state updates, commands, connection health, metrics).
- `nefit`: persistent XMPP session that publishes state and handles commands; exposes reconnect hooks and event deduplication.
- `homekit`: brutella/hap accessories + characteristic logic; consumes state events and emits commands, all guarded with race tests.
- `web`: kra/web server with elem-go templates, SSE feeds, HTMX handlers, Prometheus metrics, QR codes, eventbus debugger, and Tailscale listener management.
- `logging`: slog helpers used everywhere (no `fmt.Println`).
- `nix/`: package, overlay, module, and VM tests shared with other hosts.

Data flow:

```
Nefit boiler (XMPP)
   ⇅    nefit package (state + command events)
   ⇅    events bus
      ├─ homekit package (bridged accessories)
      ├─ web package (kra server, SSE, HTMX, metrics, QR)
      └─ logging + metrics exporters
```

All goroutines register with the eventbus and shut down via context cancellation so `SIGTERM` cleanly drains the runtime.

## Configuration surfaces

### Environment variables (`NEFITHK_*`)

- Required: `NEFITHK_NEFIT_SERIAL`, `NEFITHK_NEFIT_ACCESS_KEY`, `NEFITHK_NEFIT_PASSWORD`.
- Identity: `NEFITHK_BRIDGE_NAME` (defaults `nefit-homekit`) controls the HomeKit bridge label and doubles as the default Tailscale hostname unless `NEFITHK_TAILSCALE_HOSTNAME` is set.
- Networking: Prefer `NEFITHK_HAP_ADDR` and `NEFITHK_WEB_ADDR` (Go-style `addr:port`). When omitted, the service composes them from `NEFITHK_*_BIND_ADDRESS` (defaults `0.0.0.0`) and `NEFITHK_*_PORT` (defaults `12345`/`8080`).
- HomeKit: `NEFITHK_HAP_PIN`, `NEFITHK_HAP_STORAGE_PATH` (the module sets this to `services.nefit-homekit.dataDir + "/hap"`).
- Logging: `NEFITHK_LOG_LEVEL`, `NEFITHK_LOG_FORMAT` (json/console).
- Tailscale: `NEFITHK_TAILSCALE_HOSTNAME`, `NEFITHK_TAILSCALE_AUTHKEY`, `NEFITHK_TAILSCALE_STATE_DIR`. The module pulls the auth key from `tailscale.authKeyFile`, injects it via credentials, and points the state dir at `services.nefit-homekit.dataDir + "/tailscale"`. When `NEFITHK_TAILSCALE_HOSTNAME` is omitted, it inherits the bridge name.

Store these variables in `/etc/nefit-homekit/env` (or an agenix secret) and point `services.nefit-homekit.environmentFile` at it. Avoid ad-hoc `Environment` overrides unless you are intentionally pinning non-secret values.

### NixOS module

`nixosModules.default` exposes the following options (all documented in the README):

- `services.nefit-homekit.enable`, `.package`, `.environmentFile`, `.environment`.
- `services.nefit-homekit.bridgeName`, `.ports.{hap,web}`, `.hapPin`, `.dataDir`.
- `services.nefit-homekit.tailscale.{hostname,authKeyFile}` (hostname defaults to `bridgeName`).
- `services.nefit-homekit.log.{level,format}`.
- `services.nefit-homekit.openFirewall`, `.user`, `.group`.

The module provisions users, state/cache/runtime directories, strict systemd hardening, optional firewall rules, and loads the env file + tailscale credential.
`services.nefit-homekit.dataDir` is the single source of truth for persistent storage; the module binds HomeKit storage to `dataDir/hap` and exports `NEFITHK_TAILSCALE_STATE_DIR=dataDir/tailscale` so tsnet/tailscale artifacts never escape that tree.

## Web + Tailscale model

- kra/web runs once and binds to `NEFITHK_WEB_BIND_ADDRESS:NEFITHK_WEB_PORT`.
- When a Tailscale auth key is provided, the server auto-enrols on the tailnet and serves HTTPS via the built-in cert tooling; the plaintext listener stays available on the LAN port.
- SSE endpoints (`/events`, `/debug/eventbus`) stream JSON payloads; keep payloads stable because the Tasmota dashboard mirrors them for debugging.

## Testing + CI expectations

- `go test ./...` must stay under race detector; use table-driven tests for the eventbus, nefit client, and kra handlers.
- `nix run .#test` emits coverage; commit-time expectation is >95% on the packages listed in the README.
- `nix run .#test-race` is mandatory because GitHub Actions runs it on macOS and Linux.
- `nix run .#lint` wraps golangci-lint with the repo-specific configuration (no extra linters without updating `.golangci.yml`).
- `nix flake check --all-systems` builds the package for each default system, evaluates the overlay, and runs both VM tests. The module test validates option wiring/env files, while the integration test boots a full system with HAP + web port probes.

## Phase status

- Phases 1–7 are complete (architecture, eventbus, nefit client, HomeKit glue, web UI, optimisations, and the NixOS module).
- Phase 8 is in progress: hardware testing on the real thermostat, validating HomeKit pairing stability, tailnet exposure, and final polishing of docs/metrics.

## Immediate focus / hand-off notes

- Keep docs trimmed. The README is the authoritative workflow; this file just records context.
- `vendorHash` is stable for now—rerun `nix flake check` after touching `go.mod`/`go.sum`.
- When upstream nixpkgs lands proxy-less `buildGoModule`, switch to `modSha256` to avoid vendored fetches; track that upstream issue in the flake inputs comment.
- Home deployments (e.g., `dotfiles/machines/home.ldn`) now import both HomeKit modules and rely on the env-file/tailscale credentials provided by agenix. Update those host configs whenever options change.
