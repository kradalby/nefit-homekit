package events

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"tailscale.com/util/eventbus"
)

func TestNew(t *testing.T) {
	logger := zap.NewNop()

	bus, err := New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	if bus == nil {
		t.Fatal("New() returned nil bus")
	}

	// Verify all clients were created
	expectedClients := []ClientName{
		ClientNefit,
		ClientHomeKit,
		ClientWeb,
		ClientMetrics,
	}

	for _, name := range expectedClients {
		client, err := bus.Client(name)
		if err != nil {
			t.Errorf("Client(%q) error = %v", name, err)
		}
		if client == nil {
			t.Errorf("Client(%q) returned nil", name)
		}
	}
}

func TestNewWithNilLogger(t *testing.T) {
	bus, err := New(nil)
	if err == nil {
		t.Error("New(nil) expected error, got nil")
		if bus != nil {
			_ = bus.Close()
		}
	}
}

func TestClientNotFound(t *testing.T) {
	logger := zap.NewNop()
	bus, err := New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	_, err = bus.Client("nonexistent")
	if err == nil {
		t.Error("Client(nonexistent) expected error, got nil")
	}
}

func TestPublishAndSubscribe(t *testing.T) {
	logger := zap.NewNop()
	bus, err := New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	publisher, err := bus.Client(ClientNefit)
	if err != nil {
		t.Fatalf("Client(ClientNefit) error = %v", err)
	}

	subscriber, err := bus.Client(ClientHomeKit)
	if err != nil {
		t.Fatalf("Client(ClientHomeKit) error = %v", err)
	}

	// Test StateUpdateEvent
	t.Run("StateUpdateEvent", func(t *testing.T) {
		sub := eventbus.Subscribe[StateUpdateEvent](subscriber)
		defer sub.Close()

		expectedEvent := StateUpdateEvent{
			Timestamp:          time.Now(),
			Source:             "nefit",
			CurrentTemperature: 21.5,
			TargetTemperature:  22.0,
			HeatingActive:      true,
			Mode:               "heat",
		}

		bus.PublishStateUpdate(publisher, expectedEvent)

		select {
		case receivedEvent := <-sub.Events():
			if receivedEvent.Source != expectedEvent.Source {
				t.Errorf("receivedEvent.Source = %v, want %v", receivedEvent.Source, expectedEvent.Source)
			}
			if receivedEvent.CurrentTemperature != expectedEvent.CurrentTemperature {
				t.Errorf("receivedEvent.CurrentTemperature = %v, want %v", receivedEvent.CurrentTemperature, expectedEvent.CurrentTemperature)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for event")
		}
	})

	// Test CommandEvent
	t.Run("CommandEvent", func(t *testing.T) {
		sub := eventbus.Subscribe[CommandEvent](subscriber)
		defer sub.Close()

		temp := 23.0
		expectedEvent := CommandEvent{
			Timestamp:         time.Now(),
			Source:            "homekit",
			CommandType:       CommandTypeSetTemperature,
			TargetTemperature: &temp,
		}

		bus.PublishCommand(publisher, expectedEvent)

		select {
		case receivedEvent := <-sub.Events():
			if receivedEvent.Source != expectedEvent.Source {
				t.Errorf("receivedEvent.Source = %v, want %v", receivedEvent.Source, expectedEvent.Source)
			}
			if receivedEvent.CommandType != expectedEvent.CommandType {
				t.Errorf("receivedEvent.CommandType = %v, want %v", receivedEvent.CommandType, expectedEvent.CommandType)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for event")
		}
	})

	// Test ConnectionStatusEvent
	t.Run("ConnectionStatusEvent", func(t *testing.T) {
		sub := eventbus.Subscribe[ConnectionStatusEvent](subscriber)
		defer sub.Close()

		expectedEvent := ConnectionStatusEvent{
			Timestamp:  time.Now(),
			Component:  "nefit",
			Status:     ConnectionStatusConnected,
			Error:      "",
			Reconnects: 0,
		}

		bus.PublishConnectionStatus(publisher, expectedEvent)

		select {
		case receivedEvent := <-sub.Events():
			if receivedEvent.Component != expectedEvent.Component {
				t.Errorf("receivedEvent.Component = %v, want %v", receivedEvent.Component, expectedEvent.Component)
			}
			if receivedEvent.Status != expectedEvent.Status {
				t.Errorf("receivedEvent.Status = %v, want %v", receivedEvent.Status, expectedEvent.Status)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for event")
		}
	})
}

func TestClose(t *testing.T) {
	logger := zap.NewNop()
	bus, err := New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = bus.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify clients are cleaned up
	bus.mu.RLock()
	clientCount := len(bus.clients)
	bus.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("After Close(), client count = %d, want 0", clientCount)
	}
}

func TestConcurrentPublish(t *testing.T) {
	logger := zap.NewNop()
	bus, err := New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		_ = bus.Close()
	}()

	publisher, err := bus.Client(ClientNefit)
	if err != nil {
		t.Fatalf("Client(ClientNefit) error = %v", err)
	}

	subscriber, err := bus.Client(ClientHomeKit)
	if err != nil {
		t.Fatalf("Client(ClientHomeKit) error = %v", err)
	}

	const numEvents = 100

	sub := eventbus.Subscribe[StateUpdateEvent](subscriber)
	defer sub.Close()

	// Publish events concurrently
	for i := 0; i < numEvents; i++ {
		go func(i int) {
			event := StateUpdateEvent{
				Timestamp:          time.Now(),
				Source:             "nefit",
				CurrentTemperature: float64(i),
				TargetTemperature:  float64(i + 1),
			}
			bus.PublishStateUpdate(publisher, event)
		}(i)
	}

	// Receive all events
	count := 0
	timeout := time.After(5 * time.Second)

	for count < numEvents {
		select {
		case <-sub.Events():
			count++
		case <-timeout:
			t.Fatalf("timeout waiting for %d events, received %d", numEvents, count)
		}
	}

	if count != numEvents {
		t.Errorf("received %d events, want %d", count, numEvents)
	}
}

func TestPublishStateUpdateDeduplication(t *testing.T) {
	logger := zap.NewNop()
	bus, err := New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = bus.Close() }()

	client, err := bus.Client(ClientNefit)
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}

	// Subscribe to state updates
	sub := eventbus.Subscribe[StateUpdateEvent](client)
	defer sub.Close()

	// Publish first event
	event1 := StateUpdateEvent{
		Timestamp:           time.Now(),
		Source:              "nefit",
		CurrentTemperature:  21.5,
		TargetTemperature:   22.0,
		HeatingActive:       true,
		Mode:                "heat",
		Pressure:            1.5,
		HotWaterActive:      true,
		HotWaterTemperature: 55.0,
	}
	bus.PublishStateUpdate(client, event1)

	// Should receive first event
	select {
	case got := <-sub.Events():
		if !got.Equals(event1) {
			t.Error("first event not received correctly")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for first event")
	}

	// Publish duplicate event (same values, different timestamp/source)
	event2 := StateUpdateEvent{
		Timestamp:           time.Now().Add(time.Second),
		Source:              "web",
		CurrentTemperature:  21.5,
		TargetTemperature:   22.0,
		HeatingActive:       true,
		Mode:                "heat",
		Pressure:            1.5,
		HotWaterActive:      true,
		HotWaterTemperature: 55.0,
	}
	bus.PublishStateUpdate(client, event2)

	// Should NOT receive duplicate event
	select {
	case <-sub.Events():
		t.Error("duplicate event should have been filtered")
	case <-time.After(100 * time.Millisecond):
		// Expected - no event received
	}

	// Publish different event (temperature changed)
	event3 := StateUpdateEvent{
		Timestamp:           time.Now().Add(2 * time.Second),
		Source:              "nefit",
		CurrentTemperature:  22.0,
		TargetTemperature:   22.0,
		HeatingActive:       true,
		Mode:                "heat",
		Pressure:            1.5,
		HotWaterActive:      true,
		HotWaterTemperature: 55.0,
	}
	bus.PublishStateUpdate(client, event3)

	// Should receive changed event
	select {
	case got := <-sub.Events():
		if !got.Equals(event3) {
			t.Error("changed event not received correctly")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for changed event")
	}
}
