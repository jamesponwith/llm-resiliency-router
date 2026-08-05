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
	mu  sync.Mutex
	enc *json.Encoder
}

// newDecisionLog opens path for append. Empty path disables logging — the
// nil *decisionLog it returns is safe to call.
func newDecisionLog(path string) *decisionLog {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("decision log disabled", "path", path, "err", err)
		return nil
	}
	return &decisionLog{enc: json.NewEncoder(f)}
}

func (d *decisionLog) write(dec decision) {
	if d == nil {
		return
	}
	dec.TS = time.Now().UTC().Format(time.RFC3339Nano)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.enc.Encode(dec); err != nil {
		slog.Error("decision log", "err", err)
	}
}
