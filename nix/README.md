# NixOS Module and Tests

This directory contains the NixOS module and automated tests for nefit-homekit.

## Files

- `module.nix` - The main NixOS module that provides `services.nefit-homekit`
- `test.nix` - Basic module test (service startup, ports, permissions)
- `integration-test.nix` - Integration tests (environment files, multiple configurations, security)

## Running Tests

### Run all tests
```bash
nix flake check
```

### Run specific test
```bash
# Module test
nix build .#checks.x86_64-linux.module-test

# Integration test
nix build .#checks.x86_64-linux.integration-test
```

### Interactive testing
```bash
# Build and run test VM
nix build .#checks.x86_64-linux.module-test.driverInteractive
./result/bin/nixos-test-driver
```

## Test Coverage

### Module Test (`test.nix`)
- Service starts successfully
- Ports are opened (8080, 12345)
- Web interface is accessible
- Metrics endpoint works
- Service runs as correct user
- Graceful stop/start

### Integration Test (`integration-test.nix`)
- Environment file loading
- Environment variable override
- Multiple node configurations
- Security hardening validation:
  - Unprivileged user execution
  - `ProtectSystem=strict`
  - `PrivateTmp=yes`
  - Other systemd hardening options

## Adding New Tests

When adding new features, consider adding:

1. **Functional tests** - Does the feature work?
2. **Security tests** - Are permissions correct?
3. **Configuration tests** - Do all config options work?
4. **Regression tests** - Does it not break existing functionality?

### Example Test Structure

```nix
{ pkgs, system, self }:

pkgs.testers.runNixOSTest {
  name = "my-test";

  nodes.machine = { config, pkgs, ... }: {
    imports = [ self.nixosModules.default ];
    services.nefit-homekit = {
      enable = true;
      # ... configuration
    };
  };

  testScript = ''
    machine.start()
    machine.wait_for_unit("nefit-homekit.service")
    # ... test commands
  '';
}
```

## Test Maintenance

- Tests run automatically in CI (when configured)
- Update tests when adding new features
- Fix failing tests before merging
- Keep tests fast and focused

## Debugging Tests

If a test fails:

1. Check the test output for specific failures
2. Run interactively: `nix build .#checks.x86_64-linux.module-test.driverInteractive`
3. In the interactive shell, examine logs: `machine.succeed("journalctl -u nefit-homekit")`
4. Check service status: `machine.succeed("systemctl status nefit-homekit")`

## Known Limitations

- Tests use fake credentials (service will fail to connect to actual Nefit backend)
- Tests validate service startup and configuration, not full thermostat functionality
- Hardware testing requires actual Nefit Easy thermostat (see Phase 8 in main README)
