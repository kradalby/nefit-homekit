{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.nefit-homekit;
in
{
  options.services.nefit-homekit = {
    enable = mkEnableOption "Nefit Easy HomeKit bridge";

    package = mkOption {
      type = types.package;
      description = "The nefit-homekit package to use.";
    };

    environmentFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Environment file containing NEFITHK_* configuration variables.

        Example file contents:
        ```
        NEFITHK_NEFIT_SERIAL=your-serial
        NEFITHK_NEFIT_ACCESS_KEY=your-key
        NEFITHK_NEFIT_PASSWORD=your-password
        NEFITHK_HAP_PIN=12345678
        NEFITHK_HAP_PORT=12345
        NEFITHK_WEB_PORT=8080
        ```
      '';
      example = "/etc/nefit-homekit/env";
    };

    environment = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = ''
        Environment variables to set for the service.
        These should use the NEFITHK_ prefix.

        Note: For sensitive values like passwords, use environmentFile instead.
      '';
      example = literalExpression ''
        {
          NEFITHK_NEFIT_SERIAL = "your-serial";
          NEFITHK_NEFIT_ACCESS_KEY = "your-key";
          NEFITHK_HAP_PIN = "12345678";
          NEFITHK_LOG_LEVEL = "debug";
        }
      '';
    };

    user = mkOption {
      type = types.str;
      default = "nefit-homekit";
      description = "User account under which nefit-homekit runs.";
    };

    group = mkOption {
      type = types.str;
      default = "nefit-homekit";
      description = "Group under which nefit-homekit runs.";
    };
  };

  config = mkIf cfg.enable {
    # User and group setup
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      description = "Nefit HomeKit service user";
      home = "/var/lib/nefit-homekit";
      createHome = true;
    };

    users.groups.${cfg.group} = { };

    # Systemd service
    systemd.services.nefit-homekit = {
      description = "Nefit Easy HomeKit Bridge";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      wantedBy = [ "multi-user.target" ];

      environment = cfg.environment;

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        Restart = "on-failure";
        RestartSec = "10s";

        # Load environment from file if specified
        EnvironmentFile = mkIf (cfg.environmentFile != null) cfg.environmentFile;

        ExecStart = "${cfg.package}/bin/nefit-homekit";

        # Working directory
        WorkingDirectory = "/var/lib/nefit-homekit";

        # Security hardening
        # Filesystem access
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        ReadWritePaths = [
          "/var/lib/nefit-homekit"
        ];

        # Capabilities
        NoNewPrivileges = true;
        PrivateDevices = true;
        ProtectHostname = true;
        ProtectClock = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        RemoveIPC = true;

        # System call filtering
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
          "~@resources"
        ];
        SystemCallErrorNumber = "EPERM";
        SystemCallArchitectures = "native";

        # Process properties
        UMask = "0077";
      };
    };

    # Create storage directory
    systemd.tmpfiles.rules = [
      "d '/var/lib/nefit-homekit' 0700 ${cfg.user} ${cfg.group} - -"
    ];
  };
}
