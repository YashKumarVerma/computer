package sdk

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// failureRateCollector emits the current eval-window failure rate spread across
// every registered dimension value, computed on each scrape from the store.
// Computing at scrape time (rather than a background loop writing a gauge)
// keeps the value fresh and needs no ticker.
type failureRateCollector struct {
	c    *Client
	desc *prometheus.Desc
}

func newFailureRateCollector(c *Client) *failureRateCollector {
	return &failureRateCollector{
		c: c,
		desc: prometheus.NewDesc(
			"internal_operation_failure_rate",
			"failure rate over the current eval window, per dimension value",
			[]string{"namespace", "operation", "dimension", "value"}, nil,
		),
	}
}

func (fc *failureRateCollector) Describe(ch chan<- *prometheus.Desc) { ch <- fc.desc }

func (fc *failureRateCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()
	// ponytail: operations are registered at startup before the metric server
	// runs, so iterating state.Operations here needs no lock.
	for ns, ops := range fc.c.state.Operations {
		for _, op := range ops {
			rates, err := op.FailureRates(ctx)
			if err != nil {
				// Store hiccup: skip this op, keep the scrape up. The eval_error
				// / outage paths already make backend trouble visible elsewhere.
				continue
			}
			for dim, vals := range rates {
				for val, rate := range vals {
					ch <- prometheus.MustNewConstMetric(
						fc.desc, prometheus.GaugeValue, rate, ns.Name, op.Name, dim, val,
					)
				}
			}
		}
	}
}

func (c *Client) RunMetricServer() {
	http.Handle("/metrics", promhttp.HandlerFor(c.metricRegistry, promhttp.HandlerOpts{}))
	c.logger.Info("metric server listening on :2112")
	err := http.ListenAndServe(":2112", nil)
	if err != nil {
		return
	}
}
