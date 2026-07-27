package main

import (
	"time"

	"github.com/yashkumarverma/computer/sdk"
)

func main() {
	client, err := sdk.Init(&sdk.Config{
		Storage: "valkey",
		KeyValConfig: &sdk.KeyValCfg{
			URL:           "valkey://localhost:6379/0",
			UseSsl:        false,
			TLSDomain:     "",
			PipelineSize:  5,
			MaxConns:      50,
			IdleConns:     10,
			ConnsIdleTime: 5 * time.Minute,
		},
	})

	_, _ = client, err
}
