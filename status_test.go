package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesponwith/llm-resiliency-router/chaos"
)

// Component test: after one proxied request, /metrics exposes it and
// /status.json reports cell state plus the decision that was made.
func TestTelemetryEndpoints(t *testing.T) {
	fake := httptest.NewServer(chaos.Handler("primary", chaos.Profile{}))
	defer fake.Close()

	cfg := &Config{
		Mode:      "action",
		Upstreams: []Upstream{{Name: "primary", Kind: "openai", URL: fake.URL}},
		Health:    testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	mresp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer mresp.Body.Close()
	mb, _ := io.ReadAll(mresp.Body)
	for _, want := range []string{
		`router_requests_total{outcome="ok",upstream="primary"} 1`,
		`router_cell_state{upstream="primary"} 0`,
		`router_request_seconds_count{upstream="primary"} 1`,
	} {
		if !strings.Contains(string(mb), want) {
			t.Errorf("/metrics missing %q", want)
		}
	}

	sresp, err := http.Get(srv.URL + "/status.json")
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	var st struct {
		Mode      string `json:"mode"`
		Upstreams []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"upstreams"`
		Decisions []decision `json:"decisions"`
	}
	if err := json.NewDecoder(sresp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Mode != "action" || len(st.Upstreams) != 1 || st.Upstreams[0].State != "healthy" {
		t.Errorf("status = %+v, want action mode, healthy primary", st)
	}
	if len(st.Decisions) != 1 || st.Decisions[0].Chose != "primary" {
		t.Errorf("decisions = %+v, want the one proxied request", st.Decisions)
	}

	hresp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer hresp.Body.Close()
	hb, _ := io.ReadAll(hresp.Body)
	if hresp.StatusCode != 200 || !strings.Contains(string(hb), "<title>llm-resiliency-router</title>") {
		t.Errorf("/status = %d, want the status page", hresp.StatusCode)
	}
}

func TestDecisionRingNewestFirst(t *testing.T) {
	d := newDecisionLog("")
	for i := 0; i < 25; i++ { // wrap the 20-slot ring
		d.write(decision{Status: i})
	}
	last := d.last()
	if len(last) != 20 {
		t.Fatalf("len = %d, want 20", len(last))
	}
	if last[0].Status != 24 || last[19].Status != 5 {
		t.Errorf("ring order wrong: first=%d last=%d, want 24 and 5", last[0].Status, last[19].Status)
	}
}
