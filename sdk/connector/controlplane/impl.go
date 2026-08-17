package controlplane

import (
	"github.com/yashkumarverma/computer/sdk"
	orphanCP "github.com/yashkumarverma/computer/sdk/connector/controlplane/orphan"
	"github.com/yashkumarverma/computer/sdk/connector/storage/valkey"
)

type CP interface {
}

func New(cfg *sdk.Opts) (CP, error) {
	if cfg.ControlPlane == nil {
		return orphanCP.New(cfg)
	}
	return valkey.New(&valkey.ValkeyOpts{ConnStr: cfg.KeyValConnStr})
}
