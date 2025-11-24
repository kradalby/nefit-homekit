package nefit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kradalby/nefit-go/types"
	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"tailscale.com/util/eventbus"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestClient(t *testing.T) (*Client, *events.Bus, func()) {
	t.Helper()

	logger := testLogger()
	bus, err := events.New(logger)
	if err != nil {
		t.Fatalf("events.New() error = %v", err)
	}

	busClient, err := bus.Client(events.ClientNefit)
	if err != nil {
		t.Fatalf("bus.Client() error = %v", err)
	}

	cfg := &config.Config{
		BridgeName:            "Test Bridge",
		NefitSerial:           "TEST",
		NefitAccessKey:        "ACCESS",
		NefitPassword:         "PASS",
		XMPPKeepaliveInterval: time.Second,
		XMPPReconnectBackoff:  time.Second,
		XMPPMaxReconnectWait:  5 * time.Second,
	}
	cfg.SetListenerAddrsForTesting("127.0.0.1:12345", "127.0.0.1:8080")

	client := &Client{
		cfg:    cfg,
		logger: logger,
		bus:    bus,
		client: busClient,
		ctx:    context.Background(),
		cancel: func() {},
	}

	cleanup := func() {
		_ = bus.Close()
	}

	return client, bus, cleanup
}

func TestPublishStateUpdate(t *testing.T) {
	client, bus, cleanup := newTestClient(t)
	defer cleanup()

	webClient, err := bus.Client(events.ClientWeb)
	if err != nil {
		t.Fatalf("bus.Client(web) error = %v", err)
	}
	sub := eventbus.Subscribe[events.StateUpdateEvent](webClient)
	defer sub.Close()

	status := types.Status{
		InHouseTemp:     21.5,
		TempSetpoint:    22.0,
		BoilerIndicator: "CH",
		UserMode:        nefitModeManual,
	}

	client.publishStateUpdate(status, false)

	select {
	case evt := <-sub.Events():
		if !evt.HeatingActive {
			t.Fatalf("expected heating to be active")
		}
		if evt.Mode != "heat" {
			t.Fatalf("mode = %s, want heat", evt.Mode)
		}
		if evt.CurrentTemperature != 21.5 || evt.TargetTemperature != 22.0 {
			t.Fatalf("unexpected temperatures: %+v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for state event")
	}
}

func TestHandleNefitEventPublishesStatus(t *testing.T) {
	client, bus, cleanup := newTestClient(t)
	defer cleanup()

	webClient, err := bus.Client(events.ClientWeb)
	if err != nil {
		t.Fatalf("bus.Client(web) error = %v", err)
	}

	sub := eventbus.Subscribe[events.StateUpdateEvent](webClient)
	defer sub.Close()

	payload := map[string]interface{}{
		"in_house_temp":    19.0,
		"temp_setpoint":    17.5,
		"boiler_indicator": "off",
		"user_mode":        nefitModeClock,
	}

	client.handleNefitEvent(types.URIStatus, payload)

	select {
	case evt := <-sub.Events():
		if evt.Mode != "off" {
			t.Fatalf("mode = %s, want off", evt.Mode)
		}
		if evt.HeatingActive {
			t.Fatalf("expected heating inactive")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishConnectionStatus(t *testing.T) {
	client, bus, cleanup := newTestClient(t)
	defer cleanup()

	metricsClient, err := bus.Client(events.ClientMetrics)
	if err != nil {
		t.Fatalf("bus.Client(metrics) error = %v", err)
	}

	sub := eventbus.Subscribe[events.ConnectionStatusEvent](metricsClient)
	defer sub.Close()

	client.publishConnectionStatus(events.ConnectionStatusConnected, "")

	select {
	case evt := <-sub.Events():
		if evt.Status != events.ConnectionStatusConnected {
			t.Fatalf("status = %s, want connected", evt.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection status")
	}
}
