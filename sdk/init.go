package sdk

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/yashkumarverma/computer/sdk/connector/storage"
	"go.uber.org/zap"
)

type Opts struct {
	Debug              bool
	ControlPlane       *CPConfig
	InstrumentationCfg *InstrumentationCfg
	KeyValConnStr      string
}

type CPConfig struct{}

type InstrumentationCfg struct {
	SentryDSN   string
	TracingRate float64
	Timeout     time.Duration
}

type Client struct {
	config         *Opts
	logger         *StdLogger
	state          state
	store          storage.Storage
	metricRegistry *prometheus.Registry
	metricOps      *prometheus.CounterVec
}

func Init(cfg *Opts) (*Client, error) {
	logger := GetAppLogger()
	metricRegistry := prometheus.NewRegistry()

	keyValStore, err := storage.New(&storage.StoreOpts{ConnStr: cfg.KeyValConnStr})
	if err != nil {
		logger.Error("error initializing storage", zap.Error(err))
	}

	return &Client{
		config: cfg,
		logger: GetAppLogger(),
		state: state{
			Operations: make(map[Namespace]map[string]*Operation),
		},
		store:          keyValStore,
		metricRegistry: metricRegistry,
		metricOps: promauto.With(metricRegistry).NewCounterVec(prometheus.CounterOpts{
			Namespace: "internal",
			Subsystem: "operation",
			Name:      "invocation",
			Help:      "number of invoked operations",
		}, []string{"namespace", "operation", "outcome"}),
	}, nil
}

func (c *Client) Close() error {
	GetAppLogger().Info("closing client")
	return nil
}
