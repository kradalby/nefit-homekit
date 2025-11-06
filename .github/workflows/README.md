# GitHub Actions Workflows

This directory contains CI/CD workflows for the nefit-homekit project.

## Workflows

### Build (`build.yml`)
**Triggers:** Push to main/initial-work, PRs to main

Builds the application using Nix on multiple platforms:
- Ubuntu (Linux)
- macOS

**Steps:**
1. Checkout code
2. Install Nix (DeterminateSystems installer)
3. Setup Magic Nix Cache (GitHub Actions cache)
4. Build package with `nix build`
5. Verify binary works
6. Upload Linux binary as artifact

**Artifacts:**
- `nefit-homekit-linux` - Built binary (7-day retention)

---

### Tests (`test.yml`)
**Triggers:** Push to main/initial-work, PRs to main

Runs Go unit tests and linting:

**Unit Tests Job:**
- Runs on Ubuntu and macOS
- Executes tests with coverage
- Runs race detector
- Uploads coverage report as artifact (Ubuntu only)

**Lint Job:**
- Runs on Ubuntu
- Executes golangci-lint with 25+ linters

**Commands:**
```bash
go test -v -cover -coverprofile=coverage.out ./...
go test -race ./...
golangci-lint run ./...
```

---

### NixOS Tests (`nixos-tests.yml`)
**Triggers:** Push to main/initial-work, PRs to main

Runs NixOS integration tests in VMs:

**NixOS Tests Job:**
- Matrix strategy runs both tests in parallel
- Tests: `module-test`, `integration-test`
- Enables KVM for VM acceleration
- 30-minute timeout per test
- Shows logs on failure

**Flake Check Job:**
- Validates entire flake across all systems
- Runs `nix flake check --all-systems`
- 45-minute timeout

---

## Setup Requirements

### No Secrets Required!

All workflows use:
- **DeterminateSystems Nix Installer** - Official Nix installer for CI
- **Magic Nix Cache** - GitHub Actions cache integration (automatic)

No external services or tokens needed.

## Running Locally

You can run the same commands locally:

```bash
# Build
nix build

# Tests
nix develop --command go test -v ./...
nix develop --command go test -race ./...
nix develop --command golangci-lint run ./...

# NixOS tests (Linux only)
nix build .#checks.x86_64-linux.module-test
nix build .#checks.x86_64-linux.integration-test

# All checks
nix flake check
```

## Caching Strategy

Workflows use **Magic Nix Cache** from DeterminateSystems:
- Automatic integration with GitHub Actions cache
- No configuration needed
- Free for public repositories
- **Benefits:**
  - Faster CI runs (caches Nix store paths)
  - Reduced build times
  - No external services required
  - Works seamlessly across workflow runs

## Status Badges

Add to README.md:

```markdown
[![Build](https://github.com/kradalby/nefit-homekit/actions/workflows/build.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/build.yml)
[![Tests](https://github.com/kradalby/nefit-homekit/actions/workflows/test.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/test.yml)
[![NixOS Tests](https://github.com/kradalby/nefit-homekit/actions/workflows/nixos-tests.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/nixos-tests.yml)
```

## Troubleshooting

### NixOS tests timeout
- Default timeout: 30 minutes per test
- Increase if needed in workflow file
- Check if KVM is properly enabled

### Build failures
- Check Nix cache is accessible
- Verify flake.lock is committed
- Review build logs in Actions tab

### Test failures
- Check if tests pass locally
- Review race detector output
- Ensure go.mod/go.sum are in sync

## Performance

Typical run times (with caching):
- Build: 2-5 minutes
- Tests: 3-5 minutes
- NixOS tests: 10-15 minutes per test
- Flake check: 15-30 minutes

First run (no cache): 15-30 minutes per job
