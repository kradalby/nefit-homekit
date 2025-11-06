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
2. Install Nix with flakes enabled
3. Setup Cachix for binary caching
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
- Uploads coverage to Codecov (Ubuntu only)

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

### Required Secrets

#### Optional (for caching):
- `CACHIX_AUTH_TOKEN` - Cachix authentication token for binary cache
  - Speeds up builds significantly
  - Not required but highly recommended
  - Get from: https://app.cachix.org

#### Optional (for coverage):
- `CODECOV_TOKEN` - Codecov token for coverage uploads
  - Get from: https://codecov.io
  - Only used on Ubuntu builds

### Setting Secrets

1. Go to repository Settings → Secrets and variables → Actions
2. Click "New repository secret"
3. Add each secret with its token value

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

Workflows use Cachix for binary caching:
- **Public cache:** `nefit-homekit` (read-only for PRs)
- **Push access:** Main branch and authorized users only
- **Benefits:**
  - Faster CI runs (downloads vs rebuilds)
  - Reduced GitHub Actions minutes
  - Better developer experience

## Status Badges

Add to README.md:

```markdown
[![Build](https://github.com/kradalby/nefit-homekit/actions/workflows/build.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/build.yml)
[![Tests](https://github.com/kradalby/nefit-homekit/actions/workflows/test.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/test.yml)
[![NixOS Tests](https://github.com/kradalby/nefit-homekit/actions/workflows/nixos-tests.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/nixos-tests.yml)
[![codecov](https://codecov.io/gh/kradalby/nefit-homekit/branch/main/graph/badge.svg)](https://codecov.io/gh/kradalby/nefit-homekit)
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
