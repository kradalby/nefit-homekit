package logging

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		format  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid json logger with debug level",
			level:   "debug",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "valid json logger with info level",
			level:   "info",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "valid json logger with warn level",
			level:   "warn",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "valid json logger with error level",
			level:   "error",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "valid console logger",
			level:   "info",
			format:  "console",
			wantErr: false,
		},
		{
			name:    "invalid log level",
			level:   "invalid",
			format:  "json",
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name:    "invalid log format",
			level:   "info",
			format:  "xml",
			wantErr: true,
			errMsg:  "invalid log format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.level, tt.format)

			if tt.wantErr {
				if err == nil {
					t.Errorf("New() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("New() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("New() unexpected error = %v", err)
				return
			}

			if logger == nil {
				t.Fatal("New() returned nil logger")
			}

			// Cleanup
			_ = logger.Sync()
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		wantLevel zapcore.Level
		wantErr   bool
	}{
		{
			name:      "debug level",
			level:     "debug",
			wantLevel: zapcore.DebugLevel,
			wantErr:   false,
		},
		{
			name:      "info level",
			level:     "info",
			wantLevel: zapcore.InfoLevel,
			wantErr:   false,
		},
		{
			name:      "warn level",
			level:     "warn",
			wantLevel: zapcore.WarnLevel,
			wantErr:   false,
		},
		{
			name:      "error level",
			level:     "error",
			wantLevel: zapcore.ErrorLevel,
			wantErr:   false,
		},
		{
			name:      "invalid level",
			level:     "fatal",
			wantLevel: zapcore.InfoLevel, // Default fallback
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLevel, err := parseLevel(tt.level)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseLevel() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseLevel() unexpected error = %v", err)
				return
			}

			if gotLevel != tt.wantLevel {
				t.Errorf("parseLevel() = %v, want %v", gotLevel, tt.wantLevel)
			}
		})
	}
}

func TestLoggerOutput(t *testing.T) {
	// Test that logger can actually log without panicking
	logger, err := New("info", "json")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	// These should not panic
	logger.Info("test info message")
	logger.Debug("test debug message") // Should not appear with info level
	logger.Warn("test warn message")
	logger.Error("test error message")
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexString(s, substr) >= 0
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
