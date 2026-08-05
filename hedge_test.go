package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesponwith/llm-resiliency-router/chaos"
)

func postModel(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, out.Model
}

// Component test: primary answers 200 + headers but never a first token; the
// hedge fires after hedge_after and the backup's response wins.
func TestHedgeFiresOnStalledPrimary(t *testing.T) {
	stalled := httptest.NewServer(chaos.Handler("primary", chaos.Profile{Stall: true}))
	defer stalled.Close()
	backup := httptest.NewServer(chaos.Handler("backup", chaos.Profile{}))
	defer backup.Close()

	cfg := &Config{
		Mode:       "action",
		HedgeAfter: Duration(30 * time.Millisecond),
		Upstreams:  []Upstream{{Name: "primary", URL: stalled.URL}, {Name: "backup", URL: backup.URL}},
		Health:     testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	start := time.Now()
	status, model := postModel(t, srv.URL)
	if status != 200 || model != "backup" {
		t.Fatalf("status=%d model=%q, want 200 from backup", status, model)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("hedged response took %v — hedge did not race", d)
	}
}

// Component test: a fast primary means the hedge timer never fires and the
// backup sees zero traffic — hedging must not spend money when it isn't needed.
func TestHedgeIdleWhenPrimaryFast(t *testing.T) {
	primary := httptest.NewServer(chaos.Handler("primary", chaos.Profile{}))
	defer primary.Close()
	var backupHits atomic.Int64
	backupH := chaos.Handler("backup", chaos.Profile{})
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupHits.Add(1)
		backupH.ServeHTTP(w, r)
	}))
	defer backup.Close()

	cfg := &Config{
		Mode:       "action",
		HedgeAfter: Duration(300 * time.Millisecond),
		Upstreams:  []Upstream{{Name: "primary", URL: primary.URL}, {Name: "backup", URL: backup.URL}},
		Health:     testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		if status, model := postModel(t, srv.URL); status != 200 || model != "primary" {
			t.Fatalf("req %d: status=%d model=%q, want 200 from primary", i, status, model)
		}
	}
	time.Sleep(50 * time.Millisecond) // let any stray hedge goroutine land
	if h := backupHits.Load(); h != 0 {
		t.Errorf("backup hit %d times — hedge fired for a fast primary", h)
	}
}

// Component test: when the hedged pair both hard-fail, the loop falls
// through to the remaining upstreams.
func TestHedgePairFailureFallsThrough(t *testing.T) {
	down1 := httptest.NewServer(chaos.Handler("down1", chaos.Profile{FailEvery: 1}))
	defer down1.Close()
	down2 := httptest.NewServer(chaos.Handler("down2", chaos.Profile{FailEvery: 1}))
	defer down2.Close()
	third := httptest.NewServer(chaos.Handler("third", chaos.Profile{}))
	defer third.Close()

	cfg := &Config{
		Mode:       "action",
		HedgeAfter: Duration(20 * time.Millisecond),
		Upstreams: []Upstream{
			{Name: "down1", URL: down1.URL},
			{Name: "down2", URL: down2.URL},
			{Name: "third", URL: third.URL},
		},
		Health: testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	if status, model := postModel(t, srv.URL); status != 200 || model != "third" {
		t.Fatalf("status=%d model=%q, want 200 from third", status, model)
	}
}
