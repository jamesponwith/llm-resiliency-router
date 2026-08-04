package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// anthropicAdapter translates OpenAI /v1/chat/completions to Anthropic
// /v1/messages and back, for both JSON and SSE responses.
// ponytail: string content only — tool calls and image parts arrive if a
// client ever needs them, not before.
type anthropicAdapter struct{}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiChatRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

type antRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []oaiMessage `json:"messages"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

func toAnthropic(body []byte, u Upstream) ([]byte, error) {
	var req oaiChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parsing chat request: %w", err)
	}
	out := antRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}
	if m := u.Models[req.Model]; m != "" {
		out.Model = m
	}
	if out.MaxTokens == 0 {
		out.MaxTokens = 4096 // anthropic requires max_tokens; openai does not
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			out.System = m.Content
			continue
		}
		out.Messages = append(out.Messages, m)
	}
	return json.Marshal(out)
}

func (anthropicAdapter) roundTrip(c *http.Client, r *http.Request, u Upstream, body []byte, meta chatMeta) (*http.Response, error) {
	if r.Method != http.MethodPost || r.URL.Path != chatPath {
		// ponytail: only chat is translated; /v1/models etc. hit anthropic
		// natively and 404 — synthesize them if a client ever cares.
		return passthrough{}.roundTrip(c, r, u, body, meta)
	}
	ab, err := toAnthropic(body, u)
	if err != nil {
		return nil, err
	}
	target, _ := url.Parse(u.URL) // validated in loadConfig
	out, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target.JoinPath("/v1/messages").String(), bytes.NewReader(ab))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("anthropic-version", "2023-06-01")
	out.Header.Set("x-api-key", os.Getenv(u.APIKeyEnv))
	return c.Do(out)
}

func (anthropicAdapter) writeResponse(w http.ResponseWriter, resp *http.Response, meta chatMeta) error {
	if resp.Request.URL.Path != "/v1/messages" || resp.StatusCode != http.StatusOK {
		// untranslated path, or a 4xx provider error: forward as-is
		return copyResponse(w, resp)
	}
	defer resp.Body.Close()
	if meta.Stream {
		return translateStream(w, resp.Body)
	}
	return translateJSON(w, resp.Body)
}

type antResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func finishReason(stop string) string {
	if stop == "max_tokens" {
		return "length"
	}
	return "stop"
}

func translateJSON(w http.ResponseWriter, body io.Reader) error {
	var ar antResponse
	if err := json.NewDecoder(body).Decode(&ar); err != nil {
		return err
	}
	var text strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	out := map[string]any{
		"id":      ar.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   ar.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": text.String()},
			"finish_reason": finishReason(ar.StopReason),
		}},
		"usage": map[string]any{
			"prompt_tokens":     ar.Usage.InputTokens,
			"completion_tokens": ar.Usage.OutputTokens,
			"total_tokens":      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

// translateStream re-emits Anthropic SSE events as OpenAI
// chat.completion.chunk events. Only data: lines matter — each carries a
// "type" discriminator.
func translateStream(w http.ResponseWriter, body io.Reader) error {
	w.Header().Set("Content-Type", "text/event-stream")
	f, _ := w.(http.Flusher)
	var id, model, stop string
	emit := func(delta map[string]any, finish any) {
		chunk := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(),
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if f != nil {
			f.Flush()
		}
	}
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"message"`
			Delta struct {
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			id, model = ev.Message.ID, ev.Message.Model
			emit(map[string]any{"role": "assistant", "content": ""}, nil)
		case "content_block_delta":
			if ev.Delta.Text != "" {
				emit(map[string]any{"content": ev.Delta.Text}, nil)
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				stop = ev.Delta.StopReason
			}
		case "message_stop":
			emit(map[string]any{}, finishReason(stop))
			fmt.Fprint(w, "data: [DONE]\n\n")
			if f != nil {
				f.Flush()
			}
		}
	}
	return sc.Err()
}
