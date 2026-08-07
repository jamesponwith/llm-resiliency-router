package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// decision is one JSONL line: a routing decision and the inputs behind it.
// Events carry the engine's reasoning: skip/failover_from in action mode,
// would_skip/would_failover_from in learn mode, each with the cell state or
// status that drove it.
type decision struct {
	TS     string   `json:"ts"`
	Mode   string   `json:"mode"`
	Path   string   `json:"path"`
	Chose  string   `json:"chose"`
	Status int      `json:"status"`
	DurMS  int64    `json:"dur_ms"`
	Events []string `json:"events,omitempty"`
}

type decisionLog struct {
	mu     sync.Mutex
	enc    *json.Encoder // nil = file logging disabled; the ring still fills
	recent [20]decision  // ring for /status.json
	n      int
}

// newDecisionLog opens path for append; empty path keeps only the in-memory
// ring the status page reads.
func newDecisionLog(path string) *decisionLog {
	d := &decisionLog{}
	if path == "" {
		return d
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("decision log file disabled", "path", path, "err", err)
		return d
	}
	d.enc = json.NewEncoder(f)
	return d
}

func (d *decisionLog) write(dec decision) {
	dec.TS = time.Now().UTC().Format(time.RFC3339Nano)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recent[d.n%len(d.recent)] = dec
	d.n++
	if d.enc == nil {
		return
	}
	if err := d.enc.Encode(dec); err != nil {
		slog.Error("decision log", "err", err)
	}
}

// last returns the ring's decisions, newest first.
func (d *decisionLog) last() []decision {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []decision{}
	for i := d.n - 1; i >= 0 && i > d.n-1-len(d.recent); i-- {
		out = append(out, d.recent[i%len(d.recent)])
	}
	return out
}
