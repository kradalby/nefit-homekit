package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

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

	if server.kraweb == nil {
		t.Fatal("server.kraweb is nil")
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

func TestHandleIndex(t *testing.T) {
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

	// Test GET request
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	server.handleIndex(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("handleIndex() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("handleIndex() Content-Type = %s, want text/html", contentType)
	}

	// Test POST request (should fail)
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	w = httptest.NewRecorder()

	server.handleIndex(w, req)

	resp = w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("handleIndex() POST status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleHealth(t *testing.T) {
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

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleSetTemperature(t *testing.T) {
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

	tests := []struct {
		name       string
		temp       string
		wantStatus int
	}{
		{
			name:       "valid temperature",
			temp:       "22.5",
			wantStatus: http.StatusOK,
		},
		{
			name:       "min temperature",
			temp:       "10.0",
			wantStatus: http.StatusOK,
		},
		{
			name:       "max temperature",
			temp:       "30.0",
			wantStatus: http.StatusOK,
		},
		{
			name:       "too low",
			temp:       "5.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "too high",
			temp:       "35.0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid format",
			temp:       "abc",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{}
			form.Add("temperature", tt.temp)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/temperature", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			server.handleSetTemperature(w, req)

			resp := w.Result()
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("handleSetTemperature() status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			// If successful, verify event was published
			if tt.wantStatus == http.StatusOK {
				select {
				case event := <-sub.Events():
					if event.Source != "web" {
						t.Errorf("event.Source = %v, want web", event.Source)
					}
					if event.CommandType != events.CommandTypeSetTemperature {
						t.Errorf("event.CommandType = %v, want %v", event.CommandType, events.CommandTypeSetTemperature)
					}
				case <-time.After(1 * time.Second):
					t.Fatal("timeout waiting for command event")
				}
			}
		})
	}
}

func TestHandleSetMode(t *testing.T) {
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

	tests := []struct {
		name       string
		mode       string
		wantStatus int
	}{
		{
			name:       "heat mode",
			mode:       "heat",
			wantStatus: http.StatusOK,
		},
		{
			name:       "off mode",
			mode:       "off",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid mode",
			mode:       "cool",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{}
			form.Add("mode", tt.mode)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/mode", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			server.handleSetMode(w, req)

			resp := w.Result()
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("handleSetMode() status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			// If successful, verify event was published
			if tt.wantStatus == http.StatusOK {
				select {
				case event := <-sub.Events():
					if event.Source != "web" {
						t.Errorf("event.Source = %v, want web", event.Source)
					}
					if event.CommandType != events.CommandTypeSetMode {
						t.Errorf("event.CommandType = %v, want %v", event.CommandType, events.CommandTypeSetMode)
					}
					if event.Mode == nil || *event.Mode != tt.mode {
						t.Errorf("event.Mode = %v, want %v", event.Mode, tt.mode)
					}
				case <-time.After(1 * time.Second):
					t.Fatal("timeout waiting for command event")
				}
			}
		})
	}
}

func TestUpdateState(t *testing.T) {
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

	event := events.StateUpdateEvent{
		Source:             "nefit",
		CurrentTemperature: 21.5,
		TargetTemperature:  22.0,
		HeatingActive:      true,
		Mode:               "heat",
	}

	server.updateState(event)

	state := server.state.Load().ev

	if state == nil {
		t.Fatal("currentState is nil")
	}

	if state.CurrentTemperature != 21.5 {
		t.Errorf("CurrentTemperature = %v, want 21.5", state.CurrentTemperature)
	}
	if state.TargetTemperature != 22.0 {
		t.Errorf("TargetTemperature = %v, want 22.0", state.TargetTemperature)
	}
	if !state.HeatingActive {
		t.Error("HeatingActive = false, want true")
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

	// Verify state was updated
	state := server.state.Load().ev

	if state == nil {
		t.Fatal("currentState is nil")
	}

	if state.CurrentTemperature != 21.5 {
		t.Errorf("CurrentTemperature = %v, want 21.5", state.CurrentTemperature)
	}
	if state.TargetTemperature != 22.0 {
		t.Errorf("TargetTemperature = %v, want 22.0", state.TargetTemperature)
	}
}

func TestHandleSSE(t *testing.T) {
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

	// Set initial state
	initialEvent := events.StateUpdateEvent{
		Source:             "nefit",
		CurrentTemperature: 20.0,
		TargetTemperature:  21.0,
		HeatingActive:      false,
		Mode:               "heat",
	}
	server.updateState(initialEvent)

	// Create cancellable context for SSE request
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	// Start SSE handler in goroutine
	done := make(chan struct{})
	go func() {
		server.handleSSE(w, req)
		close(done)
	}()

	// Give it time to connect
	time.Sleep(50 * time.Millisecond)

	// Publish new state
	newEvent := events.StateUpdateEvent{
		Source:             "nefit",
		CurrentTemperature: 21.5,
		TargetTemperature:  22.0,
		HeatingActive:      true,
		Mode:               "heat",
	}
	server.updateState(newEvent)

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Cancel the request to stop SSE
	cancel()

	// Wait for handler to finish or timeout
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("SSE handler did not finish in time")
		return
	}

	// Verify SSE headers
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", contentType)
	}

	// Verify we got some data
	body := w.Body.String()
	if !strings.Contains(body, "data:") {
		t.Error("SSE response doesn't contain data events")
	}

	// Parse SSE data
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data: ")
			var event events.StateUpdateEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				t.Errorf("failed to unmarshal SSE data: %v", err)
			}
			// We should receive at least the initial event
			break
		}
	}
}

func TestHandleEventBusDebug(t *testing.T) {
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

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/eventbus", nil)
	w := httptest.NewRecorder()

	server.handleEventBusDebug(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("handleEventBusDebug() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("handleEventBusDebug() Content-Type = %s, want text/html", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "EventBus") {
		t.Error("EventBus debug page doesn't contain 'EventBus'")
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

// sseRecorder is a ResponseWriter that is safe to read while handleSSE writes
// to it. With gate set, the first Write blocks until gate closes, standing in
// for a client that stopped reading.
type sseRecorder struct {
	header  http.Header
	gate    chan struct{}
	blocked chan struct{} // closed once a Write waits on gate
	once    sync.Once

	mu   sync.Mutex
	body strings.Builder
}

func newSSERecorder(gated bool) *sseRecorder {
	r := &sseRecorder{header: http.Header{}}
	if gated {
		r.gate = make(chan struct{})
		r.blocked = make(chan struct{})
	}
	return r
}

func (r *sseRecorder) Header() http.Header { return r.header }
func (r *sseRecorder) WriteHeader(int)     {}
func (r *sseRecorder) Flush()              {}

func (r *sseRecorder) Write(b []byte) (int, error) {
	if r.gate != nil {
		r.once.Do(func() { close(r.blocked) })
		<-r.gate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(b)
}

func (r *sseRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newSSETestServer(t *testing.T) *Server {
	t.Helper()
	bus, err := events.New(testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })

	server, err := New(newTestConfig(t), testLogger(), bus)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

// Shutting down with a browser still connected must end the stream cleanly:
// no panic, and no zero-value frame (0.0°C in the UI) after the real state.
func TestSSECloseWithLiveClient(t *testing.T) {
	server := newSSETestServer(t)
	server.updateState(events.StateUpdateEvent{Source: events.SourceNefit, CurrentTemperature: 21.5, TargetTemperature: 22, Mode: modeHeat})

	rec := newSSERecorder(false)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil)
	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		server.handleSSE(rec, req)
	}()
	waitFor(t, "initial frame", func() bool { return strings.Contains(rec.String(), "21.5") })

	_ = server.Close()

	select {
	case p := <-panicked:
		if p != nil {
			t.Fatalf("handleSSE panicked: %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("handleSSE did not return after Close")
	}
	if n := strings.Count(rec.String(), "data:"); n != 1 {
		t.Errorf("got %d frames, want only the initial one:\n%s", n, rec.String())
	}
}

// A client that falls behind must catch up to the newest state, not stall on
// whatever fit in a buffer while newer updates were dropped.
func TestSSESlowClientGetsLatestState(t *testing.T) {
	server := newSSETestServer(t)
	server.updateState(events.StateUpdateEvent{Source: events.SourceNefit, CurrentTemperature: 10})

	rec := newSSERecorder(true)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleSSE(rec, req)
	}()
	defer func() {
		cancel()
		<-done
	}()

	<-rec.blocked
	for i := 11; i <= 30; i++ {
		server.updateState(events.StateUpdateEvent{Source: events.SourceNefit, CurrentTemperature: float64(i)})
	}
	close(rec.gate)

	waitFor(t, "latest state", func() bool {
		return strings.Contains(rec.String(), `"current_temperature":30,`)
	})
}
