package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metrics is the router's Prometheus surface, on a dedicated registry.
type metrics struct {
	reg         *prometheus.Registry
	requests    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	cellState   *prometheus.GaugeVec
	failovers   *prometheus.CounterVec
	hedgeFired  prometheus.Counter
	hedgeWon    prometheus.Counter
	canaryFails *prometheus.CounterVec
}

func newMetrics() *metrics {
	m := &metrics{
		reg: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "router_requests_total", Help: "Upstream attempts by outcome (ok | hard_fail).",
		}, []string{"upstream", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "router_request_seconds", Help: "Latency of successful upstream responses.",
			Buckets: prometheus.DefBuckets,
		}, []string{"upstream"}),
		cellState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "router_cell_state", Help: "0 healthy, 1 degraded, 2 ejected, 3 half-open.",
		}, []string{"upstream"}),
		failovers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "router_failovers_total", Help: "Requests moved off an upstream after a hard failure.",
		}, []string{"from"}),
		hedgeFired: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "router_hedge_fired_total", Help: "Hedge races started.",
		}),
		hedgeWon: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "router_hedge_won_total", Help: "Hedge races the backup won.",
		}),
		canaryFails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "router_canary_fails_total", Help: "Canary checks failed.",
		}, []string{"upstream"}),
	}
	m.reg.MustRegister(m.requests, m.duration, m.cellState, m.failovers,
		m.hedgeFired, m.hedgeWon, m.canaryFails)
	return m
}

// metricsHandler refreshes the cell-state gauges at scrape time, then serves
// the registry.
func (rt *router) metricsHandler() http.Handler {
	promh := promhttp.HandlerFor(rt.m.reg, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i, u := range rt.cfg.Upstreams {
			rt.m.cellState.WithLabelValues(u.Name).Set(float64(rt.cells[i].State()))
		}
		promh.ServeHTTP(w, r)
	})
}
