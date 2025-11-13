package nefit

import (
	"testing"
	"time"

	"github.com/kradalby/nefit-go/types"
	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"go.uber.org/zap"
	"tailscale.com/util/eventbus"
)

const (
	sourceNefit = "nefit"
	testModeOff = "off"
)

func TestNew(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	client, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	if client == nil {
		t.Fatal("New() returned nil client")
	}

	if client.nefitClient == nil {
		t.Fatal("client.nefitClient is nil")
	}
}

func TestNewWithNilConfig(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	_, err = New(nil, logger, bus)
	if err == nil {
		t.Error("New(nil config) expected error, got nil")
	}
}

func TestNewWithNilLogger(t *testing.T) {
	bus, err := events.New(zap.NewNop())
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	_, err = New(cfg, nil, bus)
	if err == nil {
		t.Error("New(nil logger) expected error, got nil")
	}
}

func TestNewWithNilBus(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	_, err := New(cfg, logger, nil)
	if err == nil {
		t.Error("New(nil bus) expected error, got nil")
	}
}

func TestPublishStateUpdate(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	client, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Subscribe to state updates
	subscriberClient, err := bus.Client(events.ClientHomeKit)
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}

	sub := eventbus.Subscribe[events.StateUpdateEvent](subscriberClient)
	defer sub.Close()

	tests := []struct {
		name           string
		status         types.Status
		wantTemp       float64
		wantSetpoint   float64
		wantHeating    bool
		wantMode       string
		wantHotWater   bool
	}{
		{
			name: "heating active",
			status: types.Status{
				InHouseTemp:    21.5,
				TempSetpoint:   22.0,
				BoilerIndicator: "CH",
				UserMode:       "manual",
				HotWaterActive: false,
			},
			wantTemp:     21.5,
			wantSetpoint: 22.0,
			wantHeating:  true,
			wantMode:     "heat",
			wantHotWater: false,
		},
		{
			name: "heating inactive",
			status: types.Status{
				InHouseTemp:    22.0,
				TempSetpoint:   22.0,
				BoilerIndicator: "No",
				UserMode:       "manual",
				HotWaterActive: false,
			},
			wantTemp:     22.0,
			wantSetpoint: 22.0,
			wantHeating:  false,
			wantMode:     "heat",
			wantHotWater: false,
		},
		{
			name: "hot water active",
			status: types.Status{
				InHouseTemp:    21.0,
				TempSetpoint:   21.0,
				BoilerIndicator: "HW",
				UserMode:       "manual",
				HotWaterActive: true,
			},
			wantTemp:     21.0,
			wantSetpoint: 21.0,
			wantHeating:  true, // HW indicator means heating
			wantMode:     "heat",
			wantHotWater: true,
		},
		{
			name: "mode off",
			status: types.Status{
				InHouseTemp:    20.0,
				TempSetpoint:   15.0,
				BoilerIndicator: "No",
				UserMode:       testModeOff,
				HotWaterActive: false,
			},
			wantTemp:     20.0,
			wantSetpoint: 15.0,
			wantHeating:  false,
			wantMode:     testModeOff,
			wantHotWater: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.publishStateUpdate(tt.status)

			select {
			case event := <-sub.Events():
				if event.Source != sourceNefit {
					t.Errorf("event.Source = %v, want nefit", event.Source)
				}
				if event.CurrentTemperature != tt.wantTemp {
					t.Errorf("CurrentTemperature = %v, want %v", event.CurrentTemperature, tt.wantTemp)
				}
				if event.TargetTemperature != tt.wantSetpoint {
					t.Errorf("TargetTemperature = %v, want %v", event.TargetTemperature, tt.wantSetpoint)
				}
				if event.HeatingActive != tt.wantHeating {
					t.Errorf("HeatingActive = %v, want %v", event.HeatingActive, tt.wantHeating)
				}
				if event.Mode != tt.wantMode {
					t.Errorf("Mode = %v, want %v", event.Mode, tt.wantMode)
				}
				if event.HotWaterActive != tt.wantHotWater {
					t.Errorf("HotWaterActive = %v, want %v", event.HotWaterActive, tt.wantHotWater)
				}
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for state update event")
			}
		})
	}
}

func TestHandleCommand(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	client, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	tests := []struct {
		name    string
		command events.CommandEvent
	}{
		{
			name: "set temperature",
			command: events.CommandEvent{
				Source:            "homekit",
				CommandType:       events.CommandTypeSetTemperature,
				TargetTemperature: func() *float64 { v := 22.5; return &v }(),
			},
		},
		{
			name: "set mode heat",
			command: events.CommandEvent{
				Source:      "homekit",
				CommandType: events.CommandTypeSetMode,
				Mode:        func() *string { v := "heat"; return &v }(),
			},
		},
		{
			name: "set mode off",
			command: events.CommandEvent{
				Source:      "homekit",
				CommandType: events.CommandTypeSetMode,
				Mode:        func() *string { v := testModeOff; return &v }(),
			},
		},
		{
			name: "set hot water on",
			command: events.CommandEvent{
				Source:          "web",
				CommandType:     events.CommandTypeSetHotWater,
				HotWaterEnabled: func() *bool { v := true; return &v }(),
			},
		},
		{
			name: "set hot water off",
			command: events.CommandEvent{
				Source:          "web",
				CommandType:     events.CommandTypeSetHotWater,
				HotWaterEnabled: func() *bool { v := false; return &v }(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			// Call handleCommand directly - it will fail to connect to the backend
			// but we're testing the command processing logic, not the actual connection
			client.handleCommand(tt.command)

			// If we got here without panicking, the command was processed
			// In a real integration test, we would verify the backend state
		})
	}
}

func TestHandleCommandIgnoresNefitSource(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	client, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Command from nefit source should be ignored to avoid loops
	temp := 22.5
	cmd := events.CommandEvent{
		Source:            sourceNefit,
		CommandType:       events.CommandTypeSetTemperature,
		TargetTemperature: &temp,
	}

	// This should return immediately without processing
	client.handleCommand(cmd)
}

func TestPublishConnectionStatus(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	client, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Subscribe to connection status events
	subscriberClient, err := bus.Client(events.ClientHomeKit)
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}

	sub := eventbus.Subscribe[events.ConnectionStatusEvent](subscriberClient)
	defer sub.Close()

	tests := []struct {
		name       string
		status     events.ConnectionStatus
		errMsg     string
		reconnects int
	}{
		{
			name:       "connected",
			status:     events.ConnectionStatusConnected,
			errMsg:     "",
			reconnects: 0,
		},
		{
			name:       "reconnecting",
			status:     events.ConnectionStatusReconnecting,
			errMsg:     "connection lost",
			reconnects: 2,
		},
		{
			name:       "disconnected",
			status:     events.ConnectionStatusDisconnected,
			errMsg:     "",
			reconnects: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.reconnectNum = tt.reconnects
			client.publishConnectionStatus(tt.status, tt.errMsg)

			select {
			case event := <-sub.Events():
				if event.Component != sourceNefit {
					t.Errorf("event.Component = %v, want nefit", event.Component)
				}
				if event.Status != tt.status {
					t.Errorf("event.Status = %v, want %v", event.Status, tt.status)
				}
				if event.Error != tt.errMsg {
					t.Errorf("event.Error = %v, want %v", event.Error, tt.errMsg)
				}
				if event.Reconnects != tt.reconnects {
					t.Errorf("event.Reconnects = %v, want %v", event.Reconnects, tt.reconnects)
				}
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for connection status event")
			}
		})
	}
}

func TestClose(t *testing.T) {
	logger := zap.NewNop()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := &config.Config{
		NefitSerial:    "TEST123",
		NefitAccessKey: "TESTKEY",
		NefitPassword:  "TESTPASS",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
		WebPort:        0,
	}

	client, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify context was cancelled
	select {
	case <-client.ctx.Done():
		// Success
	default:
		t.Error("context was not cancelled")
	}
}
