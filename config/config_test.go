package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid configuration with all required fields",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
			},
			wantErr: false,
		},
		{
			name: "missing nefit serial",
			envVars: map[string]string{
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
			},
			wantErr: true,
			errMsg:  "NEFITHK_NEFIT_SERIAL",
		},
		{
			name: "missing nefit access key",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":   "123456789",
				"NEFITHK_NEFIT_PASSWORD": "password123",
			},
			wantErr: true,
			errMsg:  "NEFITHK_NEFIT_ACCESS_KEY",
		},
		{
			name: "missing nefit password",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
			},
			wantErr: true,
			errMsg:  "NEFITHK_NEFIT_PASSWORD",
		},
		{
			name: "invalid HAP pin (too short)",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_HAP_PIN":          "123",
			},
			wantErr: true,
			errMsg:  "HAP pin must be exactly 8 digits",
		},
		{
			name: "invalid HAP pin (too long)",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_HAP_PIN":          "123456789",
			},
			wantErr: true,
			errMsg:  "HAP pin must be exactly 8 digits",
		},
		{
			name: "invalid HAP port (too low)",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_HAP_PORT":         "0",
			},
			wantErr: true,
			errMsg:  "HAP port must be between 1 and 65535",
		},
		{
			name: "invalid HAP port (too high)",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_HAP_PORT":         "65536",
			},
			wantErr: true,
			errMsg:  "HAP port must be between 1 and 65535",
		},
		{
			name: "invalid web port",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_WEB_PORT":         "100000",
			},
			wantErr: true,
			errMsg:  "web port must be between 1 and 65535",
		},
		{
			name: "invalid log level",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_LOG_LEVEL":        "invalid",
			},
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name: "invalid log format",
			envVars: map[string]string{
				"NEFITHK_NEFIT_SERIAL":     "123456789",
				"NEFITHK_NEFIT_ACCESS_KEY": "accesskey123",
				"NEFITHK_NEFIT_PASSWORD":   "password123",
				"NEFITHK_LOG_FORMAT":       "xml",
			},
			wantErr: true,
			errMsg:  "invalid log format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			clearEnv(t)

			// Set test environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := Load()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error = %v", err)
				return
			}

			// Verify defaults are applied
			if cfg == nil {
				t.Fatal("Load() returned nil config")
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	clearEnv(t)

	// Set only required fields
	t.Setenv("NEFITHK_NEFIT_SERIAL", "123456789")
	t.Setenv("NEFITHK_NEFIT_ACCESS_KEY", "accesskey123")
	t.Setenv("NEFITHK_NEFIT_PASSWORD", "password123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error = %v", err)
	}

	// Check defaults
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"HAPPin", cfg.HAPPin, "00102003"},
		{"HAPStoragePath", cfg.HAPStoragePath, "/var/lib/nefit-homekit"},
		{"HAPPort", cfg.HAPPort, 12345},
		{"TailscaleEnabled", cfg.TailscaleEnabled, false},
		{"TailscaleHostname", cfg.TailscaleHostname, "nefit-homekit"},
		{"WebPort", cfg.WebPort, 8080},
		{"WebBindAddress", cfg.WebBindAddress, "0.0.0.0"},
		{"XMPPKeepaliveInterval", cfg.XMPPKeepaliveInterval, 30 * time.Second},
		{"XMPPReconnectBackoff", cfg.XMPPReconnectBackoff, 5 * time.Second},
		{"XMPPMaxReconnectWait", cfg.XMPPMaxReconnectWait, 5 * time.Minute},
		{"EventBusDebugEnabled", cfg.EventBusDebugEnabled, true},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFormat", cfg.LogFormat, "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestValidate_XMPPTimings(t *testing.T) {
	tests := []struct {
		name               string
		keepalive          time.Duration
		reconnectBackoff   time.Duration
		maxReconnectWait   time.Duration
		wantErr            bool
		errMsg             string
	}{
		{
			name:             "keepalive too short",
			keepalive:        500 * time.Millisecond,
			reconnectBackoff: 5 * time.Second,
			maxReconnectWait: 5 * time.Minute,
			wantErr:          true,
			errMsg:           "XMPP keepalive interval must be at least 1 second",
		},
		{
			name:             "reconnect backoff too short",
			keepalive:        30 * time.Second,
			reconnectBackoff: 500 * time.Millisecond,
			maxReconnectWait: 5 * time.Minute,
			wantErr:          true,
			errMsg:           "XMPP reconnect backoff must be at least 1 second",
		},
		{
			name:             "max reconnect wait less than backoff",
			keepalive:        30 * time.Second,
			reconnectBackoff: 10 * time.Second,
			maxReconnectWait: 5 * time.Second,
			wantErr:          true,
			errMsg:           "XMPP max reconnect wait",
		},
		{
			name:             "valid timings",
			keepalive:        30 * time.Second,
			reconnectBackoff: 5 * time.Second,
			maxReconnectWait: 5 * time.Minute,
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				NefitSerial:           "123456789",
				NefitAccessKey:        "accesskey123",
				NefitPassword:         "password123",
				HAPPin:                "00102003",
				HAPPort:               12345,
				WebPort:               8080,
				XMPPKeepaliveInterval: tt.keepalive,
				XMPPReconnectBackoff:  tt.reconnectBackoff,
				XMPPMaxReconnectWait:  tt.maxReconnectWait,
				LogLevel:              "info",
				LogFormat:             "json",
			}

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

// clearEnv clears all NEFITHK_* environment variables.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		if len(env) > 14 && env[:14] == "NEFITHK_" {
			key := env[:indexByte(env, '=')]
			if err := os.Unsetenv(key); err != nil {
				t.Fatalf("failed to unset env var %s: %v", key, err)
			}
		}
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexString(s, substr) >= 0
}

// indexByte returns the index of the first instance of c in s, or -1 if c is not present.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// indexString returns the index of the first instance of substr in s, or -1 if substr is not present.
func indexString(s, substr string) int {
	n := len(substr)
	if n == 0 {
		return 0
	}
	if n > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-n; i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}
