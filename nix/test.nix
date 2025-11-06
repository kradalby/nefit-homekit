# NixOS test for nefit-homekit module
# Tests that the service starts, configuration is loaded, and ports are accessible
#
# Run with: nix build .#checks.x86_64-linux.module-test
# or: nix-build -A checks.x86_64-linux.module-test

{ pkgs, system, self }:

pkgs.testers.runNixOSTest {
  name = "nefit-homekit-module";

  nodes.machine = { config, pkgs, ... }: {
    imports = [ self.nixosModules.default ];

    # Enable the service with test configuration
    services.nefit-homekit = {
      enable = true;

      # Use environment variables for test configuration
      environment = {
        # Required settings (these would normally come from environmentFile)
        NEFITHK_NEFIT_SERIAL = "test-serial-12345";
        NEFITHK_NEFIT_ACCESS_KEY = "test-access-key";
        NEFITHK_NEFIT_PASSWORD = "test-password";

        # Optional settings
        NEFITHK_HAP_PIN = "12345678";
        NEFITHK_HAP_PORT = "12345";
        NEFITHK_WEB_PORT = "8080";
        NEFITHK_LOG_LEVEL = "debug";
        NEFITHK_LOG_FORMAT = "json";
      };
    };

    # Open firewall for testing
    networking.firewall.enable = true;
  };

  testScript = ''
    # Start the machine
    machine.start()
    machine.wait_for_unit("multi-user.target")

    # Wait for the nefit-homekit service to start
    machine.wait_for_unit("nefit-homekit.service")

    # Check that the service is running
    machine.succeed("systemctl is-active nefit-homekit.service")

    # Check that the web server is listening on port 8080
    machine.wait_for_open_port(8080)

    # Check that the HAP server is listening on port 12345
    machine.wait_for_open_port(12345)

    # Test that we can reach the web interface
    machine.succeed("curl -f http://localhost:8080/")

    # Test that metrics endpoint is accessible
    machine.succeed("curl -f http://localhost:8080/metrics")

    # Check that the service has proper permissions (runs as nefit-homekit user)
    output = machine.succeed("systemctl show nefit-homekit.service -p User")
    assert "User=nefit-homekit" in output, f"Service should run as nefit-homekit user, got: {output}"

    # Verify the service can be stopped gracefully
    machine.succeed("systemctl stop nefit-homekit.service")
    machine.wait_until_fails("systemctl is-active nefit-homekit.service")

    # Verify the service can be restarted
    machine.succeed("systemctl start nefit-homekit.service")
    machine.wait_for_unit("nefit-homekit.service")
    machine.wait_for_open_port(8080)

    # Check logs for any errors (should fail gracefully with test credentials)
    logs = machine.succeed("journalctl -u nefit-homekit.service")
    print(f"Service logs:\n{logs}")
  '';
}
