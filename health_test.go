package main

import (
	"testing"
	"time"
)

func testCell() (*Cell, func(time.Duration)) {
	clock := time.Unix(1000, 0)
	c := newCell(HealthConfig{
		EjectAfter:    3,
		Window:        Duration(60 * time.Second),
		DegradeP95:    Duration(time.Second),
		ProbeInterval: Duration(15 * time.Second),
	})
	c.now = func() time.Time { return clock }
	return c, func(d time.Duration) { clock = clock.Add(d) }
}

func TestCellEjectsAfterConsecutiveFailures(t *testing.T) {
	c, _ := testCell()
	for i := 0; i < 3; i++ {
		if !c.Allow() {
			t.Fatalf("Allow() = false before ejection (i=%d)", i)
		}
		c.Record(time.Millisecond, true)
	}
	if c.Allow() {
		t.Error("Allow() = true after 3 consecutive failures")
	}
	if got := c.State(); got != Ejected {
		t.Errorf("State() = %v, want ejected", got)
	}
}

func TestCellSuccessResetsStreak(t *testing.T) {
	c, _ := testCell()
	for _, hard := range []bool{true, true, false, true, true} {
		c.Record(time.Millisecond, hard)
	}
	if !c.Allow() {
		t.Error("Allow() = false without 3 consecutive failures")
	}
}

func TestCellHalfOpenProbe(t *testing.T) {
	c, advance := testCell()
	for i := 0; i < 3; i++ {
		c.Record(time.Millisecond, true)
	}
	advance(15 * time.Second)
	if got := c.State(); got != HalfOpen {
		t.Errorf("State() = %v, want half-open", got)
	}
	if !c.Allow() {
		t.Fatal("probe not admitted after probe_interval")
	}
	if c.Allow() {
		t.Error("second concurrent probe admitted")
	}
	c.Record(time.Millisecond, false) // probe succeeds
	if !c.Allow() || c.State() != Healthy {
		t.Errorf("cell not healthy after probe success: %v", c.State())
	}
}

func TestCellProbeFailureReEjects(t *testing.T) {
	c, advance := testCell()
	for i := 0; i < 3; i++ {
		c.Record(time.Millisecond, true)
	}
	advance(15 * time.Second)
	c.Allow()
	c.Record(time.Millisecond, true) // probe fails
	if c.Allow() {
		t.Error("Allow() = true right after failed probe")
	}
	advance(15 * time.Second)
	if !c.Allow() {
		t.Error("next probe not admitted after another interval")
	}
}

func TestCellDegradedOnP95(t *testing.T) {
	c, _ := testCell()
	for i := 0; i < 10; i++ {
		c.Record(2*time.Second, false)
	}
	if got := c.State(); got != Degraded {
		t.Errorf("State() = %v, want degraded", got)
	}
	if !c.Allow() {
		t.Error("degraded cell must still receive traffic (log-only in M2)")
	}
}
