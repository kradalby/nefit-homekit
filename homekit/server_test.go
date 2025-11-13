package homekit

import (
	"testing"
	"time"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"go.uber.org/zap"
	"tailscale.com/util/eventbus"
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0, // Random port
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	if server == nil {
		t.Fatal("New() returned nil server")
	}

	if server.accessory == nil {
		t.Fatal("server.accessory is nil")
	}

	if server.server == nil {
		t.Fatal("server.server is nil")
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
	}

	_, err := New(cfg, logger, nil)
	if err == nil {
		t.Error("New(nil bus) expected error, got nil")
	}
}

func TestUpdateAccessory(t *testing.T) {
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	tests := []struct {
		name          string
		event         events.StateUpdateEvent
		wantCurrent   float64
		wantTarget    float64
		wantHeating   int
		wantTargetMode int
	}{
		{
			name: "heating active",
			event: events.StateUpdateEvent{
				Source:             "nefit",
				CurrentTemperature: 21.5,
				TargetTemperature:  22.0,
				HeatingActive:      true,
				Mode:               "heat",
			},
			wantCurrent:   21.5,
			wantTarget:    22.0,
			wantHeating:   1, // Heating
			wantTargetMode: 1, // Heat
		},
		{
			name: "heating inactive",
			event: events.StateUpdateEvent{
				Source:             "nefit",
				CurrentTemperature: 22.0,
				TargetTemperature:  22.0,
				HeatingActive:      false,
				Mode:               "heat",
			},
			wantCurrent:   22.0,
			wantTarget:    22.0,
			wantHeating:   0, // Off
			wantTargetMode: 1, // Heat
		},
		{
			name: "mode off",
			event: events.StateUpdateEvent{
				Source:             "nefit",
				CurrentTemperature: 20.0,
				TargetTemperature:  15.0,
				HeatingActive:      false,
				Mode:               "off",
			},
			wantCurrent:   20.0,
			wantTarget:    15.0,
			wantHeating:   0, // Off
			wantTargetMode: 0, // Off
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server.updateAccessory(tt.event)

			if got := server.accessory.Thermostat.CurrentTemperature.Value(); got != tt.wantCurrent {
				t.Errorf("CurrentTemperature = %v, want %v", got, tt.wantCurrent)
			}

			if got := server.accessory.Thermostat.TargetTemperature.Value(); got != tt.wantTarget {
				t.Errorf("TargetTemperature = %v, want %v", got, tt.wantTarget)
			}

			if got := server.accessory.Thermostat.CurrentHeatingCoolingState.Value(); got != tt.wantHeating {
				t.Errorf("CurrentHeatingCoolingState = %v, want %v", got, tt.wantHeating)
			}

			if got := server.accessory.Thermostat.TargetHeatingCoolingState.Value(); got != tt.wantTargetMode {
				t.Errorf("TargetHeatingCoolingState = %v, want %v", got, tt.wantTargetMode)
			}
		})
	}
}

func TestUpdateAccessoryIgnoresNonNefitSource(t *testing.T) {
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	initialTemp := server.accessory.Thermostat.CurrentTemperature.Value()

	// Event from homekit should be ignored (avoid loop)
	event := events.StateUpdateEvent{
		Source:             "homekit",
		CurrentTemperature: 99.0,
		TargetTemperature:  99.0,
		HeatingActive:      true,
		Mode:               "heat",
	}

	server.updateAccessory(event)

	// Temperature should not have changed
	if got := server.accessory.Thermostat.CurrentTemperature.Value(); got != initialTemp {
		t.Errorf("CurrentTemperature changed to %v, want %v (should ignore non-nefit events)", got, initialTemp)
	}
}

func TestStateUpdatePubSub(t *testing.T) {
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	// Start server (which starts the state update handler)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Get a publisher client
	publisherClient, err := bus.Client(events.ClientNefit)
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}

	// Publish a state update
	event := events.StateUpdateEvent{
		Source:             "nefit",
		CurrentTemperature: 21.5,
		TargetTemperature:  22.0,
		HeatingActive:      true,
		Mode:               "heat",
	}

	bus.PublishStateUpdate(publisherClient, event)

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Verify accessory was updated
	if got := server.accessory.Thermostat.CurrentTemperature.Value(); got != 21.5 {
		t.Errorf("CurrentTemperature = %v, want 21.5", got)
	}
	if got := server.accessory.Thermostat.TargetTemperature.Value(); got != 22.0 {
		t.Errorf("TargetTemperature = %v, want 22.0", got)
	}
}

func TestCommandPublish(t *testing.T) {
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	// Subscribe to command events
	subscriberClient, err := bus.Client(events.ClientNefit)
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}

	sub := eventbus.Subscribe[events.CommandEvent](subscriberClient)
	defer sub.Close()

	// Simulate HomeKit user changing target temperature
	newTemp := 23.0
	server.accessory.Thermostat.TargetTemperature.SetValue(newTemp)

	// Manually call the callback function that was registered
	// (HAP server needs to be running for automatic callbacks)
	tempPtr := newTemp
	event := events.CommandEvent{
		Source:            "homekit",
		CommandType:       events.CommandTypeSetTemperature,
		TargetTemperature: &tempPtr,
	}
	bus.PublishCommand(server.client, event)

	// Wait for event
	select {
	case receivedEvent := <-sub.Events():
		if receivedEvent.Source != "homekit" {
			t.Errorf("event.Source = %v, want homekit", receivedEvent.Source)
		}
		if receivedEvent.CommandType != events.CommandTypeSetTemperature {
			t.Errorf("event.CommandType = %v, want %v", receivedEvent.CommandType, events.CommandTypeSetTemperature)
		}
		if receivedEvent.TargetTemperature == nil || *receivedEvent.TargetTemperature != newTemp {
			t.Errorf("event.TargetTemperature = %v, want %v", receivedEvent.TargetTemperature, newTemp)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for command event")
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
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
		HAPPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = server.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify context was cancelled
	select {
	case <-server.ctx.Done():
		// Success
	default:
		t.Error("context was not cancelled")
	}
}
