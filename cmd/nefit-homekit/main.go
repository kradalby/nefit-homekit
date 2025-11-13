// Nefit Easy HomeKit Bridge - Main application entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kradalby/nefit-homekit/config"
	"github.com/kradalby/nefit-homekit/events"
	"github.com/kradalby/nefit-homekit/homekit"
	"github.com/kradalby/nefit-homekit/logging"
	"github.com/kradalby/nefit-homekit/nefit"
	"github.com/kradalby/nefit-homekit/web"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup logger
	logger, err := logging.New(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	logger.Info("starting nefit-homekit",
		zap.String("log_level", cfg.LogLevel),
		zap.String("log_format", cfg.LogFormat),
		zap.String("nefit_serial", cfg.NefitSerial),
		zap.Int("hap_port", cfg.HAPPort),
		zap.Int("web_port", cfg.WebPort),
	)

	// Initialize EventBus
	logger.Info("initializing eventbus")
	bus, err := events.New(logger)
	if err != nil {
		return fmt.Errorf("failed to create eventbus: %w", err)
	}
	defer func() {
		logger.Info("closing eventbus")
		_ = bus.Close()
	}()

	// Initialize Nefit client
	logger.Info("initializing nefit client")
	nefitClient, err := nefit.New(cfg, logger, bus)
	if err != nil {
		return fmt.Errorf("failed to create nefit client: %w", err)
	}
	defer func() {
		logger.Info("closing nefit client")
		_ = nefitClient.Close()
	}()

	// Initialize HomeKit server
	logger.Info("initializing homekit server")
	homekitServer, err := homekit.New(cfg, logger, bus)
	if err != nil {
		return fmt.Errorf("failed to create homekit server: %w", err)
	}
	defer func() {
		logger.Info("closing homekit server")
		_ = homekitServer.Close()
	}()

	// Initialize Web server
	logger.Info("initializing web server")
	webServer, err := web.New(cfg, logger, bus)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}
	defer func() {
		logger.Info("closing web server")
		_ = webServer.Close()
	}()

	// Start all services
	logger.Info("starting services")

	if err := nefitClient.Start(); err != nil {
		return fmt.Errorf("failed to start nefit client: %w", err)
	}

	if err := homekitServer.Start(); err != nil {
		return fmt.Errorf("failed to start homekit server: %w", err)
	}

	if err := webServer.Start(); err != nil {
		return fmt.Errorf("failed to start web server: %w", err)
	}

	logger.Info("nefit-homekit started successfully",
		zap.Int("hap_port", cfg.HAPPort),
		zap.Int("web_port", cfg.WebPort),
	)
	logger.Info("homekit pairing",
		zap.String("pin", cfg.HAPPin),
		zap.String("instructions", "Use the Home app to add accessory with PIN"),
	)
	logger.Info("web interface",
		zap.String("url", fmt.Sprintf("http://localhost:%d", cfg.WebPort)),
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("received shutdown signal",
		zap.String("signal", sig.String()),
	)

	// Graceful shutdown
	logger.Info("shutting down gracefully")

	// Give services time to clean up
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		// Deferred functions will handle cleanup
		close(done)
	}()

	select {
	case <-done:
		logger.Info("shutdown complete")
	case <-ctx.Done():
		logger.Warn("shutdown timeout exceeded, forcing exit")
	}

	return nil
}
