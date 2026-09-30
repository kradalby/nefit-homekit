package homekit

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brutella/hap/characteristic"
	"tailscale.com/util/eventbus"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		BridgeName:     "Test Bridge",
		NefitSerial:    "TEST123",
		NefitAccessKey: "ACCESS",
		NefitPassword:  "PASSWORD",
		HAPPin:         "12345678",
		HAPStoragePath: t.TempDir(),
	}
	cfg.SetListenerAddrsForTesting("127.0.0.1:0", "127.0.0.1:0")
	return cfg
}

func TestNew(t *testing.T) {
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

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
	logger := testLogger()
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
	bus, err := events.New(testLogger())
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

	_, err = New(cfg, nil, bus)
	if err == nil {
		t.Error("New(nil logger) expected error, got nil")
	}
}

func TestNewWithNilBus(t *testing.T) {
	logger := testLogger()
	cfg := newTestConfig(t)

	_, err := New(cfg, logger, nil)
	if err == nil {
		t.Error("New(nil bus) expected error, got nil")
	}
}

func TestUpdateAccessory(t *testing.T) {
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	tests := []struct {
		name           string
		event          events.StateUpdateEvent
		wantCurrent    float64
		wantTarget     float64
		wantHeating    int
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
			wantCurrent:    21.5,
			wantTarget:     22.0,
			wantHeating:    1, // Heating
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
			wantCurrent:    22.0,
			wantTarget:     22.0,
			wantHeating:    0, // Off
			wantTargetMode: 1, // Heat
		},
		{
			name: "mode off (schedule)",
			event: events.StateUpdateEvent{
				Source:             "nefit",
				CurrentTemperature: 20.0,
				TargetTemperature:  15.0,
				HeatingActive:      false,
				Mode:               "off",
			},
			wantCurrent:    20.0,
			wantTarget:     15.0,
			wantHeating:    0, // Off
			wantTargetMode: 3, // Auto: "off" is the clock schedule, which still heats
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
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

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
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

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
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

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
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	cfg := newTestConfig(t)

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = server.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify context was canceled
	select {
	case <-server.ctx.Done():
		// Success
	default:
		t.Error("context was not canceled")
	}
}

func newCallbackTestServer(t *testing.T) (*Server, *eventbus.Subscriber[events.CommandEvent]) {
	t.Helper()
	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })

	server, err := New(newTestConfig(t), logger, bus)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	server.setupAccessoryCallbacks()

	nefitClient, err := bus.Client(events.ClientNefit)
	if err != nil {
		t.Fatal(err)
	}
	sub := eventbus.Subscribe[events.CommandEvent](nefitClient)
	t.Cleanup(sub.Close)
	return server, sub
}

// remoteWrite stands in for a paired controller writing a characteristic;
// hap only treats a write as remote when it carries a request.
func remoteWrite(t *testing.T, c interface {
	SetValueRequest(any, *http.Request) (any, int)
}, v any,
) int {
	t.Helper()
	_, code := c.SetValueRequest(v, httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/characteristics", nil))
	return code
}

// Nefit has no off. Accepting Off from HomeKit and ignoring it left the Home
// app showing Off while the boiler kept heating, and bus dedup meant no later
// update corrected it.
func TestTargetStateRejectsOff(t *testing.T) {
	server, sub := newCallbackTestServer(t)
	target := server.accessory.Thermostat.TargetHeatingCoolingState
	if err := target.SetValue(characteristic.TargetHeatingCoolingStateHeat); err != nil {
		t.Fatal(err)
	}

	if code := remoteWrite(t, target, characteristic.TargetHeatingCoolingStateOff); code == 0 {
		t.Error("remote write of Off accepted")
	}
	if got := target.Value(); got != characteristic.TargetHeatingCoolingStateHeat {
		t.Errorf("TargetHeatingCoolingState = %d after rejected Off, want Heat", got)
	}

	select {
	case cmd := <-sub.Events():
		t.Fatalf("rejected Off published %+v", cmd)
	case <-time.After(50 * time.Millisecond):
	}
}

// Auto hands control back to the thermostat's schedule (Nefit clock mode).
func TestTargetStateAutoSelectsSchedule(t *testing.T) {
	server, sub := newCallbackTestServer(t)
	target := server.accessory.Thermostat.TargetHeatingCoolingState
	if err := target.SetValue(characteristic.TargetHeatingCoolingStateHeat); err != nil {
		t.Fatal(err)
	}

	if code := remoteWrite(t, target, characteristic.TargetHeatingCoolingStateAuto); code != 0 {
		t.Fatalf("remote write of Auto rejected with %d", code)
	}

	select {
	case cmd := <-sub.Events():
		if cmd.CommandType != events.CommandTypeSetMode || cmd.Mode == nil || *cmd.Mode != modeOff {
			t.Fatalf("Auto published %+v, want set_mode %q", cmd, modeOff)
		}
	case <-time.After(time.Second):
		t.Fatal("Auto published no command")
	}
}
