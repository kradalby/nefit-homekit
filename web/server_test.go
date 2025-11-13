package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		HAPPort:        0,
		WebPort:        0, // Random port
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

func TestHandleIndex(t *testing.T) {
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
		WebPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	// Test GET request
	req := httptest.NewRequest(http.MethodGet, "/", nil)
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
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	w = httptest.NewRecorder()

	server.handleIndex(w, req)

	resp = w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("handleIndex() POST status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleHealth(t *testing.T) {
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
		WebPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleSetTemperature(t *testing.T) {
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
		WebPort:        0,
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

			req := httptest.NewRequest(http.MethodPost, "/api/temperature", strings.NewReader(form.Encode()))
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
		WebPort:        0,
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

			req := httptest.NewRequest(http.MethodPost, "/api/mode", strings.NewReader(form.Encode()))
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
		WebPort:        0,
	}

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

	server.mu.RLock()
	state := server.currentState
	server.mu.RUnlock()

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
		WebPort:        0,
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

	// Verify state was updated
	server.mu.RLock()
	state := server.currentState
	server.mu.RUnlock()

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
		WebPort:        0,
	}

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

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
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
		WebPort:        0,
	}

	server, err := New(cfg, logger, bus)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	req := httptest.NewRequest(http.MethodGet, "/debug/eventbus", nil)
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
		WebPort:        0,
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

	// Verify SSE clients were cleaned up
	server.mu.RLock()
	clientCount := len(server.sseClients)
	server.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("After Close(), SSE client count = %d, want 0", clientCount)
	}
}
