package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type CanaryConfig struct {
	Interval Duration `yaml:"interval"` // 0 = disabled
	Model    string   `yaml:"model"`    // requested-model name; per-upstream models mapping applies
	Prompts  string   `yaml:"prompts"`  // YAML list of checks
}

// canaryCheck is one fixed prompt with a cheap objective pass condition.
type canaryCheck struct {
	Prompt   string `yaml:"prompt"`
	Contains string `yaml:"contains"` // pass requires the response to contain this
	JSON     bool   `yaml:"json"`     // pass requires the response to parse as JSON
}

func (ck canaryCheck) pass(content string) bool {
	if ck.Contains != "" && !strings.Contains(content, ck.Contains) {
		return false
	}
	if ck.JSON && !json.Valid([]byte(strings.TrimSpace(content))) {
		return false
	}
	return true
}

func loadChecks(path string) ([]canaryCheck, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var checks []canaryCheck
	if err := yaml.Unmarshal(b, &checks); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("%s: no canary checks", path)
	}
	return checks, nil
}

// canaryLoop probes every upstream with the fixed prompt set each interval,
// starting immediately. Results feed the same cells as real traffic: a
// provider that answers 200s full of garbage racks up consecutive hard
// failures and is ejected by the ordinary state machine within one cycle
// (len(checks) >= eject_after), and a recovered provider earns its way back
// the same way. Runs in learn mode too — canaries are the only health signal
// for upstreams learn mode never routes to.
// ponytail: live-traffic successes interleaving with a canary cycle can reset
// the failure streak; weighting quality vs availability separately is future
// work if flapping shows up in the decision log.
func (rt *router) canaryLoop() {
	t := time.NewTicker(time.Duration(rt.cfg.Canary.Interval))
	for ; ; <-t.C {
		for i := range rt.cfg.Upstreams {
			rt.canaryOne(rt.cfg.Upstreams[i], rt.cells[i])
		}
	}
}

func (rt *router) canaryOne(u Upstream, cell *Cell) {
	start := time.Now()
	var events []string
	for n, ck := range rt.checks {
		askStart := time.Now()
		content, err := rt.canaryAsk(u, ck.Prompt)
		switch {
		case err != nil:
			events = append(events, fmt.Sprintf("canary_fail:#%d(%v)", n, err))
			cell.Record(time.Since(askStart), true)
		case !ck.pass(content):
			events = append(events, fmt.Sprintf("canary_fail:#%d(bad content %.40q)", n, content))
			cell.Record(time.Since(askStart), true)
		default:
			cell.Record(time.Since(askStart), false)
		}
	}
	slog.Info("canary", "upstream", u.Name, "cell", cell.State().String(),
		"failed", len(events), "of", len(rt.checks))
	rt.dlog.write(decision{Mode: rt.cfg.Mode, Path: "canary", Chose: u.Name,
		DurMS:  time.Since(start).Milliseconds(),
		Events: append([]string{fmt.Sprintf("canary:%d/%d_ok", len(rt.checks)-len(events), len(rt.checks))}, events...)})
}

func (rt *router) canaryAsk(u Upstream, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    rt.cfg.Canary.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(rt.cfg.Health.Timeout))
	defer cancel()
	r := httptest.NewRequest("POST", chatPath, bytes.NewReader(body)).WithContext(ctx)
	a := adapterFor(u)
	meta := chatMeta{Model: rt.cfg.Canary.Model}
	resp, err := a.roundTrip(rt.client, r, u, body, meta)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	// ponytail: reuse the adapter's client-facing translation via a recorder
	// instead of duplicating anthropic response parsing here.
	rec := httptest.NewRecorder()
	if err := a.writeResponse(rec, resp, meta); err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("unparseable canary response")
	}
	return out.Choices[0].Message.Content, nil
}
