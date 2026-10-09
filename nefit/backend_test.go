package nefit

import (
	"log/slog"
	"testing"

	nefitclient "github.com/kradalby/nefit-go/client"
	nefitserver "github.com/kradalby/nefit-go/server"

	"github.com/kradalby/nefit-homekit/config"
)

func TestBackendModes(t *testing.T) {
	for _, mode := range []string{"cloud", "offline", "both"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{NefitMode: mode, NefitDeviceIP: "127.0.0.1", NefitXMPPAddr: "127.0.0.1:0", NefitUpdatePolicy: "block"}
			b, err := newBackend(cfg, nefitclient.Config{SerialNumber: "123456789", AccessKey: "fixture-key", Password: "fixture-password"}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := b.Close(); err != nil {
					t.Error(err)
				}
			})
			if mode != "cloud" {
				s, ok := b.(*nefitserver.Server)
				if !ok || s.LocalAddress() == nil {
					t.Fatal("embedded backend not selected")
				}
			}
		})
	}
}
