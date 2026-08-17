package sdk

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (c *Client) RunMetricServer() {
	http.Handle("/metrics", promhttp.HandlerFor(c.metricRegistry, promhttp.HandlerOpts{}))
	c.logger.Info("metric server listening on :2112")
	err := http.ListenAndServe(":2112", nil)
	if err != nil {
		return
	}
}
