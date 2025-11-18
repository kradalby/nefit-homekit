# GitHub Actions Workflows

This directory contains CI/CD workflows for the nefit-homekit project.

## Workflows

### CI (`ci.yml`)

**Triggers:** Push to `main`/`initial-work`, PRs targeting `main`.

Jobs:

1. **Tests**
   - Matrix: `ubuntu-latest`, `macos-latest`
   - Runs `go test -v -cover` (writes `coverage.out`)
   - Displays coverage summary and runs `go test -race`
   - Uploads coverage artifact from the Linux job

2. **Lint**
   - Runs `nix develop --command golangci-lint run ./...`

3. **Build**
   - Matrix: `ubuntu-latest`, `macos-latest`
   - Builds via `nix build -L .#nefit-homekit`
   - Emits Linux binary artifact for quick downloads

4. **Flake Check**
   - Executes `nix flake check --all-systems`

5. **NixOS Tests**
   - Matrix over `checks.x86_64-linux.module-test` and `checks.x86_64-linux.integration-test`
   - Enables KVM, runs each VM test with a 30-minute timeout
   - Dumps logs if a test fails

**Artifacts**

- `coverage-report`: Go coverage profile (Linux only)
- `nefit-homekit-linux`: built binary (7-day retention)

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
[![CI](https://github.com/kradalby/nefit-homekit/actions/workflows/ci.yml/badge.svg)](https://github.com/kradalby/nefit-homekit/actions/workflows/ci.yml)
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
