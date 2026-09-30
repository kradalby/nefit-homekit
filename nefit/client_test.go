package nefit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
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

	// session mirrors nefit-go's: closed when the session ends and replaced
	// by the next successful Connect; nil before the first.
	session chan struct{}
	// unanswered is how many upcoming Status calls go unanswered, which in
	// nefit-go retires the session.
	unanswered int
	// flapping ends every session as soon as it starts.
	flapping bool
	// dialDelay stalls Connect, which like nefit-go's gives up when its
	// context ends.
	dialDelay time.Duration
	closed    bool
	// modeErr fails SetUserMode.
	modeErr error
	// dials counts logins, including those Status starts on its own.
	dials int
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

// Connect is a no-op while a session is alive, like nefit-go's.
func (f *fakeBackend) Connect(ctx context.Context) error {
	f.record("Connect")
	select {
	case <-time.After(f.dialDelay):
	case <-ctx.Done():
		return ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionLocked()
}

// sessionLocked logs in unless a session is live, like nefit-go does for
// Connect and for every request.
func (f *fakeBackend) sessionLocked() error {
	if f.closed {
		return errors.New("client closed")
	}
	if f.session != nil && !isClosed(f.session) {
		return nil
	}
	f.dials++
	if f.connectErr != nil {
		return f.connectErr
	}
	f.session = make(chan struct{})
	if f.flapping {
		close(f.session)
	}
	return nil
}

func (f *fakeBackend) Done() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.session == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return f.session
}

// drop ends the session the way a read error does.
func (f *fakeBackend) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endSessionLocked()
}

func (f *fakeBackend) endSessionLocked() {
	if f.session != nil && !isClosed(f.session) {
		close(f.session)
	}
}

func (f *fakeBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.endSessionLocked()
	return nil
}

func (f *fakeBackend) Subscribe(nefitclient.EventHandler) {}

func (f *fakeBackend) Status(_ context.Context, outdoor bool) (*types.Status, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("Status(%v)", outdoor))
	if err := f.sessionLocked(); err != nil {
		f.mu.Unlock()
		return nil, err
	}
	if f.unanswered > 0 {
		f.unanswered--
		f.endSessionLocked()
		f.mu.Unlock()
		return nil, context.DeadlineExceeded
	}
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
	return f.modeErr
}

func (f *fakeBackend) SetHotWaterSupply(_ context.Context, enabled bool) error {
	f.record(fmt.Sprintf("SetHotWaterSupply(%v)", enabled))
	return nil
}

func (f *fakeBackend) count(call string) int {
	return len(slices.DeleteFunc(f.Calls(), func(s string) bool { return s != call }))
}

func (f *fakeBackend) connects() int { return f.count("Connect") }
func (f *fakeBackend) polls() int    { return f.count("Status(false)") }

func (f *fakeBackend) dialCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
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
		cfg:     cfg,
		logger:  logger,
		bus:     bus,
		client:  busClient,
		ctx:     context.Background(),
		cancel:  func() {},
		refresh: make(chan struct{}, 1),
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
// decode the payload itself.
func TestHandleNefitEventRefreshesStatus(t *testing.T) {
	client, _, cleanup := newTestClient(t)
	defer cleanup()

	pending := func() bool {
		select {
		case <-client.refresh:
			return true
		default:
			return false
		}
	}

	// The raw device payload: abbreviated keys, nested under "value".
	client.handleNefitEvent(types.URIStatus, map[string]any{
		"id":    types.URIStatus,
		"value": map[string]any{"IHT": 19.0, "TSP": 17.5, "BAI": "No", "UMD": nefitModeClock},
	})
	if !pending() {
		t.Fatal("status push did not request a refresh")
	}
	if client.forceRefresh.Load() {
		t.Error("status push forced a refresh")
	}

	client.handleNefitEvent(types.URIOutdoorTemp, map[string]any{"id": types.URIOutdoorTemp})
	if pending() {
		t.Fatal("non-status push requested a refresh")
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

	select {
	case <-c.refresh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for refresh after commands")
	}

	got := fake.Calls()
	want := []string{"SetUserMode(manual)", "SetTemperature(21)", "SetHotWaterSupply(true)"}
	if !slices.Equal(got, want) {
		t.Fatalf("commands applied = %v, want %v", got, want)
	}
}

// Commands fold in arrival order. nefit-go's SetTemperature turns on a manual
// override, so a setpoint sent along with Auto would hold off the clock
// program Auto asked for; a setpoint after Auto is a deliberate override.
func TestApplyAutoAndSetpoint(t *testing.T) {
	auto, heat, temp := modeOff, modeHeat, 21.0
	setMode := func(m *string) events.CommandEvent {
		return events.CommandEvent{CommandType: events.CommandTypeSetMode, Mode: m}
	}
	setTemp := events.CommandEvent{CommandType: events.CommandTypeSetTemperature, TargetTemperature: &temp}

	for _, tc := range []struct {
		name string
		cmds []events.CommandEvent
		want []string
	}{
		{"setpoint then Auto", []events.CommandEvent{setTemp, setMode(&auto)}, []string{"SetUserMode(clock)"}},
		{"Auto then setpoint", []events.CommandEvent{setMode(&auto), setTemp}, []string{"SetUserMode(clock)", "SetTemperature(21)"}},
		{"setpoint then Heat", []events.CommandEvent{setTemp, setMode(&heat)}, []string{"SetUserMode(manual)", "SetTemperature(21)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, cleanup := newTestClient(t)
			defer cleanup()
			fake := &fakeBackend{}
			c.nefitClient = fake

			var d desired
			for _, cmd := range tc.cmds {
				var err error
				if d, err = d.with(cmd); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.apply(d); err != nil {
				t.Fatal(err)
			}
			if got := fake.Calls(); !slices.Equal(got, tc.want) {
				t.Fatalf("calls = %v, want %v", got, tc.want)
			}
		})
	}
}

// nefit-go writes hot water to the endpoint of the mode in effect, which after
// a failed mode change is not the mode the request was meant for.
func TestApplySkipsHotWaterWhenModeFails(t *testing.T) {
	c, _, cleanup := newTestClient(t)
	defer cleanup()
	fake := &fakeBackend{modeErr: errors.New("timeout")}
	c.nefitClient = fake

	mode, hotWater := modeHeat, true
	if err := c.apply(desired{mode: &mode, hotWater: &hotWater}); err == nil {
		t.Fatal("apply succeeded with the mode change failing")
	}
	if got, want := fake.Calls(), []string{"SetUserMode(manual)"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

// nefit-go runs each push handler on its own goroutine, and polls and
// post-command syncs fetch too. Overlapping fetches can publish out of order,
// letting an older status overwrite a newer one, so they must run one at a
// time.
func TestStatusFetchesDoNotOverlap(t *testing.T) {
	c, _, cleanup := newTestClient(t)
	defer cleanup()

	fake := &fakeBackend{statusDelay: 20 * time.Millisecond}
	c.nefitClient = fake
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.ctx = ctx
	if err := fake.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.refreshLoop()
	}()
	// The loop publishes on the bus, so it must stop before cleanup closes it.
	defer func() {
		cancel()
		<-done
	}()

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { c.handleNefitEvent(types.URIStatus, nil) })
	}
	wg.Go(func() { c.requestRefresh(true) })
	wg.Wait()

	waitFor(t, "a status fetch", func() bool { return slices.Contains(fake.Calls(), "Status(false)") })
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.maxInflight > 1 {
		t.Fatalf("%d status fetches ran concurrently, want 1", fake.maxInflight)
	}
}

// StateUpdateEvent has no outdoor temperature, and fetching it costs a second
// round trip through nefit-go's request queue on every refresh.
func TestFetchSkipsOutdoorTemperature(t *testing.T) {
	c, _, cleanup := newTestClient(t)
	defer cleanup()

	fake := &fakeBackend{}
	c.nefitClient = fake

	if err := c.fetchAndPublishStatus(false); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.Calls(), []string{"Status(false)"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

// Shutdown closes the bus right after the client; a fetch still in flight must
// not publish onto the closed bus.
func TestCloseWaitsForInFlightFetch(t *testing.T) {
	c, bus, cleanup := newTestClient(t)
	defer cleanup()

	fake := &fakeBackend{statusDelay: 50 * time.Millisecond}
	c.nefitClient = fake
	c.ctx, c.cancel = context.WithCancel(context.Background())

	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.requestRefresh(false)
	waitFor(t, "fetch in flight", func() bool {
		return slices.Contains(fake.Calls(), "Status(false)")
	})

	_ = c.Close()
	_ = bus.Close()
	time.Sleep(2 * fake.statusDelay)
}

// startWithFake starts c against fake and returns the connection statuses
// published so far. Call it inside a synctest bubble: the deferred Close must
// leave no goroutine behind.
func startWithFake(t *testing.T, fake *fakeBackend) (c *Client, statuses func() []events.ConnectionStatus) {
	t.Helper()

	c, bus, cleanup := newTestClient(t)
	c.nefitClient = fake
	c.ctx, c.cancel = context.WithCancel(context.Background())

	metrics, err := bus.Client(events.ClientMetrics)
	if err != nil {
		t.Fatal(err)
	}
	sub := eventbus.Subscribe[events.ConnectionStatusEvent](metrics)

	var (
		mu  sync.Mutex
		got []events.ConnectionStatus
	)
	go func() {
		for {
			select {
			case ev := <-sub.Events():
				mu.Lock()
				got = append(got, ev.Status)
				mu.Unlock()
			case <-sub.Done():
				return
			}
		}
	}()

	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	// Cleanups run last-in first-out: client, subscription, bus.
	t.Cleanup(cleanup)
	t.Cleanup(sub.Close)
	t.Cleanup(func() { _ = c.Close() })

	return c, func() []events.ConnectionStatus {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func last[T any](s []T) (T, bool) {
	if len(s) == 0 {
		var zero T
		return zero, false
	}
	return s[len(s)-1], true
}

// Pushes stop with the session and nefit-go only reconnects for a request, so
// the bridge must, then refetch what it missed.
func TestReconnectsAfterSessionDrop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeBackend{}
		_, statuses := startWithFake(t, fake)

		time.Sleep(time.Minute)
		synctest.Wait()
		if s, _ := last(statuses()); s != events.ConnectionStatusConnected {
			t.Fatalf("status = %q before drop, want connected", s)
		}

		seen, before := len(statuses()), len(fake.Calls())
		fake.drop()
		synctest.Wait()

		if got, want := fake.Calls()[before:], []string{"Connect", "Status(false)"}; !slices.Equal(got, want) {
			t.Fatalf("calls after drop = %v, want %v", got, want)
		}
		got := statuses()[seen:]
		if !slices.Contains(got, events.ConnectionStatusDisconnected) {
			t.Errorf("statuses after drop = %v, want a disconnected", got)
		}
		if s, _ := last(got); s != events.ConnectionStatusConnected {
			t.Errorf("statuses after drop = %v, want to end connected", got)
		}
	})
}

// nefit-go retires a session whose request goes unanswered, since the protocol
// cannot tell a late reply from the next answer.
func TestReconnectsAfterRequestTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeBackend{}
		c, _ := startWithFake(t, fake)

		time.Sleep(time.Minute)
		synctest.Wait()

		before := len(fake.Calls())
		fake.mu.Lock()
		fake.unanswered = 1
		fake.mu.Unlock()
		c.requestRefresh(false)
		synctest.Wait()

		want := []string{"Status(false)", "Connect", "Status(false)"}
		if got := fake.Calls()[before:]; !slices.Equal(got, want) {
			t.Fatalf("calls after timeout = %v, want %v", got, want)
		}
	})
}

// A backend that accepts a session and ends it at once must not be redialled
// in a tight loop.
func TestShortSessionsBackOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeBackend{flapping: true}
		startWithFake(t, fake)
		synctest.Wait()

		if n := fake.connects(); n != 1 {
			t.Fatalf("%d connects before any backoff elapsed, want 1", n)
		}

		// Jitter never shortens a wait below half the backoff.
		time.Sleep(time.Minute)
		synctest.Wait()
		if n, most := fake.connects(), 1+int(time.Minute/(time.Second/2)); n > most {
			t.Fatalf("%d connects in a minute, want at most %d", n, most)
		}
		// The refresh each session queues must not log in once it ended.
		if dials, connects := fake.dialCount(), fake.connects(); dials != connects {
			t.Fatalf("%d logins for %d connects", dials, connects)
		}
	})
}

// nefit-go's Status logs in when no session is up, so polls during the
// reconnect backoff would redial at the poll rate. A live session still gets
// polled, which is how a silently dead one is found.
func TestPollsWaitOutBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeBackend{connectErr: errors.New("unreachable")}
		startWithFake(t, fake)

		time.Sleep(time.Minute)
		synctest.Wait()
		if dials, connects := fake.dialCount(), fake.connects(); dials != connects {
			t.Fatalf("%d logins for %d connects while unreachable", dials, connects)
		}

		fake.mu.Lock()
		fake.connectErr = nil
		fake.mu.Unlock()
		before := fake.polls()
		time.Sleep(time.Minute)
		synctest.Wait()
		if n, least := fake.polls()-before, int(time.Minute/time.Second)/2; n < least {
			t.Fatalf("%d polls in a minute once connected, want at least %d", n, least)
		}
	})
}

func TestCloseDuringBackoffReturnsPromptly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeBackend{connectErr: errors.New("unreachable")}
		c, _ := startWithFake(t, fake)
		time.Sleep(time.Minute)
		synctest.Wait()

		start := time.Now()
		_ = c.Close()
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("Close waited %v for the backoff", waited)
		}
	})
}

// A login can take up to nefit-go's ConnectTimeout; Close must not wait it out,
// and the last word must be disconnected.
func TestCloseDuringDialEndsDisconnected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeBackend{dialDelay: time.Minute}
		c, statuses := startWithFake(t, fake)
		synctest.Wait()

		start := time.Now()
		_ = c.Close()
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("Close waited %v for the dial", waited)
		}
		synctest.Wait()

		if got := statuses(); !slices.Equal(got, []events.ConnectionStatus{
			events.ConnectionStatusConnecting,
			events.ConnectionStatusDisconnected,
		}) {
			t.Fatalf("statuses = %v, want connecting then disconnected", got)
		}
	})
}
