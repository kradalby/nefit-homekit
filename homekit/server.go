// Package homekit wires brutella/hap into the shared eventbus so HomeKit and the
// thermostat stay in sync.
package homekit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	homekitqr "github.com/kradalby/homekit-qr"
	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"tailscale.com/util/eventbus"
)

const (
	modeOff  = "off"
	modeHeat = "heat"

	// Temperature constants.
	tempOff       = 13.0 // Temperature to set when "off"
	tempDefaultOn = 18.0 // Default temperature when turning "on" with no previous state
)

// Server manages the HomeKit HAP server and accessory.
type Server struct {
	cfg       *config.Config
	logger    *slog.Logger
	bus       *events.Bus
	client    *eventbus.Client
	server    *hap.Server
	accessory *accessory.Thermostat
	ctx       context.Context
	cancel    context.CancelFunc
}

// New creates a new HomeKit server.
func New(cfg *config.Config, logger *slog.Logger, bus *events.Bus) (*Server, error) {
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

	// Set pin and listen address
	s.server.Pin = cfg.HAPPin
	s.server.Addr = cfg.HAPAddrPort().String()

	logger.Info("homekit server created",
		slog.String("name", info.Name),
		slog.String("serial", info.SerialNumber),
		slog.String("pin", cfg.HAPPin),
		slog.String("addr", cfg.HAPAddrPort().String()),
	)

	return s, nil
}

// Start starts the HomeKit server and begins handling events.
func (s *Server) Start() error {
	s.logger.Info("starting homekit server")

	// Generate and print QR code
	s.printSetupQRCode()

	// Subscribe to state update events
	go s.handleStateUpdates()

	// Setup accessory callbacks for user interactions
	s.setupAccessoryCallbacks()

	// Start HAP server in background
	go func() {
		if err := s.server.ListenAndServe(s.ctx); err != nil {
			s.logger.Error("HAP server error", slog.Any("error", err))
		}
	}()

	// Publish connection status
	s.publishConnectionStatus(events.ConnectionStatusConnected, "")

	s.logger.Info("homekit server started successfully")
	return nil
}

// printSetupQRCode generates and prints the HomeKit setup QR code to stdout.
func (s *Server) printSetupQRCode() {
	qrConfig := homekitqr.QRCodeConfig{
		SetupURIConfig: homekitqr.SetupURIConfig{
			PairingCode: s.cfg.HAPPin,
			SetupID:     s.cfg.NefitSerial, // Use serial as setup ID
			Category:    homekitqr.CategoryThermostat,
		},
	}

	qrCode, err := homekitqr.GenerateQRTerminal(qrConfig)
	if err != nil {
		s.logger.Warn("failed to generate QR code", slog.Any("error", err))
		return
	}

	separator := strings.Repeat("=", 60)
	dashes := strings.Repeat("-", 60)

	fmt.Printf("\n%s\n", separator)
	fmt.Println("HomeKit Setup Information")
	fmt.Println(separator)
	fmt.Printf("Setup Code: %s\n", homekitqr.FormatPairingCode(s.cfg.HAPPin))
	fmt.Println(dashes)
	fmt.Println("Scan this QR code with your iPhone to add to HomeKit:")
	fmt.Println(qrCode)
	fmt.Printf("%s\n\n", separator)
}

// setupAccessoryCallbacks sets up callbacks for user interactions.
func (s *Server) setupAccessoryCallbacks() {
	// Target temperature changed
	s.accessory.Thermostat.TargetTemperature.OnValueRemoteUpdate(func(temp float64) {
		s.logger.Info("target temperature changed via HomeKit",
			slog.Float64("temperature", temp),
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
			slog.Int("state", state),
		)

		switch state {
		case 0: // Off
			// Save current temperature before turning "off"
			currentTemp := s.accessory.Thermostat.TargetTemperature.Value()
			if err := savePreviousTemperature(s.cfg.HAPStoragePath, currentTemp); err != nil {
				s.logger.Warn("failed to save previous temperature",
					slog.Any("error", err),
					slog.Float64("temperature", currentTemp),
				)
			} else {
				s.logger.Info("saved previous temperature",
					slog.Float64("temperature", currentTemp),
				)
			}

			// Set to manual mode (heat) at low temperature
			mode := modeHeat
			temp := tempOff

			s.logger.Info("turning off: setting to manual mode at low temperature",
				slog.Float64("temperature", temp),
			)

			// Publish mode command
			modeEvent := events.CommandEvent{
				Source:      "homekit",
				CommandType: events.CommandTypeSetMode,
				Mode:        &mode,
			}
			s.bus.PublishCommand(s.client, modeEvent)

			// Publish temperature command
			tempEvent := events.CommandEvent{
				Source:            "homekit",
				CommandType:       events.CommandTypeSetTemperature,
				TargetTemperature: &temp,
			}
			s.bus.PublishCommand(s.client, tempEvent)

		case 1, 3: // Heat or Auto (Nefit only supports heat)
			// Load previous temperature or use default
			temp := tempDefaultOn
			if prevTemp, err := loadPreviousTemperature(s.cfg.HAPStoragePath); err == nil {
				temp = prevTemp
				s.logger.Info("restored previous temperature",
					slog.Float64("temperature", temp),
				)
			} else {
				s.logger.Info("using default temperature (no previous state)",
					slog.Float64("temperature", temp),
					slog.Any("error", err),
				)
			}

			// Set to manual mode (heat)
			mode := modeHeat

			s.logger.Info("turning on: setting to manual mode",
				slog.Float64("temperature", temp),
			)

			// Publish mode command
			modeEvent := events.CommandEvent{
				Source:      "homekit",
				CommandType: events.CommandTypeSetMode,
				Mode:        &mode,
			}
			s.bus.PublishCommand(s.client, modeEvent)

			// Publish temperature command
			tempEvent := events.CommandEvent{
				Source:            "homekit",
				CommandType:       events.CommandTypeSetTemperature,
				TargetTemperature: &temp,
			}
			s.bus.PublishCommand(s.client, tempEvent)

		default:
			s.logger.Warn("unknown heating state", slog.Int("state", state))
			return
		}
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
		slog.Float64("current_temp", event.CurrentTemperature),
		slog.Float64("target_temp", event.TargetTemperature),
		slog.Bool("heating", event.HeatingActive),
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
		s.logger.Warn("unknown mode", slog.String("mode", event.Mode))
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
