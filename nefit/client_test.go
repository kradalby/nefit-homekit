package nefit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kradalby/nefit-go/types"
	"tailscale.com/util/eventbus"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
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

	client.refreshStatus = func(bool) error { return nil }

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

	// "central heating" is what nefit-go's client.Status actually yields for a
	// firing boiler; it translates the raw "CH" wire value. Asserting on the
	// raw form here is what hid a permanently-false HeatingActive.
	status := types.Status{
		InHouseTemp:     21.5,
		TempSetpoint:    22.0,
		BoilerIndicator: "central heating",
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

func TestHeatingActiveAcceptsBothBoilerSpellings(t *testing.T) {
	client, bus, cleanup := newTestClient(t)
	defer cleanup()

	webClient, err := bus.Client(events.ClientWeb)
	if err != nil {
		t.Fatalf("bus.Client(web) error = %v", err)
	}
	sub := eventbus.Subscribe[events.StateUpdateEvent](webClient)
	defer sub.Close()

	for _, tc := range []struct {
		indicator string
		want      bool
	}{
		{"central heating", true},
		{"hot water", true},
		{"CH", true},
		{"HW", true},
		{"off", false},
		{"No", false},
	} {
		client.publishStateUpdate(types.Status{
			BoilerIndicator: tc.indicator,
			UserMode:        nefitModeManual,
			InHouseTemp:     20,
		}, true)

		select {
		case evt := <-sub.Events():
			if evt.HeatingActive != tc.want {
				t.Errorf("BoilerIndicator %q: HeatingActive = %v, want %v",
					tc.indicator, evt.HeatingActive, tc.want)
			}
		case <-time.After(time.Second):
			t.Fatalf("BoilerIndicator %q: timed out waiting for event", tc.indicator)
		}
	}
}

// A status push carries the device's abbreviated wire keys and may be partial,
// so the handler must re-read the full status through nefit-go rather than
// decode the payload itself. Before nefit-go started populating URI, this
// branch was unreachable and the mis-keyed decode it used to do went unnoticed.
func TestHandleNefitEventRefreshesStatus(t *testing.T) {
	client, _, cleanup := newTestClient(t)
	defer cleanup()

	calls := 0
	client.refreshStatus = func(force bool) error {
		if force {
			t.Errorf("refreshStatus force = true, want false")
		}
		calls++
		return nil
	}

	// The raw device payload: abbreviated keys, nested under "value".
	client.handleNefitEvent(types.URIStatus, map[string]any{
		"id":    types.URIStatus,
		"value": map[string]any{"IHT": 19.0, "TSP": 17.5, "BAI": "No", "UMD": nefitModeClock},
	})
	if calls != 1 {
		t.Fatalf("status push triggered %d refreshes, want 1", calls)
	}

	client.handleNefitEvent(types.URIOutdoorTemp, map[string]any{"id": types.URIOutdoorTemp})
	if calls != 1 {
		t.Fatalf("non-status push triggered a refresh, total = %d, want 1", calls)
	}
}

func TestHandleNefitEventLogsRefreshFailure(t *testing.T) {
	client, _, cleanup := newTestClient(t)
	defer cleanup()

	client.refreshStatus = func(bool) error { return errors.New("backend down") }

	// Must not panic or propagate; the poll loop is the backstop.
	client.handleNefitEvent(types.URIStatus, nil)
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
