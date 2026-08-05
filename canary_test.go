package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesponwith/llm-resiliency-router/chaos"
)

func TestCanaryCheckPass(t *testing.T) {
	tests := []struct {
		name    string
		check   canaryCheck
		content string
		want    bool
	}{
		{"contains hit", canaryCheck{Contains: "42"}, "the answer is 42.", true},
		{"contains miss", canaryCheck{Contains: "42"}, "I cannot help with that", false},
		{"json valid", canaryCheck{JSON: true}, ` {"ok": true} `, true},
		{"json garbage", canaryCheck{JSON: true}, `sure! here is your json: {"ok"`, false},
		{"both required, one fails", canaryCheck{Contains: "ok", JSON: true}, `"not an object but valid json"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.check.pass(tt.content); got != tt.want {
				t.Errorf("pass(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

// Component test: a provider that answers 200 OK with garbage — invisible to
// availability checks — is ejected by the canary within one cycle, and
// traffic lands on the healthy backup.
func TestCanaryEjectsLobotomizedProvider(t *testing.T) {
	lobo := httptest.NewServer(chaos.Handler("lobo", chaos.Profile{Reply: "as an AI I cannot answer"}))
	defer lobo.Close()
	good := httptest.NewServer(chaos.Handler("good", chaos.Profile{Reply: `{"answer": "42", "cat": "tac"}`}))
	defer good.Close()

	prompts := filepath.Join(t.TempDir(), "prompts.yaml")
	src := "- prompt: 7 times 6?\n  contains: \"42\"\n- prompt: json please\n  json: true\n- prompt: cat backwards\n  contains: tac\n"
	if err := os.WriteFile(prompts, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Mode:      "action",
		Upstreams: []Upstream{{Name: "lobo", URL: lobo.URL}, {Name: "good", URL: good.URL}},
		Health:    testHealth(),
		Canary:    CanaryConfig{Interval: Duration(50 * time.Millisecond), Model: "m", Prompts: prompts},
	}
	srv := httptest.NewServer(handler(cfg))
	defer srv.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(b), `"good"`) {
			return // canary ejected lobo; the healthy backup serves
		}
		if time.Now().After(deadline) {
			t.Fatalf("lobotomized provider never ejected; last body: %s", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
