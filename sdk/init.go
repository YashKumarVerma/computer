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
	metricEvalErr  *prometheus.CounterVec
}

func Init(cfg *Opts) (*Client, error) {
	logger := GetAppLogger()
	metricRegistry := prometheus.NewRegistry()

	keyValStore, err := storage.New(&storage.StoreOpts{ConnStr: cfg.KeyValConnStr})
	if err != nil {
		logger.Error("error initializing storage", zap.Error(err))
		return nil, err
	}

	c := &Client{
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
		// Design doc §7.4: eval errors are emitted independently of the
		// success/fail stream so a broken predicate (which reads as 100%
		// success) stays visible instead of silently sleeping the breaker.
		metricEvalErr: promauto.With(metricRegistry).NewCounterVec(prometheus.CounterOpts{
			Namespace: "internal",
			Subsystem: "operation",
			Name:      "eval_error",
			Help:      "predicate errors recovered and treated as success",
		}, []string{"namespace", "operation"}),
	}
	// Failure rate is a derived value, so it's a scrape-time collector rather
	// than a counter incremented on the write path.
	metricRegistry.MustRegister(newFailureRateCollector(c))
	return c, nil
}

func (c *Client) Close() {
	GetAppLogger().Info("closing client")
}
