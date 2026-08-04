// Package chaos is a fake OpenAI-compatible provider with scripted failure
// profiles — shared by the component tests and the live failover demo.
package chaos

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration parses "2s"-style YAML values (yaml.v3 has no native support).
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

type Profile struct {
	FailEvery int      `yaml:"fail_every"` // every Nth request → 500 (0 = never, 1 = always)
	Latency   Duration `yaml:"latency"`    // added before every response
	Hang      bool     `yaml:"hang"`       // accept the request, never respond
	Reply     string   `yaml:"reply"`      // completion text (default canned)
}

func Load(path string) (map[string]Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]Profile
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Handler serves /v1/chat/completions (JSON + SSE) as provider `name`. The
// response's model field is the provider name, so a client can see which fake
// answered — that's the whole failover demo.
// ponytail: fail_every is a deterministic counter, not a random rate — tests
// and demos need reproducibility more than realism.
func Handler(name string, p Profile) http.Handler {
	var n atomic.Int64
	reply := p.Reply
	if reply == "" {
		reply = "Hello from " + name + "."
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Duration(p.Latency))
		if p.Hang {
			<-r.Context().Done()
			return
		}
		if k := n.Add(1); p.FailEvery > 0 && k%int64(p.FailEvery) == 0 {
			http.Error(w, `{"error":{"message":"chaos: injected failure"}}`, http.StatusInternalServerError)
			return
		}
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			f := w.(http.Flusher)
			for _, word := range strings.Fields(reply) {
				chunk := map[string]any{
					"id": "chatcmpl-chaos", "object": "chat.completion.chunk", "model": name,
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": word + " "}}},
				}
				b, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", b)
				f.Flush()
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			f.Flush()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-chaos", "object": "chat.completion", "model": name,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": reply},
				"finish_reason": "stop",
			}},
		})
	})
}
