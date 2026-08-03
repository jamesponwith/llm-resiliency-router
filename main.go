// llm-resiliency-router: an OpenAI-compatible reverse proxy for LLM upstreams.
// M1: transparent passthrough to a single upstream with per-chunk SSE flush.
// Failover, health model, and provider adapters arrive in M2 (see SPEC.md).
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Upstream struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type Config struct {
	Listen    string     `yaml:"listen"`
	Upstreams []Upstream `yaml:"upstreams"`
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
	if c.Listen == "" {
		c.Listen = ":8484"
	}
	if len(c.Upstreams) == 0 {
		return nil, fmt.Errorf("%s: at least one upstream required", path)
	}
	for _, u := range c.Upstreams {
		p, err := url.Parse(u.URL)
		if err != nil || p.Scheme == "" || p.Host == "" {
			return nil, fmt.Errorf("upstream %q: invalid url %q", u.Name, u.URL)
		}
	}
	return &c, nil
}

// newProxy builds a transparent proxy to one upstream. FlushInterval -1 flushes
// every write immediately — this is what keeps SSE tokens streaming in real time.
func newProxy(u Upstream) *httputil.ReverseProxy {
	target, _ := url.Parse(u.URL) // validated in loadConfig
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
			if u.APIKeyEnv != "" {
				r.Out.Header.Set("Authorization", "Bearer "+os.Getenv(u.APIKeyEnv))
			}
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("upstream error", "upstream", u.Name, "err", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

func handler(c *Config) http.Handler {
	// ponytail: M1 routes everything to the first upstream; the policy engine
	// that picks between upstreams arrives with the health model in M2.
	up := c.Upstreams[0]
	proxy := newProxy(up)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		proxy.ServeHTTP(w, r)
		slog.Info("proxied", "upstream", up.Name, "method", r.Method,
			"path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
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
	slog.Info("listening", "addr", cfg.Listen, "upstream", cfg.Upstreams[0].Name)
	if err := http.ListenAndServe(cfg.Listen, handler(cfg)); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
