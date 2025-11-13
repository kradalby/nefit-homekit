// Package homekit provides HomeKit HAP server integration.
package homekit

import (
	"context"
	"fmt"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"go.uber.org/zap"
	"tailscale.com/util/eventbus"
)

const (
	modeOff  = "off"
	modeHeat = "heat"
)

// Server manages the HomeKit HAP server and accessory.
type Server struct {
	cfg       *config.Config
	logger    *zap.Logger
	bus       *events.Bus
	client    *eventbus.Client
	server    *hap.Server
	accessory *accessory.Thermostat
	ctx       context.Context
	cancel    context.CancelFunc
}

// New creates a new HomeKit server.
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
	client, err := bus.Client(events.ClientHomeKit)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get eventbus client: %w", err)
	}

	s := &Server{
		cfg:    cfg,
		logger: logger,
		bus:    bus,
		client: client,
		ctx:    ctx,
		cancel: cancel,
	}

	// Create thermostat accessory
	info := accessory.Info{
		Name:         "Nefit Easy",
		Manufacturer: "Bosch",
		Model:        "Nefit Easy",
		SerialNumber: cfg.NefitSerial,
	}

	s.accessory = accessory.NewThermostat(info)

	// Set temperature range
	s.accessory.Thermostat.TargetTemperature.SetMinValue(10.0)
	s.accessory.Thermostat.TargetTemperature.SetMaxValue(30.0)
	s.accessory.Thermostat.TargetTemperature.SetStepValue(0.5)
	s.accessory.Thermostat.TargetTemperature.SetValue(20.0)

	// Create HAP server
	s.server, err = hap.NewServer(
		hap.NewFsStore(cfg.HAPStoragePath),
		s.accessory.A,
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create HAP server: %w", err)
	}

	// Set pin
	s.server.Pin = cfg.HAPPin

	// Set port
	s.server.Addr = fmt.Sprintf(":%d", cfg.HAPPort)

	logger.Info("homekit server created",
		zap.String("name", info.Name),
		zap.String("serial", info.SerialNumber),
		zap.String("pin", cfg.HAPPin),
		zap.Int("port", cfg.HAPPort),
	)

	return s, nil
}

// Start starts the HomeKit server and begins handling events.
func (s *Server) Start() error {
	s.logger.Info("starting homekit server")

	// Subscribe to state update events
	go s.handleStateUpdates()

	// Setup accessory callbacks for user interactions
	s.setupAccessoryCallbacks()

	// Start HAP server in background
	go func() {
		if err := s.server.ListenAndServe(s.ctx); err != nil {
			s.logger.Error("HAP server error", zap.Error(err))
		}
	}()

	// Publish connection status
	s.publishConnectionStatus(events.ConnectionStatusConnected, "")

	s.logger.Info("homekit server started successfully")
	return nil
}

// setupAccessoryCallbacks sets up callbacks for user interactions.
func (s *Server) setupAccessoryCallbacks() {
	// Target temperature changed
	s.accessory.Thermostat.TargetTemperature.OnValueRemoteUpdate(func(temp float64) {
		s.logger.Info("target temperature changed via HomeKit",
			zap.Float64("temperature", temp),
		)

		// Publish command event
		event := events.CommandEvent{
			Source:            "homekit",
			CommandType:       events.CommandTypeSetTemperature,
			TargetTemperature: &temp,
		}
		s.bus.PublishCommand(s.client, event)
	})

	// Target heating cooling state changed
	s.accessory.Thermostat.TargetHeatingCoolingState.OnValueRemoteUpdate(func(state int) {
		s.logger.Info("heating mode changed via HomeKit",
			zap.Int("state", state),
		)

		// Map HomeKit state to mode string
		var mode string
		switch state {
		case 0: // Off
			mode = modeOff
		case 1: // Heat
			mode = modeHeat
		case 3: // Auto
			mode = modeHeat // Nefit only supports heat, not auto
		default:
			s.logger.Warn("unknown heating state", zap.Int("state", state))
			return
		}

		// Publish command event
		event := events.CommandEvent{
			Source:      "homekit",
			CommandType: events.CommandTypeSetMode,
			Mode:        &mode,
		}
		s.bus.PublishCommand(s.client, event)
	})
}

// handleStateUpdates subscribes to state update events and updates the accessory.
func (s *Server) handleStateUpdates() {
	sub := eventbus.Subscribe[events.StateUpdateEvent](s.client)
	defer sub.Close()

	s.logger.Info("subscribed to state update events")

	for {
		select {
		case event := <-sub.Events():
			s.updateAccessory(event)
		case <-s.ctx.Done():
			s.logger.Info("stopping state update handler")
			return
		}
	}
}

// updateAccessory updates the accessory with new state.
func (s *Server) updateAccessory(event events.StateUpdateEvent) {
	// Only update if event is from nefit (avoid loops)
	if event.Source != "nefit" {
		return
	}

	s.logger.Debug("updating accessory from state event",
		zap.Float64("current_temp", event.CurrentTemperature),
		zap.Float64("target_temp", event.TargetTemperature),
		zap.Bool("heating", event.HeatingActive),
	)

	// Update current temperature
	s.accessory.Thermostat.CurrentTemperature.SetValue(event.CurrentTemperature)

	// Update target temperature
	s.accessory.Thermostat.TargetTemperature.SetValue(event.TargetTemperature)

	// Update current heating cooling state
	if event.HeatingActive {
		_ = s.accessory.Thermostat.CurrentHeatingCoolingState.SetValue(1) // Heating
	} else {
		_ = s.accessory.Thermostat.CurrentHeatingCoolingState.SetValue(0) // Off
	}

	// Update target heating cooling state based on mode
	switch event.Mode {
	case modeOff:
		_ = s.accessory.Thermostat.TargetHeatingCoolingState.SetValue(0) // Off
	case modeHeat:
		_ = s.accessory.Thermostat.TargetHeatingCoolingState.SetValue(1) // Heat
	default:
		s.logger.Warn("unknown mode", zap.String("mode", event.Mode))
	}
}

// publishConnectionStatus publishes a connection status event.
func (s *Server) publishConnectionStatus(status events.ConnectionStatus, errMsg string) {
	event := events.ConnectionStatusEvent{
		Component: "homekit",
		Status:    status,
		Error:     errMsg,
	}
	s.bus.PublishConnectionStatus(s.client, event)
}

// Close gracefully shuts down the HomeKit server.
func (s *Server) Close() error {
	s.logger.Info("shutting down homekit server")

	s.publishConnectionStatus(events.ConnectionStatusDisconnected, "")

	s.cancel()

	// The server stops when the context is cancelled

	s.logger.Info("homekit server shut down complete")
	return nil
}
