// Package nefit provides integration with Nefit Easy thermostats via XMPP.
package nefit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	nefitclient "github.com/kradalby/nefit-go/client"
	"github.com/kradalby/nefit-go/types"
	"tailscale.com/util/eventbus"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
)

const (
	modeOff         = "off"
	modeHeat        = "heat"
	nefitModeManual = "manual"
	nefitModeClock  = "clock"

	commandDebounceInterval = 500 * time.Millisecond
)

// heatingBoilerStates are the types.Status.BoilerIndicator values that mean the
// boiler is firing. nefit-go's client.Status runs the raw BAI wire value
// through parseBoilerIndicator ("CH" -> "central heating", "HW" -> "hot
// water"), while its types.Status doc comment still documents the raw form.
// Accept both spellings so an upstream realignment either way cannot silently
// pin HeatingActive to false.
var heatingBoilerStates = []string{"central heating", "hot water", "CH", "HW"}

// backend is the part of the nefit-go client this package drives, so tests can
// substitute a fake for the XMPP connection.
type backend interface {
	Connect(ctx context.Context) error
	Done() <-chan struct{}
	Close() error
	Subscribe(handler nefitclient.EventHandler)
	Status(ctx context.Context, includeOutdoorTemp bool) (*types.Status, error)
	SetTemperature(ctx context.Context, temperature float64) error
	SetUserMode(ctx context.Context, mode string) error
	SetHotWaterSupply(ctx context.Context, enabled bool) error
}

// Client manages the persistent connection to the Nefit Easy thermostat.
type Client struct {
	cfg         *config.Config
	logger      *slog.Logger
	bus         *events.Bus
	client      *eventbus.Client
	nefitClient backend
	ctx         context.Context
	cancel      context.CancelFunc
	stateMu     sync.RWMutex
	lastEvent   *events.StateUpdateEvent

	// refresh wakes refreshLoop. One slot: requests made while a fetch runs
	// coalesce into a single follow-up fetch.
	refresh      chan struct{}
	forceRefresh atomic.Bool

	// wg tracks the goroutines that publish, so Close can outlast them and
	// nothing publishes onto a bus closed right after.
	wg sync.WaitGroup
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
		refresh:     make(chan struct{}, 1),
	}

	logger.Info(
		"nefit client created",
		slog.String("serial", cfg.NefitSerial),
	)

	return c, nil
}

// Start connects to the Nefit Easy backend and starts event handling.
func (c *Client) Start() error {
	c.logger.Info("starting nefit client")

	// Subscribe to push notifications from Nefit backend
	c.nefitClient.Subscribe(c.handleNefitEvent)

	// Subscribe before returning so commands published right after Start are
	// not lost.
	sub := eventbus.Subscribe[events.CommandEvent](c.client)
	c.wg.Go(func() { c.handleCommands(sub) })

	c.wg.Go(c.refreshLoop)
	c.wg.Go(c.connectWithRetry)

	c.logger.Info("nefit client started successfully")
	return nil
}

// connectWithRetry keeps a session with the Nefit backend open until Close and
// publishes its status. Requests reconnect on their own; pushes only arrive
// while a session is up.
func (c *Client) connectWithRetry() {
	backoff := c.cfg.XMPPReconnectBackoff
	failures := 0

	for {
		c.logger.Info(
			"attempting to connect to nefit backend",
			slog.Int("attempt", failures+1),
		)

		c.publishConnectionStatus(events.ConnectionStatusConnecting, "", failures)

		err := c.nefitClient.Connect(c.ctx)
		if c.ctx.Err() != nil {
			return
		}
		if err == nil {
			lasted := c.holdSession(failures)
			if c.ctx.Err() != nil {
				return
			}
			// A session that ends at once counts as a failed attempt, so a
			// backend dropping every login is not redialled in a tight loop.
			if lasted >= c.cfg.XMPPReconnectBackoff {
				backoff, failures = c.cfg.XMPPReconnectBackoff, 0
				continue
			}
			err = errSessionEndedEarly
		}

		failures++
		c.logger.Error(
			"failed to connect to nefit backend",
			slog.Any("error", err),
			slog.Int("attempt", failures),
			slog.Duration("backoff", backoff),
		)

		c.publishConnectionStatus(events.ConnectionStatusReconnecting, err.Error(), failures)

		select {
		case <-time.After(jitter(backoff)):
			backoff = min(backoff*2, c.cfg.XMPPMaxReconnectWait)
		case <-c.ctx.Done():
			return
		}
	}
}

var errSessionEndedEarly = errors.New("session ended right after connecting")

// holdSession announces the session Connect opened and waits for it or the
// client to end, returning how long it lasted.
func (c *Client) holdSession(reconnects int) time.Duration {
	done := c.nefitClient.Done()
	start := time.Now()

	c.logger.Info("connected to nefit backend")
	c.publishConnectionStatus(events.ConnectionStatusConnected, "", reconnects)
	// Pushes missed while down would otherwise wait for the next poll. A
	// silent gateway leaves it unanswered, which ends the session early.
	c.requestRefresh(false)

	select {
	case <-done:
	case <-c.ctx.Done():
	}

	// Close ends the session too; that is not a loss.
	if c.ctx.Err() == nil {
		c.logger.Warn("lost connection to nefit backend")
		c.publishConnectionStatus(events.ConnectionStatusDisconnected, "", reconnects)
	}

	return time.Since(start)
}

// jitter picks a wait in [d/2, d] so bridges cut off by the same outage do not
// redial in step.
func jitter(d time.Duration) time.Duration {
	return d/2 + rand.N(d/2+1) //nolint:gosec // G404: jitter needs spread, not secrecy.
}

// refreshLoop is the only goroutine that fetches and publishes status, so an
// older response can never overwrite a newer one. It outlives sessions and
// fetches on request or on an interval; the poll also exposes a silently dead
// session, which nefit-go retires once a request goes unanswered.
func (c *Client) refreshLoop() {
	ticker := time.NewTicker(c.cfg.XMPPKeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-c.refresh:
		case <-c.ctx.Done():
			return
		}

		c.syncState(c.forceRefresh.Swap(false))
	}
}

// requestRefresh asks refreshLoop for a fresh status without blocking. force
// sticks until a fetch consumes it, even when requests coalesce.
func (c *Client) requestRefresh(force bool) {
	if force {
		c.forceRefresh.Store(true)
	}

	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// fetchAndPublishStatus retrieves current status and publishes it to eventbus.
func (c *Client) fetchAndPublishStatus(force bool) error {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	status, err := c.nefitClient.Status(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}
	if status == nil {
		return fmt.Errorf("status response was nil")
	}

	c.publishStateUpdate(*status, force)
	return nil
}

// handleNefitEvent is called when the Nefit backend sends a push notification.
//
// A push carries the raw device representation ({"id": ..., "value": {"IHT":
// ..., "TSP": ..., "BAI": ...}}) using the backend's abbreviated keys, and may
// be a partial view of the resource. Rather than decode that wire format a
// second time here, a status push is treated purely as an invalidation signal
// and the full status is re-read through nefit-go's own parser, which owns the
// key mapping and the value translations.
func (c *Client) handleNefitEvent(uri string, _ any) {
	c.logger.Debug(
		"received nefit event",
		slog.String("uri", uri),
	)

	if uri == types.URIStatus {
		c.requestRefresh(false)
	}
}

// publishStateUpdate converts Nefit status to our event format and publishes it.
func (c *Client) publishStateUpdate(status types.Status, force bool) {
	// Determine if heating is active
	heatingActive := slices.Contains(heatingBoilerStates, status.BoilerIndicator)

	// Determine mode
	mode := modeHeat
	switch status.UserMode {
	case nefitModeClock:
		mode = modeOff
	case nefitModeManual:
		mode = modeHeat
	}

	event := events.StateUpdateEvent{
		Source:             events.SourceNefit,
		CurrentTemperature: status.InHouseTemp,
		TargetTemperature:  status.TempSetpoint,
		HeatingActive:      heatingActive,
		Mode:               mode,
		HotWaterActive:     status.HotWaterActive,
	}

	c.logger.Debug(
		"publishing state update",
		slog.Float64("current_temp", event.CurrentTemperature),
		slog.Float64("target_temp", event.TargetTemperature),
		slog.Bool("heating", event.HeatingActive),
	)

	if force {
		c.bus.PublishStateUpdateForce(c.client, event)
	} else {
		c.bus.PublishStateUpdate(c.client, event)
	}

	eventCopy := event
	c.stateMu.Lock()
	c.lastEvent = &eventCopy
	c.stateMu.Unlock()
}

// desired folds the commands received in one debounce window. Each command
// type owns one field, so a later command replaces only its own kind and every
// requested change survives to the flush, save a setpoint that Auto follows.
// Nil means not requested.
type desired struct {
	mode        *string
	temperature *float64
	hotWater    *bool
}

// with returns d with cmd folded in; d itself is left untouched.
func (d desired) with(cmd events.CommandEvent) (desired, error) {
	switch cmd.CommandType {
	case events.CommandTypeSetMode:
		if cmd.Mode == nil {
			return d, errors.New("set mode command missing mode")
		}
		d.mode = new(*cmd.Mode)
		// nefit-go's SetTemperature turns on a manual override, which would
		// hold off the clock program Auto asked for.
		if *cmd.Mode == modeOff {
			d.temperature = nil
		}
	case events.CommandTypeSetTemperature:
		if cmd.TargetTemperature == nil {
			return d, errors.New("set temperature command missing temperature")
		}
		d.temperature = new(*cmd.TargetTemperature)
	case events.CommandTypeSetHotWater:
		if cmd.HotWaterEnabled == nil {
			return d, errors.New("set hot water command missing value")
		}
		d.hotWater = new(*cmd.HotWaterEnabled)
	default:
		return d, fmt.Errorf("unknown command type %q", cmd.CommandType)
	}

	return d, nil
}

// handleCommands debounces command events from sub and applies them to the
// Nefit backend.
func (c *Client) handleCommands(sub *eventbus.Subscriber[events.CommandEvent]) {
	defer sub.Close()

	var (
		pending desired
		timer   *time.Timer
		timerC  <-chan time.Time
	)

	resetTimer := func() {
		if timer == nil {
			timer = time.NewTimer(commandDebounceInterval)
			timerC = timer.C
			return
		}

		// Go 1.23 made timer channels unbuffered, so Stop/Reset can no longer
		// leave a stale tick behind and the old drain dance is dead code. Go
		// 1.27 removed the asynctimerchan escape hatch that could have
		// restored the buffered behavior, and this repo sets no GODEBUG.
		timer.Stop()
		timer.Reset(commandDebounceInterval)
	}

	stopTimer := func() {
		if timer == nil {
			return
		}

		timer.Stop()
		timer = nil
		timerC = nil
	}

	for {
		select {
		case event, ok := <-sub.Events():
			if !ok {
				c.logger.Info("command subscription closed")
				stopTimer()
				return
			}
			// Only process commands from homekit and web (not from ourselves)
			if event.Source == events.SourceNefit {
				continue
			}

			next, err := pending.with(event)
			if err != nil {
				c.logger.Warn("ignoring command", slog.Any("error", err))
				continue
			}
			pending = next
			resetTimer()
		case <-timerC:
			timer = nil
			timerC = nil

			err := c.apply(pending)
			if err != nil {
				c.logger.Error("failed to apply commands", slog.Any("error", err))
			}
			pending = desired{}
			c.requestRefresh(err != nil)
		case <-c.ctx.Done():
			c.logger.Info("stopping command handler")
			stopTimer()
			return
		}
	}
}

// apply pushes every change in d to the Nefit backend. Mode goes first so the
// setpoint and hot water land on the mode they were meant for; nefit-go picks
// the hot water endpoint by the current mode, so hot water waits on the mode.
func (c *Client) apply(d desired) error {
	var errs []error

	var modeErr error
	if d.mode != nil {
		c.logger.Info("setting mode", slog.String("mode", *d.mode))
		modeErr = c.withTimeout(func(ctx context.Context) error {
			return c.setUserMode(ctx, *d.mode)
		})
		errs = append(errs, modeErr)
	}

	if d.temperature != nil {
		c.logger.Info("setting target temperature", slog.Float64("temperature", *d.temperature))
		errs = append(errs, c.withTimeout(func(ctx context.Context) error {
			if err := c.nefitClient.SetTemperature(ctx, *d.temperature); err != nil {
				return fmt.Errorf("failed to set temperature: %w", err)
			}
			return nil
		}))
	}

	if d.hotWater != nil {
		if modeErr != nil {
			return errors.Join(append(errs, errors.New("skipped hot water: mode not set"))...)
		}
		c.logger.Info("setting hot water", slog.Bool("enabled", *d.hotWater))
		errs = append(errs, c.withTimeout(func(ctx context.Context) error {
			if err := c.nefitClient.SetHotWaterSupply(ctx, *d.hotWater); err != nil {
				return fmt.Errorf("failed to set hot water: %w", err)
			}
			return nil
		}))
	}

	return errors.Join(errs...)
}

// withTimeout runs f with a per-call deadline so one slow request cannot eat
// the budget of the rest.
func (c *Client) withTimeout(f func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	return f(ctx)
}

// syncState fetches the latest status and publishes it so every component sees
// the same view. Only refreshLoop may call it.
func (c *Client) syncState(force bool) {
	if c.ctx.Err() != nil {
		return
	}

	if err := c.fetchAndPublishStatus(force); err != nil {
		c.logger.Warn("failed to sync state", slog.Any("error", err), slog.Bool("force", force))
		if force {
			c.republishLastState()
		}
	}
}

func (c *Client) republishLastState() {
	c.stateMu.RLock()
	last := c.lastEvent
	c.stateMu.RUnlock()

	if last == nil {
		return
	}

	c.logger.Debug(
		"republishing last known state after sync failure",
		slog.Float64("current_temp", last.CurrentTemperature),
		slog.Float64("target_temp", last.TargetTemperature),
	)

	c.bus.PublishStateUpdateForce(c.client, *last)
}

// setUserMode maps our simplified modes to the nefit-go helpers.
// "heat" maps to manual mode, "off" maps to clock/schedule mode.
func (c *Client) setUserMode(ctx context.Context, mode string) error {
	switch mode {
	case modeOff:
		if err := c.nefitClient.SetUserMode(ctx, nefitModeClock); err != nil {
			return fmt.Errorf("failed to set scheduled mode: %w", err)
		}
		return nil
	case modeHeat:
		if err := c.nefitClient.SetUserMode(ctx, nefitModeManual); err != nil {
			return fmt.Errorf("failed to set manual mode: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
}

// publishConnectionStatus publishes a connection status event.
func (c *Client) publishConnectionStatus(status events.ConnectionStatus, errMsg string, reconnects int) {
	event := events.ConnectionStatusEvent{
		Component:  events.SourceNefit,
		Status:     status,
		Error:      errMsg,
		Reconnects: reconnects,
	}
	c.bus.PublishConnectionStatus(c.client, event)
}

// Close gracefully shuts down the Nefit client.
func (c *Client) Close() error {
	c.logger.Info("shutting down nefit client")

	c.cancel()

	if c.nefitClient != nil {
		if err := c.nefitClient.Close(); err != nil {
			c.logger.Warn("error closing nefit client", slog.Any("error", err))
		}
	}

	c.wg.Wait()

	// After the wait, so no status from a stopping goroutine can follow it.
	c.publishConnectionStatus(events.ConnectionStatusDisconnected, "", 0)

	c.logger.Info("nefit client shut down complete")
	return nil
}
