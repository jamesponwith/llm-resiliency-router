// The Operate stage's half of the contract (flywheel ADR 0002/0003).
//
// tools/watch probes this; the version it reports is what lets Learn attribute
// an incident to a release. Kept separate from /status, which is a human page:
// a prober needs a stable machine contract that will not change when the
// status page gets a new column.
package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// Version is injected at build time: goreleaser sets -X main.Version.
var Version = "dev"

var started = time.Now()

// healthz reports ok unless every upstream is ejected — a router with no
// reachable upstream is up but useless, and reporting ok would make the SLO
// prober blind to the one failure that matters most.
//
// Always HTTP 200, even when degraded: a 5xx here is indistinguishable from
// the process being down, and the body already carries the detail.
func (rt *router) healthz(w http.ResponseWriter, _ *http.Request) {
	status := "ok"
	if live := rt.liveUpstreams(); live == 0 {
		status = "degraded"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   status,
		"version":  Version,
		"uptime_s": int(time.Since(started).Seconds()),
	})
}

// liveUpstreams counts cells that would currently accept a request.
func (rt *router) liveUpstreams() int {
	n := 0
	for _, c := range rt.cells {
		if c != nil && c.Allow() {
			n++
		}
	}
	return n
}
