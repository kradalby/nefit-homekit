package nefit

import (
	"fmt"
	"log/slog"
	"net"
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
		s, err := nefitserver.New(nefitserver.Config{Device: device, Mode: nefitserver.Mode(cfg.NefitMode), ListenAddress: cfg.NefitXMPPAddr, DeviceIP: net.ParseIP(cfg.NefitDeviceIP), UpstreamAddress: cfg.NefitUpstream, UpdatePolicy: nefitserver.UpdatePolicy(cfg.NefitUpdatePolicy)})
		if err != nil {
			return nil, err
		}
		s.SetLogger(logger)
		return s, nil
	default:
		return nil, fmt.Errorf("unknown Nefit mode %q", cfg.NefitMode)
	}
}
