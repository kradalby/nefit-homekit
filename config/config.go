// Package config provides configuration management for the nefit-homekit application.
// It handles loading configuration from environment variables and validation.
package config

import (
	"fmt"
	"time"

	"github.com/Netflix/go-env"
)

// Config holds all configuration for the nefit-homekit application.
type Config struct {
	// Nefit Easy Configuration
	NefitSerial    string `env:"NEFITHK_NEFIT_SERIAL,required=true"`
	NefitAccessKey string `env:"NEFITHK_NEFIT_ACCESS_KEY,required=true"`
	NefitPassword  string `env:"NEFITHK_NEFIT_PASSWORD,required=true"`

	// HomeKit Configuration
	HAPPin         string `env:"NEFITHK_HAP_PIN,default=00102003"`
	HAPStoragePath string `env:"NEFITHK_HAP_STORAGE_PATH,default=/var/lib/nefit-homekit"`
	HAPPort        int    `env:"NEFITHK_HAP_PORT,default=12345"`

	// Tailscale Configuration
	TailscaleEnabled  bool   `env:"NEFITHK_TAILSCALE_ENABLED,default=false"`
	TailscaleAuthKey  string `env:"NEFITHK_TAILSCALE_AUTHKEY"`
	TailscaleHostname string `env:"NEFITHK_TAILSCALE_HOSTNAME,default=nefit-homekit"`

	// Web Server Configuration
	WebPort        int    `env:"NEFITHK_WEB_PORT,default=8080"`
	WebBindAddress string `env:"NEFITHK_WEB_BIND_ADDRESS,default=0.0.0.0"`

	// XMPP Connection Configuration
	XMPPKeepaliveInterval time.Duration `env:"NEFITHK_XMPP_KEEPALIVE_INTERVAL,default=30s"`
	XMPPReconnectBackoff  time.Duration `env:"NEFITHK_XMPP_RECONNECT_BACKOFF,default=5s"`
	XMPPMaxReconnectWait  time.Duration `env:"NEFITHK_XMPP_MAX_RECONNECT_WAIT,default=5m"`

	// EventBus Configuration
	EventBusDebugEnabled bool `env:"NEFITHK_EVENTBUS_DEBUG_ENABLED,default=true"`

	// Logging
	LogLevel  string `env:"NEFITHK_LOG_LEVEL,default=info"`
	LogFormat string `env:"NEFITHK_LOG_FORMAT,default=json"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config

	_, err := env.UnmarshalFromEnviron(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate checks that the configuration is valid.
// Note: Required field validation is handled by go-env library.
func (c *Config) Validate() error {
	// Validate HAP pin format (must be 8 digits)
	if len(c.HAPPin) != 8 {
		return fmt.Errorf("HAP pin must be exactly 8 digits, got %d", len(c.HAPPin))
	}

	// Validate port ranges
	if c.HAPPort < 1 || c.HAPPort > 65535 {
		return fmt.Errorf("HAP port must be between 1 and 65535, got %d", c.HAPPort)
	}
	if c.WebPort < 1 || c.WebPort > 65535 {
		return fmt.Errorf("web port must be between 1 and 65535, got %d", c.WebPort)
	}

	// Validate timing configurations
	if c.XMPPKeepaliveInterval < time.Second {
		return fmt.Errorf("XMPP keepalive interval must be at least 1 second, got %s", c.XMPPKeepaliveInterval)
	}
	if c.XMPPReconnectBackoff < time.Second {
		return fmt.Errorf("XMPP reconnect backoff must be at least 1 second, got %s", c.XMPPReconnectBackoff)
	}
	if c.XMPPMaxReconnectWait < c.XMPPReconnectBackoff {
		return fmt.Errorf("XMPP max reconnect wait (%s) must be >= reconnect backoff (%s)", c.XMPPMaxReconnectWait, c.XMPPReconnectBackoff)
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, error", c.LogLevel)
	}

	// Validate log format
	validLogFormats := map[string]bool{
		"json":    true,
		"console": true,
	}
	if !validLogFormats[c.LogFormat] {
		return fmt.Errorf("invalid log format %q, must be one of: json, console", c.LogFormat)
	}

	return nil
}
