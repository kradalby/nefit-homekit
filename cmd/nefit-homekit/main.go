// Nefit Easy HomeKit Bridge - Main application entry point.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// closeWithLog closes c, logging start and any error. Cleanup is best-effort.
func closeWithLog(logger *slog.Logger, name string, c io.Closer) {
	logger.Info("closing " + name)
	if err := c.Close(); err != nil {
		logger.Warn(
			"failed to close",
			slog.String("component", name),
			slog.Any("error", err),
		)
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

	logger.Info(
		"starting nefit-homekit",
		slog.String("log_level", cfg.LogLevel),
		slog.String("log_format", cfg.LogFormat),
		slog.String("nefit_serial", cfg.NefitSerial),
		slog.String("hap_addr", cfg.HAPAddrPort().String()),
		slog.String("web_addr", cfg.WebAddrPort().String()),
		slog.String("bridge_name", cfg.BridgeName),
	)

	// Initialize EventBus
	logger.Info("initializing eventbus")
	bus, err := events.New(logger)
	if err != nil {
		return fmt.Errorf("failed to create eventbus: %w", err)
	}
	defer closeWithLog(logger, "eventbus", bus)

	// Initialize Nefit client
	logger.Info("initializing nefit client")
	nefitClient, err := nefit.New(cfg, logger, bus)
	if err != nil {
		return fmt.Errorf("failed to create nefit client: %w", err)
	}
	defer closeWithLog(logger, "nefit client", nefitClient)

	// Initialize HomeKit server
	logger.Info("initializing homekit server")
	homekitServer, err := homekit.New(cfg, logger, bus)
	if err != nil {
		return fmt.Errorf("failed to create homekit server: %w", err)
	}
	defer closeWithLog(logger, "homekit server", homekitServer)

	// Initialize Web server
	logger.Info("initializing web server")
	webServer, err := web.New(cfg, logger, bus)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}
	defer closeWithLog(logger, "web server", webServer)

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

	logger.Info(
		"nefit-homekit started successfully",
		slog.String("hap_addr", cfg.HAPAddrPort().String()),
		slog.String("web_addr", cfg.WebAddrPort().String()),
	)
	logger.Info(
		"homekit pairing",
		slog.String("pin", cfg.HAPPin),
		slog.String("instructions", "Use the Home app to add accessory with PIN"),
	)
	logger.Info(
		"web interface",
		slog.String("url", fmt.Sprintf("http://%s", cfg.WebAddrPort().String())),
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info(
		"received shutdown signal",
		slog.String("signal", sig.String()),
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
