// chaos runs a fake OpenAI-compatible provider for the failover demo:
// start two, point the router at both, kill one, watch traffic shift.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/jamesponwith/llm-resiliency-router/chaos"
)

func main() {
	listen := flag.String("listen", ":9001", "listen address")
	name := flag.String("name", "fake", "provider name (returned as the model field)")
	profile := flag.String("profile", "healthy", "profile name from -profiles")
	profiles := flag.String("profiles", "chaos/profiles.yaml", "profiles file")
	flag.Parse()
	m, err := chaos.Load(*profiles)
	if err != nil {
		slog.Error("profiles", "err", err)
		os.Exit(1)
	}
	p, ok := m[*profile]
	if !ok {
		slog.Error("unknown profile", "profile", *profile)
		os.Exit(1)
	}
	slog.Info("chaos provider up", "name", *name, "listen", *listen, "profile", *profile)
	if err := http.ListenAndServe(*listen, chaos.Handler(*name, p)); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
