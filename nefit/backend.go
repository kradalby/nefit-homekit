package nefit

import (
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	nefitclient "github.com/kradalby/nefit-go/client"
	nefitserver "github.com/kradalby/nefit-go/server"

	"github.com/kradalby/nefit-homekit/config"
)

func newBackend(cfg *config.Config, device nefitclient.Config, logger *slog.Logger) (backend, error) {
	switch cfg.NefitMode {
	case "", "cloud":
		c, err := nefitclient.NewClient(device)
		if err != nil {
			return nil, err
		}
		c.SetLogger(logger)
		return c, nil
	case "offline", "both":
		device.ConnectTimeout = 90 * time.Second
		device.RetryTimeout = 10 * time.Second
		ip, err := netip.ParseAddr(cfg.NefitDeviceIP)
		if err != nil {
			return nil, fmt.Errorf("invalid Nefit device IP: %w", err)
		}
		s, err := nefitserver.New(nefitserver.Config{Device: device, LocalOptions: nefitclient.LocalOptions{
			Mode: nefitclient.ServerMode(cfg.NefitMode), ListenAddress: cfg.NefitXMPPAddr, DeviceIP: ip,
			UpstreamAddress: cfg.NefitUpstream, UpdatePolicy: nefitclient.UpdatePolicy(cfg.NefitUpdatePolicy),
		}})
		if err != nil {
			return nil, err
		}
		s.SetLogger(logger)
		return s, nil
	default:
		return nil, fmt.Errorf("unknown Nefit mode %q", cfg.NefitMode)
	}
}
