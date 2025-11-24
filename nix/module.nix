{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.nefit-homekit;
  dataDirStr = toString cfg.dataDir;
  hapDir = "${cfg.dataDir}/hap";
  tailscaleDir = "${cfg.dataDir}/tailscale";
  dataDirUnderVarLib = lib.hasPrefix "/var/lib/" dataDirStr;
  dataDirName = builtins.baseNameOf dataDirStr;
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

    tailscale = {
      authKeyFile = mkOption {
        type = types.nullOr types.path;
        default = null;
        description = ''
          Path to a file containing the Tailscale auth key.
          The contents of this file will be passed to the service via the
          NEFITHK_TAILSCALE_AUTHKEY environment variable.

          When set, this automatically enables Tailscale integration.

          This is more secure than putting the auth key in the environment
          or environmentFile options, as it allows using secrets management.
        '';
        example = "/run/secrets/tailscale-authkey";
      };

      hostname = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = ''
          Hostname to use on the Tailscale network.
          When unset, falls back to services.nefit-homekit.bridgeName.
          Only used when authKeyFile is set.
        '';
      };
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

    bridgeName = mkOption {
      type = types.str;
      default = "tasmota-homekit";
      description = ''
        Base name for the HomeKit bridge; also used as the default
        Tailscale hostname when tailscale.hostname is unset.
      '';
      example = "tasmota-homekit-dev";
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

    ports = {
      hap = mkOption {
        type = types.port;
        default = 12345;
        description = "Port for the HomeKit Accessory Protocol (HAP) server.";
      };

      web = mkOption {
        type = types.port;
        default = 8080;
        description = "Port for the web interface.";
      };
    };

    bindAddresses = {
      hap = mkOption {
        type = types.str;
        default = "0.0.0.0";
        description = "Address to bind the HAP listener to.";
      };

      web = mkOption {
        type = types.str;
        default = "0.0.0.0";
        description = "Address to bind the web listener to.";
      };
    };

    hapPin = mkOption {
      type = types.str;
      default = "00102003";
      description = ''
        HomeKit setup PIN code (must be exactly 8 digits).
        This is used to pair the accessory with HomeKit.
      '';
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/nefit-homekit";
      description = ''
        Root data directory for persistent service state. The module stores
        HomeKit data under `dataDir/hap` and Tailscale state under
        `dataDir/tailscale`.
      '';
    };

    log = {
      level = mkOption {
        type = types.enum [ "debug" "info" "warn" "error" ];
        default = "info";
        description = "Logging level for the service.";
      };

      format = mkOption {
        type = types.enum [ "json" "console" ];
        default = "json";
        description = "Logging format (json or console).";
      };
    };



    openFirewall = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to automatically open the firewall ports for HomeKit and mDNS.
        This opens:
        - TCP port for HAP (services.nefit-homekit.ports.hap)
        - TCP port for web interface (services.nefit-homekit.ports.web)
        - UDP port 5353 for mDNS (required for HomeKit discovery)
      '';
    };
  };

  config = mkIf cfg.enable (mkMerge [
    {
      # User and group setup
      users.users.${cfg.user} = {
        isSystemUser = true;
        group = cfg.group;
        description = "Nefit HomeKit service user";
        home = cfg.dataDir;
        createHome = true;
      };

      users.groups.${cfg.group} = { };

      # Systemd service
      systemd.services.nefit-homekit =
        let
          tailscaleHostname =
            if cfg.tailscale.hostname != null then cfg.tailscale.hostname else cfg.bridgeName;

          envVars = {
            NEFITHK_HAP_ADDR = "${cfg.bindAddresses.hap}:${toString cfg.ports.hap}";
            NEFITHK_WEB_ADDR = "${cfg.bindAddresses.web}:${toString cfg.ports.web}";
            NEFITHK_HAP_PORT = toString cfg.ports.hap;
            NEFITHK_WEB_PORT = toString cfg.ports.web;
            NEFITHK_HAP_PIN = cfg.hapPin;
            NEFITHK_HAP_STORAGE_PATH = hapDir;
            NEFITHK_LOG_LEVEL = cfg.log.level;
            NEFITHK_LOG_FORMAT = cfg.log.format;
            NEFITHK_BRIDGE_NAME = cfg.bridgeName;
            NEFITHK_TAILSCALE_HOSTNAME = tailscaleHostname;
            NEFITHK_TAILSCALE_STATE_DIR = tailscaleDir;
          } // cfg.environment;

          tailscaleExport =
            lib.optionalString (cfg.tailscale.authKeyFile != null) ''
              export NEFITHK_TAILSCALE_AUTHKEY="$(cat "$CREDENTIALS_DIRECTORY/tailscale-authkey")"
            '';

          startScript = pkgs.writeShellScript "nefit-homekit-start" ''
            set -euo pipefail
            ${tailscaleExport}
            exec ${cfg.package}/bin/nefit-homekit
          '';
        in
        {
          description = "Nefit Easy HomeKit Bridge";
          documentation = [ "https://github.com/kradalby/nefit-homekit" ];

          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];
          wantedBy = [ "multi-user.target" ];

          unitConfig = {
            StartLimitIntervalSec = "5min";
            StartLimitBurst = 5;
          };

          environment = envVars;

          serviceConfig =
            {
              Type = "simple";
              ExecStart = startScript;
              User = cfg.user;
              Group = cfg.group;

              Restart = "on-failure";
              RestartSec = "10s";
              RestartPreventExitStatus = [ 1 ];

              TimeoutStartSec = "60s";
              TimeoutStopSec = "30s";

              WorkingDirectory = cfg.dataDir;
              CacheDirectory = "nefit-homekit";
              RuntimeDirectory = "nefit-homekit";

              StandardOutput = "journal";
              StandardError = "journal";
              SyslogIdentifier = "nefit-homekit";

              UMask = "0077";
            }
            // (optionalAttrs dataDirUnderVarLib {
              StateDirectory = dataDirName;
              StateDirectoryMode = "0700";
            })
            // (optionalAttrs (cfg.environmentFile != null) {
              EnvironmentFile = cfg.environmentFile;
            })
            // (optionalAttrs (cfg.tailscale.authKeyFile != null) {
              LoadCredential = "tailscale-authkey:${cfg.tailscale.authKeyFile}";
            });
        };

      systemd.tmpfiles.rules = [
        "d ${cfg.dataDir} 0700 ${cfg.user} ${cfg.group} - -"
        "d ${hapDir} 0700 ${cfg.user} ${cfg.group} - -"
        "d ${tailscaleDir} 0700 ${cfg.user} ${cfg.group} - -"
      ];
    }

    # Firewall configuration
    (mkIf cfg.openFirewall {
      networking.firewall = {
        allowedTCPPorts = [
          cfg.ports.hap
          cfg.ports.web
        ];
        allowedUDPPorts = [
          5353 # mDNS for HomeKit discovery
        ];
      };
    })
  ]);
}
