# Integration test that validates environment file loading
# and tests multiple configuration scenarios

{ pkgs, system, self }:

pkgs.testers.runNixOSTest {
  name = "nefit-homekit-integration";

  nodes = {
    # Test with environmentFile
    withEnvFile = { config, pkgs, ... }: {
      imports = [ self.nixosModules.default ];

      # Create a test environment file
      environment.etc."nefit-homekit/test.env" = {
        text = ''
          NEFITHK_NEFIT_SERIAL=serial-from-file
          NEFITHK_NEFIT_ACCESS_KEY=key-from-file
          NEFITHK_NEFIT_PASSWORD=password-from-file
          NEFITHK_WEB_PORT=9090
          NEFITHK_HAP_PORT=54321
        '';
        mode = "0600";
        user = "nefit-homekit";
      };

      services.nefit-homekit = {
        enable = true;
        environmentFile = "/etc/nefit-homekit/test.env";

        # Additional env vars that override or supplement the file
        environment = {
          NEFITHK_LOG_LEVEL = "debug";
        };
      };

      networking.firewall.enable = true;
    };

    # Test with minimal configuration
    minimal = { config, pkgs, ... }: {
      imports = [ self.nixosModules.default ];

      services.nefit-homekit = {
        enable = true;
        environment = {
          NEFITHK_NEFIT_SERIAL = "minimal-test";
          NEFITHK_NEFIT_ACCESS_KEY = "minimal-key";
          NEFITHK_NEFIT_PASSWORD = "minimal-pass";
        };
      };
    };
  };

  testScript = ''
    # Test environmentFile node
    withEnvFile.start()
    withEnvFile.wait_for_unit("multi-user.target")
    withEnvFile.wait_for_unit("nefit-homekit.service")

    # Verify custom ports are used
    withEnvFile.wait_for_open_port(9090)
    withEnvFile.wait_for_open_port(54321)

    # Verify web interface responds
    withEnvFile.succeed("curl -f http://localhost:9090/")

    # Test minimal configuration node
    minimal.start()
    minimal.wait_for_unit("multi-user.target")
    minimal.wait_for_unit("nefit-homekit.service")

    # Should use default ports
    minimal.wait_for_open_port(8080)
    minimal.wait_for_open_port(12345)

    # Verify both nodes can run simultaneously without conflicts
    withEnvFile.succeed("systemctl is-active nefit-homekit.service")
    minimal.succeed("systemctl is-active nefit-homekit.service")

    # Check security hardening is in place
    for machine in [withEnvFile, minimal]:
        # Verify service runs as unprivileged user
        output = machine.succeed("systemctl show nefit-homekit.service -p User")
        assert "User=nefit-homekit" in output

        # Verify ProtectSystem is enabled
        output = machine.succeed("systemctl show nefit-homekit.service -p ProtectSystem")
        assert "ProtectSystem=strict" in output

        # Verify PrivateTmp is enabled
        output = machine.succeed("systemctl show nefit-homekit.service -p PrivateTmp")
        assert "PrivateTmp=yes" in output

    print("✓ All integration tests passed")
  '';
}
