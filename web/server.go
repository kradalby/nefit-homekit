// Package web provides a web interface for monitoring and controlling the thermostat.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"tailscale.com/util/eventbus"
)

const (
	modeOff  = "off"
	modeHeat = "heat"
)

// Server manages the web interface.
type Server struct {
	cfg    *config.Config
	logger *zap.Logger
	bus    *events.Bus
	client *eventbus.Client
	server *http.Server
	mux    *http.ServeMux
	ctx    context.Context
	cancel context.CancelFunc

	// Current state for SSE clients
	mu           sync.RWMutex
	currentState *events.StateUpdateEvent
	sseClients   map[chan events.StateUpdateEvent]struct{}
}

// New creates a new web server.
func New(cfg *config.Config, logger *zap.Logger, bus *events.Bus) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("eventbus is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Get eventbus client
	client, err := bus.Client(events.ClientWeb)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get eventbus client: %w", err)
	}

	mux := http.NewServeMux()

	s := &Server{
		cfg:        cfg,
		logger:     logger,
		bus:        bus,
		client:     client,
		mux:        mux,
		ctx:        ctx,
		cancel:     cancel,
		sseClients: make(map[chan events.StateUpdateEvent]struct{}),
	}

	// Create HTTP server
	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.WebPort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Setup routes
	s.setupRoutes()

	logger.Info("web server created",
		zap.Int("port", cfg.WebPort),
	)

	return s, nil
}

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes() {
	// Main thermostat UI
	s.mux.HandleFunc("/", s.handleIndex)

	// SSE for real-time updates
	s.mux.HandleFunc("/events", s.handleSSE)

	// HTMX API endpoints
	s.mux.HandleFunc("/api/temperature", s.handleSetTemperature)
	s.mux.HandleFunc("/api/mode", s.handleSetMode)

	// EventBus debugger
	s.mux.HandleFunc("/debug/eventbus", s.handleEventBusDebug)

	// Prometheus metrics
	s.mux.Handle("/metrics", promhttp.Handler())

	// Health check
	s.mux.HandleFunc("/health", s.handleHealth)
}

// Start starts the web server and begins handling events.
func (s *Server) Start() error {
	s.logger.Info("starting web server")

	// Subscribe to state update events
	go s.handleStateUpdates()

	// Start HTTP server in background
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("web server error", zap.Error(err))
		}
	}()

	// Publish connection status
	s.publishConnectionStatus(events.ConnectionStatusConnected, "")

	s.logger.Info("web server started successfully")
	return nil
}

// handleStateUpdates subscribes to state update events and broadcasts to SSE clients.
func (s *Server) handleStateUpdates() {
	sub := eventbus.Subscribe[events.StateUpdateEvent](s.client)
	defer sub.Close()

	s.logger.Info("subscribed to state update events")

	for {
		select {
		case event := <-sub.Events():
			s.updateState(event)
		case <-s.ctx.Done():
			s.logger.Info("stopping state update handler")
			return
		}
	}
}

// updateState updates current state and broadcasts to all SSE clients.
func (s *Server) updateState(event events.StateUpdateEvent) {
	s.mu.Lock()
	s.currentState = &event

	// Broadcast to all SSE clients
	for client := range s.sseClients {
		select {
		case client <- event:
		default:
			// Client is slow or disconnected, skip
		}
	}
	s.mu.Unlock()

	s.logger.Debug("state updated",
		zap.Float64("current_temp", event.CurrentTemperature),
		zap.Float64("target_temp", event.TargetTemperature),
		zap.Bool("heating", event.HeatingActive),
	)
}

// handleIndex serves the main thermostat UI.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	state := s.currentState
	s.mu.RUnlock()

	html := s.renderThermostatUI(state)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// handleSSE handles Server-Sent Events for real-time updates.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Create client channel
	clientChan := make(chan events.StateUpdateEvent, 10)

	// Register client
	s.mu.Lock()
	s.sseClients[clientChan] = struct{}{}
	s.mu.Unlock()

	// Send current state immediately
	s.mu.RLock()
	if s.currentState != nil {
		clientChan <- *s.currentState
	}
	s.mu.RUnlock()

	// Cleanup on disconnect
	defer func() {
		s.mu.Lock()
		delete(s.sseClients, clientChan)
		s.mu.Unlock()
		close(clientChan)
	}()

	// Stream events
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case event := <-clientChan:
			data, err := json.Marshal(event)
			if err != nil {
				s.logger.Error("failed to marshal event", zap.Error(err))
				continue
			}

			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// handleSetTemperature handles temperature change requests via HTMX.
func (s *Server) handleSetTemperature(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	tempStr := r.FormValue("temperature")
	temp, err := strconv.ParseFloat(tempStr, 64)
	if err != nil {
		http.Error(w, "Invalid temperature value", http.StatusBadRequest)
		return
	}

	// Validate temperature range
	if temp < 10.0 || temp > 30.0 {
		http.Error(w, "Temperature out of range (10-30°C)", http.StatusBadRequest)
		return
	}

	// Publish command event
	event := events.CommandEvent{
		Source:            "web",
		CommandType:       events.CommandTypeSetTemperature,
		TargetTemperature: &temp,
	}
	s.bus.PublishCommand(s.client, event)

	s.logger.Info("temperature changed via web",
		zap.Float64("temperature", temp),
	)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleSetMode handles mode change requests via HTMX.
func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	mode := r.FormValue("mode")
	if mode != modeOff && mode != modeHeat {
		http.Error(w, "Invalid mode (must be 'off' or 'heat')", http.StatusBadRequest)
		return
	}

	// Publish command event
	event := events.CommandEvent{
		Source:      "web",
		CommandType: events.CommandTypeSetMode,
		Mode:        &mode,
	}
	s.bus.PublishCommand(s.client, event)

	s.logger.Info("mode changed via web",
		zap.String("mode", mode),
	)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleEventBusDebug shows EventBus statistics and recent events.
func (s *Server) handleEventBusDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	html := s.renderEventBusDebug()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// handleHealth returns server health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// publishConnectionStatus publishes a connection status event.
func (s *Server) publishConnectionStatus(status events.ConnectionStatus, errMsg string) {
	event := events.ConnectionStatusEvent{
		Component: "web",
		Status:    status,
		Error:     errMsg,
	}
	s.bus.PublishConnectionStatus(s.client, event)
}

// Close gracefully shuts down the web server.
func (s *Server) Close() error {
	s.logger.Info("shutting down web server")

	s.publishConnectionStatus(events.ConnectionStatusDisconnected, "")

	// Close all SSE clients
	s.mu.Lock()
	for client := range s.sseClients {
		close(client)
	}
	s.sseClients = make(map[chan events.StateUpdateEvent]struct{})
	s.mu.Unlock()

	// Cancel context to stop background goroutines
	s.cancel()

	// Gracefully shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		s.logger.Warn("server shutdown error", zap.Error(err))
	}

	s.logger.Info("web server shut down complete")
	return nil
}

// renderThermostatUI renders the main thermostat UI using elem-go.
func (s *Server) renderThermostatUI(state *events.StateUpdateEvent) string {
	currentTemp := "N/A"
	targetTemp := "20.0"
	heating := false
	mode := modeHeat

	if state != nil {
		currentTemp = fmt.Sprintf("%.1f°C", state.CurrentTemperature)
		targetTemp = fmt.Sprintf("%.1f", state.TargetTemperature)
		heating = state.HeatingActive
		mode = state.Mode
	}

	heatingStatus := "Off"
	heatingClass := "status-off"
	if heating {
		heatingStatus = "Heating"
		heatingClass = "status-heating"
	}

	return elem.Html(nil,
		elem.Head(nil,
			elem.Title(nil, elem.Text("Nefit Easy Thermostat")),
			elem.Meta(attrs.Props{attrs.Charset: "utf-8"}),
			elem.Meta(attrs.Props{attrs.Name: "viewport", attrs.Content: "width=device-width, initial-scale=1"}),
			elem.Script(attrs.Props{attrs.Src: "https://unpkg.com/htmx.org@1.9.10"}),
			elem.Style(nil, elem.Text(s.getCSS())),
		),
		elem.Body(nil,
			elem.Div(attrs.Props{attrs.Class: "container"},
				elem.H1(nil, elem.Text("Nefit Easy Thermostat")),

				elem.Div(attrs.Props{attrs.Class: "status-card"},
					elem.Div(attrs.Props{attrs.Class: "temp-display"},
						elem.Div(attrs.Props{attrs.Class: "current-temp"},
							elem.Span(attrs.Props{attrs.Class: "label"}, elem.Text("Current")),
							elem.Span(attrs.Props{attrs.Class: "value", attrs.ID: "current-temp"}, elem.Text(currentTemp)),
						),
						elem.Div(attrs.Props{attrs.Class: heatingClass, attrs.ID: "heating-status"}, elem.Text(heatingStatus)),
					),
				),

				elem.Div(attrs.Props{attrs.Class: "control-card"},
					elem.H2(nil, elem.Text("Target Temperature")),
					elem.Form(attrs.Props{
						"hx-post":   "/api/temperature",
						"hx-target": "#response",
					},
						elem.Input(attrs.Props{
							attrs.Type:  "range",
							attrs.Name:  "temperature",
							attrs.Min:   "10",
							attrs.Max:   "30",
							attrs.Step:  "0.5",
							attrs.Value: targetTemp,
							attrs.ID:    "temp-slider",
							"hx-trigger": "change",
						}),
						elem.Div(attrs.Props{attrs.Class: "temp-value", attrs.ID: "target-temp"}, elem.Text(targetTemp+"°C")),
					),

					elem.H2(nil, elem.Text("Mode")),
					elem.Form(attrs.Props{
						"hx-post":   "/api/mode",
						"hx-target": "#response",
					},
						elem.Div(attrs.Props{attrs.Class: "mode-buttons"},
							elem.Button(attrs.Props{
								attrs.Type:  "submit",
								attrs.Name:  "mode",
								attrs.Value: modeHeat,
								attrs.Class: func() string {
									if mode == modeHeat {
										return "mode-btn active"
									}
									return "mode-btn"
								}(),
							}, elem.Text("Heat")),
							elem.Button(attrs.Props{
								attrs.Type:  "submit",
								attrs.Name:  "mode",
								attrs.Value: modeOff,
								attrs.Class: func() string {
									if mode == modeOff {
										return "mode-btn active"
									}
									return "mode-btn"
								}(),
							}, elem.Text("Off")),
						),
					),

					elem.Div(attrs.Props{attrs.ID: "response"}),
				),

				elem.Div(attrs.Props{attrs.Class: "links"},
					elem.A(attrs.Props{attrs.Href: "/debug/eventbus"}, elem.Text("EventBus Debug")),
					elem.Text(" | "),
					elem.A(attrs.Props{attrs.Href: "/metrics"}, elem.Text("Metrics")),
				),
			),

			// SSE handler script
			elem.Script(nil, elem.Text(`
				const eventSource = new EventSource('/events');
				const tempSlider = document.getElementById('temp-slider');
				const targetTempDisplay = document.getElementById('target-temp');

				eventSource.onmessage = function(e) {
					const data = JSON.parse(e.data);
					document.getElementById('current-temp').textContent = data.CurrentTemperature.toFixed(1) + '°C';

					const heatingStatus = document.getElementById('heating-status');
					if (data.HeatingActive) {
						heatingStatus.textContent = 'Heating';
						heatingStatus.className = 'status-heating';
					} else {
						heatingStatus.textContent = 'Off';
						heatingStatus.className = 'status-off';
					}
				};

				tempSlider.addEventListener('input', function(e) {
					targetTempDisplay.textContent = e.target.value + '°C';
				});
			`)),
		),
	).Render()
}

// renderEventBusDebug renders the EventBus debugger interface.
func (s *Server) renderEventBusDebug() string {
	s.mu.RLock()
	sseClientCount := len(s.sseClients)
	currentState := s.currentState
	s.mu.RUnlock()

	stateJSON := "No state available"
	if currentState != nil {
		data, err := json.MarshalIndent(currentState, "", "  ")
		if err == nil {
			stateJSON = string(data)
		}
	}

	return elem.Html(nil,
		elem.Head(nil,
			elem.Title(nil, elem.Text("EventBus Debug")),
			elem.Meta(attrs.Props{attrs.Charset: "utf-8"}),
			elem.Meta(attrs.Props{attrs.Name: "viewport", attrs.Content: "width=device-width, initial-scale=1"}),
			elem.Style(nil, elem.Text(s.getCSS())),
		),
		elem.Body(nil,
			elem.Div(attrs.Props{attrs.Class: "container"},
				elem.H1(nil, elem.Text("EventBus Debugger")),

				elem.Div(attrs.Props{attrs.Class: "debug-card"},
					elem.H2(nil, elem.Text("Statistics")),
					elem.Div(nil,
						elem.P(nil, elem.Text(fmt.Sprintf("Connected SSE Clients: %d", sseClientCount))),
						elem.P(nil, elem.Text(fmt.Sprintf("Server Uptime: %s", time.Since(time.Now()).String()))),
					),
				),

				elem.Div(attrs.Props{attrs.Class: "debug-card"},
					elem.H2(nil, elem.Text("Current State")),
					elem.Pre(nil, elem.Text(stateJSON)),
				),

				elem.Div(attrs.Props{attrs.Class: "links"},
					elem.A(attrs.Props{attrs.Href: "/"}, elem.Text("Back to Thermostat")),
				),
			),
		),
	).Render()
}

// getCSS returns CSS styles for the UI.
func (s *Server) getCSS() string {
	return `
		* { margin: 0; padding: 0; box-sizing: border-box; }
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
			min-height: 100vh;
			padding: 20px;
		}
		.container {
			max-width: 600px;
			margin: 0 auto;
		}
		h1 {
			color: white;
			text-align: center;
			margin-bottom: 30px;
			font-size: 2em;
		}
		h2 {
			color: #333;
			margin-bottom: 15px;
			font-size: 1.2em;
		}
		.status-card, .control-card, .debug-card {
			background: white;
			border-radius: 20px;
			padding: 30px;
			margin-bottom: 20px;
			box-shadow: 0 10px 40px rgba(0,0,0,0.1);
		}
		.temp-display {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		.current-temp {
			display: flex;
			flex-direction: column;
		}
		.current-temp .label {
			color: #666;
			font-size: 0.9em;
			margin-bottom: 5px;
		}
		.current-temp .value {
			font-size: 3em;
			font-weight: bold;
			color: #333;
		}
		.status-off {
			background: #e0e0e0;
			color: #666;
			padding: 10px 20px;
			border-radius: 20px;
			font-weight: bold;
		}
		.status-heating {
			background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
			color: white;
			padding: 10px 20px;
			border-radius: 20px;
			font-weight: bold;
		}
		input[type="range"] {
			width: 100%;
			height: 8px;
			border-radius: 5px;
			background: #e0e0e0;
			outline: none;
			margin: 20px 0;
		}
		input[type="range"]::-webkit-slider-thumb {
			-webkit-appearance: none;
			appearance: none;
			width: 25px;
			height: 25px;
			border-radius: 50%;
			background: #667eea;
			cursor: pointer;
		}
		.temp-value {
			text-align: center;
			font-size: 1.5em;
			font-weight: bold;
			color: #667eea;
		}
		.mode-buttons {
			display: flex;
			gap: 10px;
			margin-top: 15px;
		}
		.mode-btn {
			flex: 1;
			padding: 15px;
			border: 2px solid #e0e0e0;
			background: white;
			border-radius: 10px;
			font-size: 1em;
			font-weight: bold;
			cursor: pointer;
			transition: all 0.3s;
		}
		.mode-btn:hover {
			border-color: #667eea;
		}
		.mode-btn.active {
			background: #667eea;
			color: white;
			border-color: #667eea;
		}
		.links {
			text-align: center;
			margin-top: 20px;
		}
		.links a {
			color: white;
			text-decoration: none;
			font-weight: bold;
		}
		.links a:hover {
			text-decoration: underline;
		}
		pre {
			background: #f5f5f5;
			padding: 15px;
			border-radius: 5px;
			overflow-x: auto;
			font-size: 0.9em;
		}
		#response {
			margin-top: 10px;
			padding: 10px;
			border-radius: 5px;
			text-align: center;
		}
	`
}
