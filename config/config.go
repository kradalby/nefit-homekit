// Package config provides configuration management for the nefit-homekit application.
// It handles loading configuration from environment variables and validation.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"time"

	"github.com/Netflix/go-env"
)

const (
	defaultBindAddress = "0.0.0.0"
	defaultHAPPort     = 12345
	defaultWebPort     = 8080

	defaultBridgeName = "nefit-homekit"
)

// Config holds all configuration for the nefit-homekit application.
type Config struct {
	BridgeName string `env:"NEFITHK_BRIDGE_NAME,default=nefit-homekit"`

	// Nefit Easy Configuration
	NefitSerial    string `env:"NEFITHK_NEFIT_SERIAL,required=true"`
	NefitAccessKey string `env:"NEFITHK_NEFIT_ACCESS_KEY,required=true"`
	NefitPassword  string `env:"NEFITHK_NEFIT_PASSWORD,required=true"`

	// HomeKit Configuration
	HAPPin         string `env:"NEFITHK_HAP_PIN,default=00102003"`
	HAPStoragePath string `env:"NEFITHK_HAP_STORAGE_PATH,default=/var/lib/nefit-homekit"`
	HAPAddr        string `env:"NEFITHK_HAP_ADDR"`
	HAPBindAddress string `env:"NEFITHK_HAP_BIND_ADDRESS,default=0.0.0.0"`
	HAPPort        int    `env:"NEFITHK_HAP_PORT,default=12345"`

	// Tailscale Configuration
	TailscaleAuthKey  string `env:"NEFITHK_TAILSCALE_AUTHKEY"`
	TailscaleHostname string `env:"NEFITHK_TAILSCALE_HOSTNAME"`
	TailscaleStateDir string `env:"NEFITHK_TAILSCALE_STATE_DIR"`

	// Web Server Configuration
	WebAddr        string `env:"NEFITHK_WEB_ADDR"`
	WebBindAddress string `env:"NEFITHK_WEB_BIND_ADDRESS,default=0.0.0.0"`
	WebPort        int    `env:"NEFITHK_WEB_PORT,default=8080"`

	// XMPP Connection Configuration
	XMPPKeepaliveInterval time.Duration `env:"NEFITHK_XMPP_KEEPALIVE_INTERVAL,default=30s"`
	XMPPReconnectBackoff  time.Duration `env:"NEFITHK_XMPP_RECONNECT_BACKOFF,default=5s"`
	XMPPMaxReconnectWait  time.Duration `env:"NEFITHK_XMPP_MAX_RECONNECT_WAIT,default=5m"`

	// EventBus Configuration
	EventBusDebugEnabled bool `env:"NEFITHK_EVENTBUS_DEBUG_ENABLED,default=true"`

	// Logging
	LogLevel  string `env:"NEFITHK_LOG_LEVEL,default=info"`
	LogFormat string `env:"NEFITHK_LOG_FORMAT,default=json"`

	hapAddr netip.AddrPort
	webAddr netip.AddrPort
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config

	_, err := env.UnmarshalFromEnviron(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	cfg.applyDerivedDefaults()

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

	if err := c.parseAddrPorts(); err != nil {
		return err
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

func (c *Config) applyDerivedDefaults() {
	if c.BridgeName == "" {
		c.BridgeName = defaultBridgeName
	}
	if c.TailscaleHostname == "" {
		c.TailscaleHostname = c.BridgeName
	}
}

func (c *Config) parseAddrPorts() error {
	if c.HAPBindAddress == "" {
		c.HAPBindAddress = defaultBindAddress
	}
	if c.HAPPort == 0 && !envVarSet("NEFITHK_HAP_PORT") {
		c.HAPPort = defaultHAPPort
	}
	if err := validatePortRange("HAP port", c.HAPPort); err != nil {
		return err
	}
	hapAddr := c.HAPAddr
	if hapAddr == "" {
		hapAddr = fmt.Sprintf("%s:%d", c.HAPBindAddress, c.HAPPort)
	}
	parsedHAP, err := netip.ParseAddrPort(hapAddr)
	if err != nil {
		return fmt.Errorf("invalid HAP addr %q: %w", hapAddr, err)
	}
	c.hapAddr = parsedHAP

	if c.WebBindAddress == "" {
		c.WebBindAddress = defaultBindAddress
	}
	if c.WebPort == 0 && !envVarSet("NEFITHK_WEB_PORT") {
		c.WebPort = defaultWebPort
	}
	if err := validatePortRange("web port", c.WebPort); err != nil {
		return err
	}

	webAddr := c.WebAddr
	if webAddr == "" {
		webAddr = fmt.Sprintf("%s:%d", c.WebBindAddress, c.WebPort)
	}
	parsedWeb, err := netip.ParseAddrPort(webAddr)
	if err != nil {
		return fmt.Errorf("invalid web addr %q: %w", webAddr, err)
	}
	c.webAddr = parsedWeb

	return nil
}

// HAPAddrPort returns the parsed HAP listener address.
func (c *Config) HAPAddrPort() netip.AddrPort {
	c.ensureAddrs()
	return c.hapAddr
}

// WebAddrPort returns the parsed web listener address.
func (c *Config) WebAddrPort() netip.AddrPort {
	c.ensureAddrs()
	return c.webAddr
}

func (c *Config) ensureAddrs() {
	if !c.hapAddr.IsValid() || !c.webAddr.IsValid() {
		if err := c.parseAddrPorts(); err != nil {
			panic(fmt.Sprintf("failed to parse listener addresses: %v", err))
		}
	}
}

// SetListenerAddrsForTesting overrides parsed listener addresses.
func (c *Config) SetListenerAddrsForTesting(hap, web string) {
	c.hapAddr = netip.MustParseAddrPort(hap)
	c.webAddr = netip.MustParseAddrPort(web)
}

func validatePortRange(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535, got %d", name, port)
	}
	return nil
}

func envVarSet(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}
