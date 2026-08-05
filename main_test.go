package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesponwith/llm-resiliency-router/chaos"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name       string
		yamlSrc    string
		wantErr    bool
		wantListen string
	}{
		{"valid with default listen", "upstreams:\n  - name: x\n    url: http://localhost:1\n", false, ":8484"},
		{"explicit listen", "listen: :9000\nupstreams:\n  - name: x\n    url: http://localhost:1\n", false, ":9000"},
		{"no upstreams", "listen: :9000\n", true, ""},
		{"missing scheme", "upstreams:\n  - name: x\n    url: localhost:1\n", true, ""},
		{"unknown kind", "upstreams:\n  - name: x\n    kind: bard\n", true, ""},
		{"kind implies url", "upstreams:\n  - name: x\n    kind: ollama\n", false, ":8484"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "c.yaml")
			if err := os.WriteFile(p, []byte(tt.yamlSrc), 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := loadConfig(p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadConfig() err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && c.Listen != tt.wantListen {
				t.Errorf("Listen = %q, want %q", c.Listen, tt.wantListen)
			}
		})
	}
}

func TestLoadConfigHealthDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	src := "upstreams:\n  - name: x\n    kind: ollama\nhealth:\n  eject_after: 5\n  probe_interval: 30s\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Health.EjectAfter != 5 {
		t.Errorf("EjectAfter = %d, want explicit 5", c.Health.EjectAfter)
	}
	if time.Duration(c.Health.ProbeInterval) != 30*time.Second {
		t.Errorf("ProbeInterval = %v, want parsed 30s", time.Duration(c.Health.ProbeInterval))
	}
	if time.Duration(c.Health.Timeout) != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s default", time.Duration(c.Health.Timeout))
	}
}

func testHealth() HealthConfig {
	return HealthConfig{
		EjectAfter:    3,
		Window:        Duration(time.Minute),
		ProbeInterval: Duration(time.Hour), // no probes during tests
		Timeout:       Duration(5 * time.Second),
	}
}

// Component test: request passes through untouched, auth header injected from env.
func TestPassthrough(t *testing.T) {
	t.Setenv("TEST_ROUTER_KEY", "sekret")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// echo request facts into headers to avoid shared-state races
		w.Header().Set("Echo-Auth", r.Header.Get("Authorization"))
		w.Header().Set("Echo-Path", r.URL.Path)
		io.WriteString(w, `{"object":"chat.completion"}`)
	}))
	defer fake.Close()

	cfg := &Config{
		Upstreams: []Upstream{{Name: "fake", URL: fake.URL, APIKeyEnv: "TEST_ROUTER_KEY"}},
		Health:    testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := string(body); got != `{"object":"chat.completion"}` {
		t.Errorf("body = %q", got)
	}
	if got := resp.Header.Get("Echo-Auth"); got != "Bearer sekret" {
		t.Errorf("auth = %q, want Bearer sekret", got)
	}
	if got := resp.Header.Get("Echo-Path"); got != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", got)
	}
}

// Component test: SSE chunks reach the client before the upstream finishes the
// response. Fails (times out) if the proxy buffers instead of flushing per chunk.
func TestStreamingFlushPerChunk(t *testing.T) {
	release := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		io.WriteString(w, "data: one\n\n")
		f.Flush()
		<-release // hold the stream open until the client has read chunk one
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer fake.Close()

	cfg := &Config{Upstreams: []Upstream{{Name: "fake", URL: fake.URL}}, Health: testHealth()}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	line, err := r.ReadString('\n') // blocks until timeout if proxy buffers
	if err != nil {
		t.Fatalf("reading first chunk: %v", err)
	}
	if !strings.Contains(line, "one") {
		t.Fatalf("first chunk = %q, want data: one", line)
	}
	close(release)
	rest, _ := io.ReadAll(r)
	if !strings.Contains(string(rest), "[DONE]") {
		t.Fatalf("rest = %q, want [DONE]", rest)
	}
}

// Component test: a dead primary falls through to the backup before the
// client sees anything — the chaos handler reports its name as the model.
func TestFailover(t *testing.T) {
	down := httptest.NewServer(chaos.Handler("primary", chaos.Profile{FailEvery: 1}))
	defer down.Close()
	backup := httptest.NewServer(chaos.Handler("backup", chaos.Profile{}))
	defer backup.Close()

	cfg := &Config{
		Mode:      "action",
		Upstreams: []Upstream{{Name: "primary", URL: down.URL}, {Name: "backup", URL: backup.URL}},
		Health:    testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Model string `json:"model"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || out.Model != "backup" {
			t.Fatalf("req %d: status=%d model=%q err=%v, want 200 from backup", i, resp.StatusCode, out.Model, err)
		}
	}
}

// Component test: after eject_after consecutive failures the primary stops
// receiving traffic entirely (no probes: interval is 1h in testHealth).
func TestEjectionStopsTraffic(t *testing.T) {
	var hits atomic.Int64
	downH := chaos.Handler("primary", chaos.Profile{FailEvery: 1})
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		downH.ServeHTTP(w, r)
	}))
	defer down.Close()
	backup := httptest.NewServer(chaos.Handler("backup", chaos.Profile{}))
	defer backup.Close()

	cfg := &Config{
		Mode:      "action",
		Upstreams: []Upstream{{Name: "primary", URL: down.URL}, {Name: "backup", URL: backup.URL}},
		Health:    testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	for i := 0; i < 10; i++ {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if h := hits.Load(); h != 3 {
		t.Errorf("primary hit %d times, want exactly eject_after (3)", h)
	}
}

// Component test: learn mode runs the decision loop but does not act — the
// first upstream serves everything, failures reach the client, and the
// would-be decisions land in the JSONL log.
func TestLearnModeObservesOnly(t *testing.T) {
	var backupHits atomic.Int64
	down := httptest.NewServer(chaos.Handler("primary", chaos.Profile{FailEvery: 1}))
	defer down.Close()
	backupH := chaos.Handler("backup", chaos.Profile{})
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupHits.Add(1)
		backupH.ServeHTTP(w, r)
	}))
	defer backup.Close()

	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	cfg := &Config{
		Mode:        "learn",
		DecisionLog: logPath,
		Upstreams:   []Upstream{{Name: "primary", URL: down.URL}, {Name: "backup", URL: backup.URL}},
		Health:      testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	for i := 0; i < 4; i++ {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 500 {
			t.Fatalf("req %d: status = %d — learn mode must not hide the failure", i, resp.StatusCode)
		}
	}
	if h := backupHits.Load(); h != 0 {
		t.Errorf("backup hit %d times — learn mode must not shift traffic", h)
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 4 {
		t.Fatalf("decision log has %d lines, want 4", len(lines))
	}
	var first, last decision
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[3]), &last); err != nil {
		t.Fatal(err)
	}
	if first.Mode != "learn" || first.Chose != "primary" {
		t.Errorf("first decision = %+v, want learn/primary", first)
	}
	if !strings.Contains(strings.Join(first.Events, " "), "would_failover_from:primary") {
		t.Errorf("first events = %v, want would_failover_from:primary", first.Events)
	}
	// after 3 hard failures the cell is ejected; the 4th decision records the would-be skip
	if !strings.Contains(strings.Join(last.Events, " "), "would_skip:primary(ejected)") {
		t.Errorf("last events = %v, want would_skip:primary(ejected)", last.Events)
	}
}

// Component test: action mode records the failover it actually performed.
func TestActionModeDecisionLog(t *testing.T) {
	down := httptest.NewServer(chaos.Handler("primary", chaos.Profile{FailEvery: 1}))
	defer down.Close()
	backup := httptest.NewServer(chaos.Handler("backup", chaos.Profile{}))
	defer backup.Close()

	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	cfg := &Config{
		Mode:        "action",
		DecisionLog: logPath,
		Upstreams:   []Upstream{{Name: "primary", URL: down.URL}, {Name: "backup", URL: backup.URL}},
		Health:      testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var dec decision
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &dec); err != nil {
		t.Fatal(err)
	}
	if dec.Mode != "action" || dec.Chose != "backup" || dec.Status != 200 {
		t.Errorf("decision = %+v, want action/backup/200", dec)
	}
	if !strings.Contains(strings.Join(dec.Events, " "), "failover_from:primary") {
		t.Errorf("events = %v, want failover_from:primary", dec.Events)
	}
}

// Component test: streaming works through failover — dead primary, backup streams.
func TestFailoverStreaming(t *testing.T) {
	down := httptest.NewServer(chaos.Handler("primary", chaos.Profile{FailEvery: 1}))
	defer down.Close()
	backup := httptest.NewServer(chaos.Handler("backup", chaos.Profile{Reply: "streamed reply"}))
	defer backup.Close()

	cfg := &Config{
		Mode:      "action",
		Upstreams: []Upstream{{Name: "primary", URL: down.URL}, {Name: "backup", URL: backup.URL}},
		Health:    testHealth(),
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"backup"`) || !strings.Contains(string(body), "data: [DONE]") {
		t.Fatalf("stream = %q, want chunks from backup ending in [DONE]", body)
	}
}
