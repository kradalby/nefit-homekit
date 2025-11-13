// Package events provides event definitions and eventbus management.
package events

import (
	"time"
)

// EventType represents the type of event.
type EventType string

const (
	// EventTypeStateUpdate is emitted when thermostat state changes.
	EventTypeStateUpdate EventType = "state_update"

	// EventTypeCommand is emitted when a command is issued to the thermostat.
	EventTypeCommand EventType = "command"

	// EventTypeConnectionStatus is emitted when connection status changes.
	EventTypeConnectionStatus EventType = "connection_status"
)

// StateUpdateEvent is published when the thermostat state changes.
type StateUpdateEvent struct {
	Timestamp           time.Time
	Source              string  // "nefit", "homekit", "web"
	CurrentTemperature  float64 // Celsius
	TargetTemperature   float64 // Celsius
	HeatingActive       bool
	Mode                string // "heat", "off"
	Pressure            float64 // Bar
	HotWaterActive      bool
	HotWaterTemperature float64 // Celsius
}

// Equals compares two StateUpdateEvent for equality, ignoring Timestamp and Source.
// This is used for event deduplication.
func (e StateUpdateEvent) Equals(other StateUpdateEvent) bool {
	const epsilon = 0.01 // Temperature comparison tolerance

	return abs(e.CurrentTemperature-other.CurrentTemperature) < epsilon &&
		abs(e.TargetTemperature-other.TargetTemperature) < epsilon &&
		e.HeatingActive == other.HeatingActive &&
		e.Mode == other.Mode &&
		abs(e.Pressure-other.Pressure) < epsilon &&
		e.HotWaterActive == other.HotWaterActive &&
		abs(e.HotWaterTemperature-other.HotWaterTemperature) < epsilon
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// CommandEvent is published when a command should be executed.
type CommandEvent struct {
	Timestamp         time.Time
	Source            string // "homekit", "web"
	CommandType       CommandType
	TargetTemperature *float64 // For SetTemperature
	Mode              *string  // For SetMode
	HotWaterEnabled   *bool    // For SetHotWater
}

// CommandType represents the type of command.
type CommandType string

const (
	// CommandTypeSetTemperature sets the target temperature.
	CommandTypeSetTemperature CommandType = "set_temperature"

	// CommandTypeSetMode sets the thermostat mode.
	CommandTypeSetMode CommandType = "set_mode"

	// CommandTypeSetHotWater enables/disables hot water.
	CommandTypeSetHotWater CommandType = "set_hot_water"
)

// ConnectionStatusEvent is published when connection status changes.
type ConnectionStatusEvent struct {
	Timestamp  time.Time
	Component  string // "nefit", "homekit", "web"
	Status     ConnectionStatus
	Error      string // Empty if no error
	Reconnects int    // Number of reconnection attempts
}

// ConnectionStatus represents the connection status.
type ConnectionStatus string

const (
	// ConnectionStatusDisconnected means not connected.
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"

	// ConnectionStatusConnecting means attempting to connect.
	ConnectionStatusConnecting ConnectionStatus = "connecting"

	// ConnectionStatusConnected means successfully connected.
	ConnectionStatusConnected ConnectionStatus = "connected"

	// ConnectionStatusReconnecting means attempting to reconnect.
	ConnectionStatusReconnecting ConnectionStatus = "reconnecting"

	// ConnectionStatusFailed means connection failed.
	ConnectionStatusFailed ConnectionStatus = "failed"
)
