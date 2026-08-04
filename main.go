// llm-resiliency-router: an OpenAI-compatible reverse proxy for LLM upstreams.
// M2: priority failover — upstreams tried in config order, each behind a
// health cell (ejection + half-open probes). Learn/Action modes, canary
// evals, and hedging arrive in M3 (see SPEC.md).
package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/jamesponwith/llm-resiliency-router/chaos"
	"gopkg.in/yaml.v3"
)

// Duration parses "60s"-style YAML values (yaml.v3 has no native support).
type Duration = chaos.Duration

type Upstream struct {
	Name      string            `yaml:"name"`
	Kind      string            `yaml:"kind"` // openai | anthropic | ollama ("" = openai)
	URL       string            `yaml:"url"`
	APIKeyEnv string            `yaml:"api_key_env"`
	Models    map[string]string `yaml:"models"` // requested-model → provider-model
}

type HealthConfig struct {
	EjectAfter    int      `yaml:"eject_after"`    // consecutive hard failures before ejection
	Window        Duration `yaml:"window"`         // p95 window for the degraded signal
	DegradeP95    Duration `yaml:"degrade_p95"`    // 0 = disabled
	ProbeInterval Duration `yaml:"probe_interval"` // half-open probe cadence while ejected
	Timeout       Duration `yaml:"timeout"`        // upstream response-header timeout
}

type Config struct {
	Listen    string       `yaml:"listen"`
	Upstreams []Upstream   `yaml:"upstreams"` // priority order: first is preferred
	Health    HealthConfig `yaml:"health"`
}

var defaultURLs = map[string]string{
	"openai":    "https://api.openai.com",
	"anthropic": "https://api.anthropic.com",
	"ollama":    "http://localhost:11434",
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Listen = cmp.Or(c.Listen, ":8484")
	c.Health.EjectAfter = cmp.Or(c.Health.EjectAfter, 3)
	c.Health.Window = cmp.Or(c.Health.Window, Duration(60*time.Second))
	c.Health.ProbeInterval = cmp.Or(c.Health.ProbeInterval, Duration(15*time.Second))
	c.Health.Timeout = cmp.Or(c.Health.Timeout, Duration(30*time.Second))
	if len(c.Upstreams) == 0 {
		return nil, fmt.Errorf("%s: at least one upstream required", path)
	}
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		if u.Kind == "" {
			u.Kind = "openai"
		}
		if _, ok := defaultURLs[u.Kind]; !ok {
			return nil, fmt.Errorf("upstream %q: unknown kind %q", u.Name, u.Kind)
		}
		if u.URL == "" {
			u.URL = defaultURLs[u.Kind]
		}
		p, err := url.Parse(u.URL)
		if err != nil || p.Scheme == "" || p.Host == "" {
			return nil, fmt.Errorf("upstream %q: invalid url %q", u.Name, u.URL)
		}
	}
	return &c, nil
}

// handler tries upstreams in config order, skipping ejected cells. A hard
// failure (transport error, timeout, 429, 5xx) before anything is written to
// the client falls through to the next upstream — the client never sees it.
func handler(c *Config) http.Handler {
	cells := make([]*Cell, len(c.Upstreams))
	for i := range c.Upstreams {
		cells[i] = newCell(c.Health)
	}
	client := &http.Client{Transport: &http.Transport{
		ResponseHeaderTimeout: time.Duration(c.Health.Timeout),
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"error":{"message":"reading request body"}}`, http.StatusBadRequest)
			return
		}
		var meta chatMeta
		_ = json.Unmarshal(body, &meta) // non-JSON or empty body: zero meta is fine
		for i := range c.Upstreams {
			u, cell := c.Upstreams[i], cells[i]
			if !cell.Allow() {
				continue
			}
			start := time.Now()
			a := adapterFor(u)
			resp, err := a.roundTrip(client, r, u, body, meta)
			if err != nil || resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
				status := 0
				if resp != nil {
					status = resp.StatusCode
					resp.Body.Close()
				}
				cell.Record(time.Since(start), true)
				slog.Warn("upstream hard failure", "upstream", u.Name, "cell", cell.State().String(),
					"status", status, "err", err)
				continue
			}
			werr := a.writeResponse(w, resp, meta)
			cell.Record(time.Since(start), werr != nil)
			slog.Info("proxied", "upstream", u.Name, "cell", cell.State().String(),
				"method", r.Method, "path", r.URL.Path, "status", resp.StatusCode,
				"dur", time.Since(start).Round(time.Millisecond), "midstream_err", werr)
			return
		}
		http.Error(w, `{"error":{"message":"all upstreams unavailable"}}`, http.StatusBadGateway)
	})
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	slog.Info("listening", "addr", cfg.Listen, "upstreams", len(cfg.Upstreams), "first", cfg.Upstreams[0].Name)
	if err := http.ListenAndServe(cfg.Listen, handler(cfg)); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
