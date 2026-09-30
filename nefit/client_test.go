package nefit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	nefitclient "github.com/kradalby/nefit-go/client"

	"github.com/kradalby/nefit-go/types"
	"tailscale.com/util/eventbus"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
)

// fakeBackend records what the client asks of the thermostat.
type fakeBackend struct {
	mu          sync.Mutex
	calls       []string
	status      types.Status
	connectErr  error
	statusDelay time.Duration
	inflight    int
	maxInflight int
}

func (f *fakeBackend) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeBackend) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeBackend) Connect(context.Context) error {
	f.record("Connect")
	return f.connectErr
}

func (f *fakeBackend) Close() error                       { return nil }
func (f *fakeBackend) Subscribe(nefitclient.EventHandler) {}

func (f *fakeBackend) Status(_ context.Context, outdoor bool) (*types.Status, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("Status(%v)", outdoor))
	f.inflight++
	f.maxInflight = max(f.maxInflight, f.inflight)
	st := f.status
	f.mu.Unlock()

	time.Sleep(f.statusDelay)

	f.mu.Lock()
	f.inflight--
	f.mu.Unlock()
	return &st, nil
}

func (f *fakeBackend) SetTemperature(_ context.Context, temp float64) error {
	f.record(fmt.Sprintf("SetTemperature(%v)", temp))
	return nil
}

func (f *fakeBackend) SetUserMode(_ context.Context, mode string) error {
	f.record(fmt.Sprintf("SetUserMode(%s)", mode))
	return nil
}

func (f *fakeBackend) SetHotWaterSupply(_ context.Context, enabled bool) error {
	f.record(fmt.Sprintf("SetHotWaterSupply(%v)", enabled))
	return nil
}

// waitFor polls cond until it holds or a deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

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

	client.publishConnectionStatus(events.ConnectionStatusConnected, "", 0)

	select {
	case evt := <-sub.Events():
		if evt.Status != events.ConnectionStatusConnected {
			t.Fatalf("status = %s, want connected", evt.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection status")
	}
}

// HomeKit and the web UI can issue several commands inside one debounce
// window, e.g. a mode switch and a setpoint; each kind must reach the
// thermostat, with the latest value of each kind winning.
func TestCommandsInDebounceWindowAllApply(t *testing.T) {
	c, bus, cleanup := newTestClient(t)
	defer cleanup()

	fake := &fakeBackend{}
	c.nefitClient = fake
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.ctx = ctx

	go c.handleCommands(eventbus.Subscribe[events.CommandEvent](c.client))

	hk, err := bus.Client(events.ClientHomeKit)
	if err != nil {
		t.Fatal(err)
	}
	mode, draft, temp, hotWater := modeHeat, 19.0, 21.0, true
	bus.PublishCommand(hk, events.CommandEvent{Source: events.SourceHomeKit, CommandType: events.CommandTypeSetTemperature, TargetTemperature: &draft})
	bus.PublishCommand(hk, events.CommandEvent{Source: events.SourceHomeKit, CommandType: events.CommandTypeSetHotWater, HotWaterEnabled: &hotWater})
	bus.PublishCommand(hk, events.CommandEvent{Source: events.SourceHomeKit, CommandType: events.CommandTypeSetTemperature, TargetTemperature: &temp})
	bus.PublishCommand(hk, events.CommandEvent{Source: events.SourceHomeKit, CommandType: events.CommandTypeSetMode, Mode: &mode})

	waitFor(t, "sync after commands", func() bool {
		return slices.Contains(fake.Calls(), "Status(false)") || slices.Contains(fake.Calls(), "Status(true)")
	})

	// Mode goes first: the setpoint and hot water endpoints depend on it.
	var got []string
	for _, call := range fake.Calls() {
		if !strings.HasPrefix(call, "Status") {
			got = append(got, call)
		}
	}
	want := []string{"SetUserMode(manual)", "SetTemperature(21)", "SetHotWaterSupply(true)"}
	if !slices.Equal(got, want) {
		t.Fatalf("commands applied = %v, want %v", got, want)
	}
}

// Close publishes a connection status while connectWithRetry is still counting
// failed attempts; run under -race.
func TestCloseDuringReconnect(t *testing.T) {
	c, _, cleanup := newTestClient(t)
	defer cleanup()

	fake := &fakeBackend{connectErr: errors.New("unreachable")}
	c.nefitClient = fake
	c.cfg.XMPPReconnectBackoff = time.Millisecond
	c.cfg.XMPPMaxReconnectWait = time.Millisecond
	c.ctx, c.cancel = context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.connectWithRetry()
	}()

	waitFor(t, "a few failed attempts", func() bool {
		return len(fake.Calls()) >= 3
	})
	_ = c.Close()
	<-done
}
