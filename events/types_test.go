package events

import (
	"testing"
	"time"
)

func TestStateUpdateEvent(t *testing.T) {
	now := time.Now()
	event := StateUpdateEvent{
		Timestamp:          now,
		Source:             "nefit",
		CurrentTemperature: 21.5,
		TargetTemperature:  22.0,
		HeatingActive:      true,
		Mode:               "heat",
		Pressure:           1.5,
		HotWaterActive:     true,
		HotWaterTemperature: 55.0,
	}

	if event.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, now)
	}
	if event.Source != "nefit" {
		t.Errorf("Source = %v, want nefit", event.Source)
	}
	if event.CurrentTemperature != 21.5 {
		t.Errorf("CurrentTemperature = %v, want 21.5", event.CurrentTemperature)
	}
}

func TestCommandEvent(t *testing.T) {
	now := time.Now()
	temp := 23.0
	mode := "heat"
	hotWater := true

	event := CommandEvent{
		Timestamp:         now,
		Source:            "homekit",
		CommandType:       CommandTypeSetTemperature,
		TargetTemperature: &temp,
		Mode:              &mode,
		HotWaterEnabled:   &hotWater,
	}

	if event.Source != "homekit" {
		t.Errorf("Source = %v, want homekit", event.Source)
	}
	if event.CommandType != CommandTypeSetTemperature {
		t.Errorf("CommandType = %v, want %v", event.CommandType, CommandTypeSetTemperature)
	}
	if event.TargetTemperature == nil || *event.TargetTemperature != 23.0 {
		t.Errorf("TargetTemperature = %v, want 23.0", event.TargetTemperature)
	}
}

func TestConnectionStatusEvent(t *testing.T) {
	now := time.Now()
	event := ConnectionStatusEvent{
		Timestamp:  now,
		Component:  "nefit",
		Status:     ConnectionStatusConnected,
		Error:      "",
		Reconnects: 0,
	}

	if event.Component != "nefit" {
		t.Errorf("Component = %v, want nefit", event.Component)
	}
	if event.Status != ConnectionStatusConnected {
		t.Errorf("Status = %v, want %v", event.Status, ConnectionStatusConnected)
	}
	if event.Error != "" {
		t.Errorf("Error = %v, want empty string", event.Error)
	}
}

func TestEventTypes(t *testing.T) {
	tests := []struct {
		name     string
		eventType EventType
		want     string
	}{
		{"state update", EventTypeStateUpdate, "state_update"},
		{"command", EventTypeCommand, "command"},
		{"connection status", EventTypeConnectionStatus, "connection_status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.eventType) != tt.want {
				t.Errorf("EventType = %v, want %v", tt.eventType, tt.want)
			}
		})
	}
}

func TestCommandTypes(t *testing.T) {
	tests := []struct {
		name        string
		commandType CommandType
		want        string
	}{
		{"set temperature", CommandTypeSetTemperature, "set_temperature"},
		{"set mode", CommandTypeSetMode, "set_mode"},
		{"set hot water", CommandTypeSetHotWater, "set_hot_water"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.commandType) != tt.want {
				t.Errorf("CommandType = %v, want %v", tt.commandType, tt.want)
			}
		})
	}
}

func TestConnectionStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status ConnectionStatus
		want   string
	}{
		{"disconnected", ConnectionStatusDisconnected, "disconnected"},
		{"connecting", ConnectionStatusConnecting, "connecting"},
		{"connected", ConnectionStatusConnected, "connected"},
		{"reconnecting", ConnectionStatusReconnecting, "reconnecting"},
		{"failed", ConnectionStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("ConnectionStatus = %v, want %v", tt.status, tt.want)
			}
		})
	}
}

func TestStateUpdateEventEquals(t *testing.T) {
	baseEvent := StateUpdateEvent{
		Timestamp:           time.Now(),
		Source:              "nefit",
		CurrentTemperature:  21.5,
		TargetTemperature:   22.0,
		HeatingActive:       true,
		Mode:                "heat",
		Pressure:            1.5,
		HotWaterActive:      true,
		HotWaterTemperature: 55.0,
	}

	tests := []struct {
		name  string
		event StateUpdateEvent
		want  bool
	}{
		{
			name:  "identical event",
			event: baseEvent,
			want:  true,
		},
		{
			name: "different timestamp and source (should still be equal)",
			event: StateUpdateEvent{
				Timestamp:           time.Now().Add(time.Hour),
				Source:              "web",
				CurrentTemperature:  21.5,
				TargetTemperature:   22.0,
				HeatingActive:       true,
				Mode:                "heat",
				Pressure:            1.5,
				HotWaterActive:      true,
				HotWaterTemperature: 55.0,
			},
			want: true,
		},
		{
			name: "different current temperature",
			event: StateUpdateEvent{
				Timestamp:           baseEvent.Timestamp,
				Source:              baseEvent.Source,
				CurrentTemperature:  20.0,
				TargetTemperature:   baseEvent.TargetTemperature,
				HeatingActive:       baseEvent.HeatingActive,
				Mode:                baseEvent.Mode,
				Pressure:            baseEvent.Pressure,
				HotWaterActive:      baseEvent.HotWaterActive,
				HotWaterTemperature: baseEvent.HotWaterTemperature,
			},
			want: false,
		},
		{
			name: "different target temperature",
			event: StateUpdateEvent{
				Timestamp:           baseEvent.Timestamp,
				Source:              baseEvent.Source,
				CurrentTemperature:  baseEvent.CurrentTemperature,
				TargetTemperature:   23.0,
				HeatingActive:       baseEvent.HeatingActive,
				Mode:                baseEvent.Mode,
				Pressure:            baseEvent.Pressure,
				HotWaterActive:      baseEvent.HotWaterActive,
				HotWaterTemperature: baseEvent.HotWaterTemperature,
			},
			want: false,
		},
		{
			name: "different heating active",
			event: StateUpdateEvent{
				Timestamp:           baseEvent.Timestamp,
				Source:              baseEvent.Source,
				CurrentTemperature:  baseEvent.CurrentTemperature,
				TargetTemperature:   baseEvent.TargetTemperature,
				HeatingActive:       false,
				Mode:                baseEvent.Mode,
				Pressure:            baseEvent.Pressure,
				HotWaterActive:      baseEvent.HotWaterActive,
				HotWaterTemperature: baseEvent.HotWaterTemperature,
			},
			want: false,
		},
		{
			name: "different mode",
			event: StateUpdateEvent{
				Timestamp:           baseEvent.Timestamp,
				Source:              baseEvent.Source,
				CurrentTemperature:  baseEvent.CurrentTemperature,
				TargetTemperature:   baseEvent.TargetTemperature,
				HeatingActive:       baseEvent.HeatingActive,
				Mode:                "off",
				Pressure:            baseEvent.Pressure,
				HotWaterActive:      baseEvent.HotWaterActive,
				HotWaterTemperature: baseEvent.HotWaterTemperature,
			},
			want: false,
		},
		{
			name: "tiny temperature difference within epsilon",
			event: StateUpdateEvent{
				Timestamp:           baseEvent.Timestamp,
				Source:              baseEvent.Source,
				CurrentTemperature:  21.505,
				TargetTemperature:   22.005,
				HeatingActive:       baseEvent.HeatingActive,
				Mode:                baseEvent.Mode,
				Pressure:            baseEvent.Pressure,
				HotWaterActive:      baseEvent.HotWaterActive,
				HotWaterTemperature: baseEvent.HotWaterTemperature,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := baseEvent.Equals(tt.event)
			if got != tt.want {
				t.Errorf("Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}
