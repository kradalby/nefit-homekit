// Package homekit wires brutella/hap into the shared eventbus so HomeKit and the
// thermostat stay in sync.
package homekit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	homekitqr "github.com/kradalby/homekit-qr"
	"tailscale.com/util/eventbus"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
)

const (
	// Event modes. "off" is Nefit's clock schedule, not off: the thermostat
	// has no off and keeps heating on its program.
	modeOff  = "off"
	modeHeat = "heat"
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

	// serve runs the HAP server until its context ends; tests stub it.
	serve func(context.Context) error

	// states exists from New on: the bus drops repeated states, so a status
	// published before a later subscription would never come again.
	states *eventbus.Subscriber[events.StateUpdateEvent]
	wg     sync.WaitGroup
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
		Name:         cfg.BridgeName,
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

	// Offer only what Nefit can do, so hap rejects Off before any callback.
	// Auto, Nefit's default, stands in until the first status arrives.
	target := s.accessory.Thermostat.TargetHeatingCoolingState
	target.ValidVals = []int{characteristic.TargetHeatingCoolingStateHeat, characteristic.TargetHeatingCoolingStateAuto}
	s.setChar("TargetHeatingCoolingState", target.SetValue(characteristic.TargetHeatingCoolingStateAuto))

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
	s.serve = s.server.ListenAndServe
	s.states = eventbus.Subscribe[events.StateUpdateEvent](client)

	logger.Info(
		"homekit server created",
		slog.String("name", info.Name),
		slog.String("serial", info.SerialNumber),
		slog.String("pin", cfg.HAPPin),
		slog.String("addr", cfg.HAPAddrPort().String()),
		slog.String("bridge_name", cfg.BridgeName),
	)

	return s, nil
}

// Start starts the HomeKit server and begins handling events.
func (s *Server) Start() error {
	s.logger.Info("starting homekit server")

	// Generate and print QR code
	s.printSetupQRCode()

	s.wg.Go(s.handleStateUpdates)

	// Setup accessory callbacks for user interactions
	s.setupAccessoryCallbacks()

	// Start HAP server in background
	go func() {
		if err := s.serve(s.ctx); err != nil {
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
		PairingCode: s.cfg.HAPPin,
		SetupID:     s.cfg.NefitSerial, // Use serial as setup ID
		Category:    homekitqr.CategoryThermostat,
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
		s.logger.Info(
			"target temperature changed via HomeKit",
			slog.Float64("temperature", temp),
		)

		// Publish command event
		event := events.CommandEvent{
			Source:            events.SourceHomeKit,
			CommandType:       events.CommandTypeSetTemperature,
			TargetTemperature: &temp,
		}
		s.bus.PublishCommand(s.client, event)
	})

	// Target heating cooling state changed
	s.accessory.Thermostat.TargetHeatingCoolingState.OnValueRemoteUpdate(func(state int) {
		s.logger.Info(
			"heating mode changed via HomeKit",
			slog.Int("state", state),
		)

		// No setpoint goes with Heat: Nefit resumes its own manual setpoint.
		var mode string
		switch state {
		case characteristic.TargetHeatingCoolingStateAuto:
			mode = modeOff
		case characteristic.TargetHeatingCoolingStateHeat:
			mode = modeHeat
		default:
			s.logger.Warn("unknown heating state", slog.Int("state", state))
			return
		}

		s.bus.PublishCommand(s.client, events.CommandEvent{
			Source:      events.SourceHomeKit,
			CommandType: events.CommandTypeSetMode,
			Mode:        &mode,
		})
	})
}

// handleStateUpdates mirrors state update events onto the accessory.
func (s *Server) handleStateUpdates() {
	for {
		select {
		case event := <-s.states.Events():
			s.updateAccessory(event)
		case <-s.ctx.Done():
			s.logger.Info("stopping state update handler")
			return
		}
	}
}

// setChar applies a HomeKit characteristic SetValue result, logging any error.
// Characteristic updates are best-effort; a failure should not abort the sync.
func (s *Server) setChar(name string, err error) {
	if err != nil {
		s.logger.Warn(
			"failed to set HomeKit characteristic",
			slog.String("characteristic", name),
			slog.Any("error", err),
		)
	}
}

// updateAccessory updates the accessory with new state.
func (s *Server) updateAccessory(event events.StateUpdateEvent) {
	// Only update if event is from nefit (avoid loops)
	if event.Source != events.SourceNefit {
		return
	}

	s.logger.Debug(
		"updating accessory from state event",
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
		s.setChar("CurrentHeatingCoolingState", s.accessory.Thermostat.CurrentHeatingCoolingState.SetValue(1)) // Heating
	} else {
		s.setChar("CurrentHeatingCoolingState", s.accessory.Thermostat.CurrentHeatingCoolingState.SetValue(0)) // Off
	}

	switch event.Mode {
	case modeOff:
		s.setChar("TargetHeatingCoolingState", s.accessory.Thermostat.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateAuto))
	case modeHeat:
		s.setChar("TargetHeatingCoolingState", s.accessory.Thermostat.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateHeat))
	default:
		s.logger.Warn("unknown mode", slog.String("mode", event.Mode))
	}
}

// publishConnectionStatus publishes a connection status event.
func (s *Server) publishConnectionStatus(status events.ConnectionStatus, errMsg string) {
	event := events.ConnectionStatusEvent{
		Component: events.SourceHomeKit,
		Status:    status,
		Error:     errMsg,
	}
	s.bus.PublishConnectionStatus(s.client, event)
}

// Close gracefully shuts down the HomeKit server.
func (s *Server) Close() error {
	s.logger.Info("shutting down homekit server")

	s.publishConnectionStatus(events.ConnectionStatusDisconnected, "")

	// The HAP server stops with the context.
	s.cancel()
	s.wg.Wait()
	s.states.Close()

	s.logger.Info("homekit server shut down complete")
	return nil
}
