package events

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"tailscale.com/util/eventbus"
)

// ClientName represents a named eventbus client.
type ClientName string

const (
	// ClientNefit is the Nefit client.
	ClientNefit ClientName = "nefit"

	// ClientHomeKit is the HomeKit client.
	ClientHomeKit ClientName = "homekit"

	// ClientWeb is the Web server client.
	ClientWeb ClientName = "web"

	// ClientMetrics is the metrics client.
	ClientMetrics ClientName = "metrics"
)

// Bus manages the eventbus and named clients.
type Bus struct {
	bus       *eventbus.Bus
	clients   map[ClientName]*eventbus.Client
	mu        sync.RWMutex
	logger    *zap.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	lastState *StateUpdateEvent // For deduplication
	stateMu   sync.Mutex        // Protects lastState
}

// New creates a new eventbus with named clients.
func New(logger *zap.Logger) (*Bus, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	bus := eventbus.New()

	b := &Bus{
		bus:     bus,
		clients: make(map[ClientName]*eventbus.Client),
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Create named clients
	b.createClients()

	logger.Info("eventbus initialized",
		zap.Int("client_count", len(b.clients)),
	)

	return b, nil
}

// createClients creates all named eventbus clients.
func (b *Bus) createClients() {
	clientNames := []ClientName{
		ClientNefit,
		ClientHomeKit,
		ClientWeb,
		ClientMetrics,
	}

	for _, name := range clientNames {
		client := b.bus.Client(string(name))
		b.clients[name] = client
	}
}

// Client returns the eventbus client for the given name.
func (b *Bus) Client(name ClientName) (*eventbus.Client, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	client, ok := b.clients[name]
	if !ok {
		return nil, fmt.Errorf("client %q not found", name)
	}

	return client, nil
}

// PublishStateUpdate publishes a state update event with deduplication.
// If the event is identical to the last published event (ignoring timestamp and source),
// it will be skipped to reduce unnecessary updates.
func (b *Bus) PublishStateUpdate(client *eventbus.Client, event StateUpdateEvent) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()

	// Check if this event is a duplicate of the last published state
	if b.lastState != nil && event.Equals(*b.lastState) {
		b.logger.Debug("skipping duplicate state update event",
			zap.String("source", event.Source),
			zap.Float64("current_temp", event.CurrentTemperature),
			zap.Float64("target_temp", event.TargetTemperature),
		)
		return
	}

	b.logger.Debug("publishing state update event",
		zap.String("source", event.Source),
		zap.Float64("current_temp", event.CurrentTemperature),
		zap.Float64("target_temp", event.TargetTemperature),
	)

	publisher := eventbus.Publish[StateUpdateEvent](client)
	defer publisher.Close()
	publisher.Publish(event)

	// Update last state for future deduplication
	b.lastState = &event
}

// PublishCommand publishes a command event.
func (b *Bus) PublishCommand(client *eventbus.Client, event CommandEvent) {
	b.logger.Debug("publishing command event",
		zap.String("source", event.Source),
		zap.String("command_type", string(event.CommandType)),
	)

	publisher := eventbus.Publish[CommandEvent](client)
	defer publisher.Close()
	publisher.Publish(event)
}

// PublishConnectionStatus publishes a connection status event.
func (b *Bus) PublishConnectionStatus(client *eventbus.Client, event ConnectionStatusEvent) {
	b.logger.Debug("publishing connection status event",
		zap.String("component", event.Component),
		zap.String("status", string(event.Status)),
	)

	publisher := eventbus.Publish[ConnectionStatusEvent](client)
	defer publisher.Close()
	publisher.Publish(event)
}

// Close gracefully shuts down the eventbus.
func (b *Bus) Close() error {
	b.logger.Info("shutting down eventbus")

	b.cancel()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Close all clients
	for name, client := range b.clients {
		client.Close()
		delete(b.clients, name)
	}

	b.logger.Info("eventbus shut down complete")
	return nil
}
