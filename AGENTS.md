# AGENTS NOTES – `nefit-homekit`

## Working Rules

- Always enter the Nix shell first: `nix develop`.
- Logging must use stdlib `slog`; no zap/fmt logging in new code.
- Prefer root-level packages; no `internal/` or `pkg/` directories.
- HTML must be written with [elem-go](https://github.com/chasefleming/elem-go).
- Avoid wrappers unless they materially simplify lifecycle management or testing.
- Run the full test + lint suite before every commit/push.

## Daily Commands (from `nix develop`)

```
go test ./...
golangci-lint run ./...
prek run --all-files
nix flake check
```

## Repository Layout Highlights

- `cmd/nefit-homekit`: top-level binary.
- `config`, `events`, `homekit`, `web`, `nefit`, `logging`: core packages (stay modular but rooted).
- `flake.nix`: authoritative devShell, package, and NixOS module definitions.
- Keep repo-level planning notes up to date, but do not mention them inside documentation files.
- Web/kra server exposes `/`, `/api/{temperature,mode}`, `/events`, `/health`, `/metrics`, `/qrcode`, `/debug/eventbus`; keep parity with `tasmota-nefit`.
- GitHub Actions `ci.yml` runs go tests (coverage + race, Linux/macOS), golangci-lint, `nix build`, `nix flake check --all-systems`, and both NixOS VM tests. Match those locally before pushing.
- Flake apps: `nix run .#test`, `.#lint`, `.#test-race`, `.#coverage`—prefer these over make/adhoc scripts.

## Expectations

- Mirror structure/API decisions into `tasmota-nefit` when reasonable.
- Keep documentation concise (no emojis/faff); update README + this file when workflows change.
- When adding reusable helpers, consider pushing them to `../kra` or exporting them here for other repos.
