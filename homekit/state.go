// Package homekit wires brutella/hap into the shared eventbus so HomeKit and the
// thermostat stay in sync.
package homekit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const stateFileName = "temperature_state.json"

// temperatureState represents the persisted state.
type temperatureState struct {
	PreviousTemperature float64 `json:"previous_temperature"`
}

// loadPreviousTemperature loads the previously saved temperature from disk.
// Returns an error if the file doesn't exist or can't be read.
func loadPreviousTemperature(storagePath string) (float64, error) {
	statePath := filepath.Join(storagePath, stateFileName)

	//nolint:gosec // statePath is constructed from trusted config path
	data, err := os.ReadFile(statePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read state file: %w", err)
	}

	var state temperatureState
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return state.PreviousTemperature, nil
}

// savePreviousTemperature saves the temperature to disk atomically.
func savePreviousTemperature(storagePath string, temp float64) error {
	state := temperatureState{
		PreviousTemperature: temp,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	statePath := filepath.Join(storagePath, stateFileName)

	// Write atomically using a temp file
	tempPath := statePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	if err := os.Rename(tempPath, statePath); err != nil {
		return fmt.Errorf("failed to rename temp state file: %w", err)
	}

	return nil
}
