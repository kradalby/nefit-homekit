// Package nefit provides integration with Nefit Easy thermostats via XMPP.
package nefit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	nefitclient "github.com/kradalby/nefit-go/client"
	"github.com/kradalby/nefit-go/types"
	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"tailscale.com/util/eventbus"
)

const (
	modeOff = "off"
)

// Client manages the persistent connection to the Nefit Easy thermostat.
type Client struct {
	cfg          *config.Config
	logger       *slog.Logger
	bus          *events.Bus
	client       *eventbus.Client
	nefitClient  *nefitclient.Client
	ctx          context.Context
	cancel       context.CancelFunc
	reconnectNum int
}

// New creates a new Nefit client.
func New(cfg *config.Config, logger *slog.Logger, bus *events.Bus) (*Client, error) {
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
	busClient, err := bus.Client(events.ClientNefit)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get eventbus client: %w", err)
	}

	// Create nefit-go client
	nefitCfg := nefitclient.Config{
		SerialNumber: cfg.NefitSerial,
		AccessKey:    cfg.NefitAccessKey,
		Password:     cfg.NefitPassword,
	}

	nefitClient, err := nefitclient.NewClient(nefitCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create nefit client: %w", err)
	}

	c := &Client{
		cfg:         cfg,
		logger:      logger,
		bus:         bus,
		client:      busClient,
		nefitClient: nefitClient,
		ctx:         ctx,
		cancel:      cancel,
	}

	logger.Info("nefit client created",
		slog.String("serial", cfg.NefitSerial),
	)

	return c, nil
}

// Start connects to the Nefit Easy backend and starts event handling.
func (c *Client) Start() error {
	c.logger.Info("starting nefit client")

	// Subscribe to push notifications from Nefit backend
	c.nefitClient.Subscribe(c.handleNefitEvent)

	// Subscribe to command events from eventbus
	go c.handleCommands()

	// Connect with retry logic
	go c.connectWithRetry()

	c.logger.Info("nefit client started successfully")
	return nil
}

// connectWithRetry attempts to connect to the Nefit backend with exponential backoff.
func (c *Client) connectWithRetry() {
	backoff := c.cfg.XMPPReconnectBackoff

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("stopping connection attempts")
			return
		default:
		}

		c.logger.Info("attempting to connect to nefit backend",
			slog.Int("attempt", c.reconnectNum+1),
		)

		c.publishConnectionStatus(events.ConnectionStatusConnecting, "")

		err := c.nefitClient.Connect(c.ctx)
		if err == nil {
			c.logger.Info("connected to nefit backend")
			c.publishConnectionStatus(events.ConnectionStatusConnected, "")
			c.reconnectNum = 0

			// Start periodic status polling to keep connection alive
			go c.pollStatus()

			// Wait for connection to close or context to be cancelled
			<-c.ctx.Done()
			return
		}

		c.reconnectNum++
		c.logger.Error("failed to connect to nefit backend",
			slog.Any("error", err),
			slog.Int("attempt", c.reconnectNum),
			slog.Duration("backoff", backoff),
		)

		c.publishConnectionStatus(events.ConnectionStatusReconnecting, err.Error())

		// Exponential backoff with max
		select {
		case <-time.After(backoff):
			backoff *= 2
			if backoff > c.cfg.XMPPMaxReconnectWait {
				backoff = c.cfg.XMPPMaxReconnectWait
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// pollStatus periodically requests status to keep connection alive and get latest state.
func (c *Client) pollStatus() {
	ticker := time.NewTicker(c.cfg.XMPPKeepaliveInterval)
	defer ticker.Stop()

	c.logger.Debug("starting status polling",
		slog.Duration("interval", c.cfg.XMPPKeepaliveInterval),
	)

	for {
		select {
		case <-ticker.C:
			if err := c.fetchAndPublishStatus(); err != nil {
				c.logger.Warn("failed to fetch status", slog.Any("error", err))
			}
		case <-c.ctx.Done():
			c.logger.Debug("stopping status polling")
			return
		}
	}
}

// fetchAndPublishStatus retrieves current status and publishes it to eventbus.
func (c *Client) fetchAndPublishStatus() error {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	status, err := c.nefitClient.Status(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}
	if status == nil {
		return fmt.Errorf("status response was nil")
	}

	c.publishStateUpdate(*status)
	return nil
}

// handleNefitEvent is called when the Nefit backend sends a push notification.
func (c *Client) handleNefitEvent(uri string, data interface{}) {
	c.logger.Debug("received nefit event",
		slog.String("uri", uri),
	)

	// For status updates, publish to eventbus
	if uri == types.URIStatus {
		if raw, ok := data.(map[string]interface{}); ok {
			status := types.Status{}
			if in, ok := raw["in_house_temp"].(float64); ok {
				status.InHouseTemp = in
			}
			if sp, ok := raw["temp_setpoint"].(float64); ok {
				status.TempSetpoint = sp
			}
			if boiler, ok := raw["boiler_indicator"].(string); ok {
				status.BoilerIndicator = boiler
			}
			if mode, ok := raw["user_mode"].(string); ok {
				status.UserMode = mode
			}
			c.publishStateUpdate(status)
		}
	}
}

// publishStateUpdate converts Nefit status to our event format and publishes it.
func (c *Client) publishStateUpdate(status types.Status) {
	// Determine if heating is active
	heatingActive := status.BoilerIndicator == "CH" || status.BoilerIndicator == "HW"

	// Determine mode
	mode := "heat"
	if status.UserMode == modeOff {
		mode = modeOff
	}

	event := events.StateUpdateEvent{
		Source:             "nefit",
		CurrentTemperature: status.InHouseTemp,
		TargetTemperature:  status.TempSetpoint,
		HeatingActive:      heatingActive,
		Mode:               mode,
		HotWaterActive:     status.HotWaterActive,
	}

	c.logger.Debug("publishing state update",
		slog.Float64("current_temp", event.CurrentTemperature),
		slog.Float64("target_temp", event.TargetTemperature),
		slog.Bool("heating", event.HeatingActive),
	)

	c.bus.PublishStateUpdate(c.client, event)
}

// handleCommands subscribes to command events and executes them on the Nefit backend.
func (c *Client) handleCommands() {
	sub := eventbus.Subscribe[events.CommandEvent](c.client)
	defer sub.Close()

	c.logger.Info("subscribed to command events")

	for {
		select {
		case event := <-sub.Events():
			// Only process commands from homekit and web (not from ourselves)
			if event.Source == "nefit" {
				continue
			}

			c.handleCommand(event)
		case <-c.ctx.Done():
			c.logger.Info("stopping command handler")
			return
		}
	}
}

// handleCommand executes a single command on the Nefit backend.
func (c *Client) handleCommand(cmd events.CommandEvent) {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	switch cmd.CommandType {
	case events.CommandTypeSetTemperature:
		if cmd.TargetTemperature == nil {
			c.logger.Warn("set temperature command missing temperature value")
			return
		}

		c.logger.Info("setting target temperature",
			slog.Float64("temperature", *cmd.TargetTemperature),
		)

		if err := c.nefitClient.Put(ctx, types.URIManualSetpoint, *cmd.TargetTemperature); err != nil {
			c.logger.Error("failed to set temperature", slog.Any("error", err))
			return
		}

		// Fetch updated status to confirm change
		if err := c.fetchAndPublishStatus(); err != nil {
			c.logger.Warn("failed to fetch status after temperature change", slog.Any("error", err))
		}

	case events.CommandTypeSetMode:
		if cmd.Mode == nil {
			c.logger.Warn("set mode command missing mode value")
			return
		}

		c.logger.Info("setting mode",
			slog.String("mode", *cmd.Mode),
		)

		// Map our mode to Nefit mode
		nefitMode := "manual"
		if *cmd.Mode == modeOff {
			nefitMode = modeOff
		}

		if err := c.nefitClient.Put(ctx, types.URIUserMode, nefitMode); err != nil {
			c.logger.Error("failed to set mode", slog.Any("error", err))
			return
		}

		// Fetch updated status to confirm change
		if err := c.fetchAndPublishStatus(); err != nil {
			c.logger.Warn("failed to fetch status after mode change", slog.Any("error", err))
		}

	case events.CommandTypeSetHotWater:
		if cmd.HotWaterEnabled == nil {
			c.logger.Warn("set hot water command missing value")
			return
		}

		c.logger.Info("setting hot water",
			slog.Bool("enabled", *cmd.HotWaterEnabled),
		)

		mode := modeOff
		if *cmd.HotWaterEnabled {
			mode = "on"
		}

		if err := c.nefitClient.Put(ctx, types.URIHotWaterManualMode, mode); err != nil {
			c.logger.Error("failed to set hot water", slog.Any("error", err))
			return
		}

	default:
		c.logger.Warn("unknown command type",
			slog.String("type", string(cmd.CommandType)),
		)
	}
}

// publishConnectionStatus publishes a connection status event.
func (c *Client) publishConnectionStatus(status events.ConnectionStatus, errMsg string) {
	event := events.ConnectionStatusEvent{
		Component:  "nefit",
		Status:     status,
		Error:      errMsg,
		Reconnects: c.reconnectNum,
	}
	c.bus.PublishConnectionStatus(c.client, event)
}

// Close gracefully shuts down the Nefit client.
func (c *Client) Close() error {
	c.logger.Info("shutting down nefit client")

	c.publishConnectionStatus(events.ConnectionStatusDisconnected, "")

	c.cancel()

	if c.nefitClient != nil {
		if err := c.nefitClient.Close(); err != nil {
			c.logger.Warn("error closing nefit client", slog.Any("error", err))
		}
	}

	c.logger.Info("nefit client shut down complete")
	return nil
}
