package sdk

import (
	"time"
)

type Config struct {
	Debug              bool
	Storage            string
	KeyValConfig       *KeyValCfg
	InstrumentationCfg *InstrumentationCfg
}

type KeyValCfg struct {
	URL           string
	UseSsl        bool
	TLSDomain     string
	KeyPrefix     string
	PipelineSize  int
	MaxConns      int
	IdleConns     int
	ConnsIdleTime time.Duration
}

type InstrumentationCfg struct {
	SentryDSN   string
	TracingRate float64
	Timeout     time.Duration
}

type Client struct {
	config *Config
	logger *StdLogger
}

func Init(cfg *Config) (*Client, error) {
	return &Client{
		config: cfg,
		logger: GetAppLogger(),
	}, nil
}
