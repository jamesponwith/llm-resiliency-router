package main

import (
	"slices"
	"sync"
	"time"
)

type CellState int

const (
	Healthy CellState = iota
	Degraded
	Ejected
	HalfOpen
)

func (s CellState) String() string {
	return [...]string{"healthy", "degraded", "ejected", "half-open"}[s]
}

type outcome struct {
	at      time.Time
	latency time.Duration
	hard    bool
}

// Cell tracks one upstream's recent outcomes and decides whether it may
// receive traffic. healthy → degraded (p95 breach; log-only in M2, still
// routed) → ejected (eject_after consecutive hard failures) → half-open
// (one probe of real traffic per probe_interval) → healthy on probe success,
// back to ejected on probe failure.
type Cell struct {
	cfg HealthConfig
	now func() time.Time // injectable for tests

	mu          sync.Mutex
	ring        [256]outcome
	n           int // total recorded
	consecutive int // consecutive hard failures
	ejectedAt   time.Time
	probing     bool // a half-open probe is in flight
}

func newCell(cfg HealthConfig) *Cell {
	return &Cell{cfg: cfg, now: time.Now}
}

// Allow reports whether a request may be routed to this cell. When ejected,
// it admits exactly one in-flight probe per probe_interval (half-open).
func (c *Cell) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consecutive < c.cfg.EjectAfter {
		return true
	}
	if !c.probing && c.now().Sub(c.ejectedAt) >= time.Duration(c.cfg.ProbeInterval) {
		c.probing = true
		return true
	}
	return false
}

func (c *Cell) Record(latency time.Duration, hard bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ring[c.n%len(c.ring)] = outcome{at: c.now(), latency: latency, hard: hard}
	c.n++
	c.probing = false
	if !hard {
		// ponytail: one success fully recovers the streak — any straggler
		// success un-ejects too, which is evidence of health, not a bug.
		c.consecutive = 0
		return
	}
	c.consecutive++
	if c.consecutive >= c.cfg.EjectAfter {
		c.ejectedAt = c.now() // (re)start the probe timer
	}
}

func (c *Cell) State() CellState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consecutive >= c.cfg.EjectAfter {
		if c.probing || c.now().Sub(c.ejectedAt) >= time.Duration(c.cfg.ProbeInterval) {
			return HalfOpen
		}
		return Ejected
	}
	if p95 := c.p95(); c.cfg.DegradeP95 > 0 && p95 > time.Duration(c.cfg.DegradeP95) {
		return Degraded
	}
	return Healthy
}

// p95 latency over outcomes inside the window; 0 with fewer than 5 samples.
func (c *Cell) p95() time.Duration {
	cutoff := c.now().Add(-time.Duration(c.cfg.Window))
	var lats []time.Duration
	for i := 0; i < c.n && i < len(c.ring); i++ {
		if o := c.ring[i]; o.at.After(cutoff) {
			lats = append(lats, o.latency)
		}
	}
	if len(lats) < 5 {
		return 0
	}
	slices.Sort(lats)
	return lats[len(lats)*95/100]
}
