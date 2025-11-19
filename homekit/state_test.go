package homekit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavePreviousTemperature(t *testing.T) {
	tempDir := t.TempDir()
	temp := 22.5

	err := savePreviousTemperature(tempDir, temp)
	if err != nil {
		t.Fatalf("savePreviousTemperature() error = %v", err)
	}

	// Verify file was created
	statePath := filepath.Join(tempDir, stateFileName)
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Errorf("state file was not created at %s", statePath)
	}
}

func TestLoadPreviousTemperature(t *testing.T) {
	tempDir := t.TempDir()
	expectedTemp := 21.0

	// Save a temperature first
	err := savePreviousTemperature(tempDir, expectedTemp)
	if err != nil {
		t.Fatalf("savePreviousTemperature() error = %v", err)
	}

	// Load it back
	loadedTemp, err := loadPreviousTemperature(tempDir)
	if err != nil {
		t.Fatalf("loadPreviousTemperature() error = %v", err)
	}

	if loadedTemp != expectedTemp {
		t.Errorf("loadPreviousTemperature() = %v, want %v", loadedTemp, expectedTemp)
	}
}

func TestLoadPreviousTemperatureNotFound(t *testing.T) {
	tempDir := t.TempDir()

	// Try to load when no file exists
	_, err := loadPreviousTemperature(tempDir)
	if err == nil {
		t.Error("loadPreviousTemperature() expected error when file doesn't exist, got nil")
	}
}

func TestSavePreviousTemperatureOverwrite(t *testing.T) {
	tempDir := t.TempDir()

	// Save first temperature
	err := savePreviousTemperature(tempDir, 20.0)
	if err != nil {
		t.Fatalf("savePreviousTemperature() first save error = %v", err)
	}

	// Overwrite with new temperature
	newTemp := 23.5
	err = savePreviousTemperature(tempDir, newTemp)
	if err != nil {
		t.Fatalf("savePreviousTemperature() second save error = %v", err)
	}

	// Load and verify it's the new temperature
	loadedTemp, err := loadPreviousTemperature(tempDir)
	if err != nil {
		t.Fatalf("loadPreviousTemperature() error = %v", err)
	}

	if loadedTemp != newTemp {
		t.Errorf("loadPreviousTemperature() = %v, want %v", loadedTemp, newTemp)
	}
}
