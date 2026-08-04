package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToAnthropic(t *testing.T) {
	u := Upstream{Models: map[string]string{"gpt-4o": "claude-sonnet-5"}}
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}],"stream":true}`)
	out, err := toAnthropic(body, u)
	if err != nil {
		t.Fatal(err)
	}
	var got antRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-sonnet-5" {
		t.Errorf("model = %q, want mapped claude-sonnet-5", got.Model)
	}
	if got.System != "be brief" {
		t.Errorf("system = %q, want extracted from messages", got.System)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Errorf("messages = %+v, want just the user turn", got.Messages)
	}
	if got.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want 4096 default", got.MaxTokens)
	}
	if !got.Stream {
		t.Error("stream not carried over")
	}
}

func TestTranslateJSON(t *testing.T) {
	in := `{"id":"msg_1","model":"claude-sonnet-5","content":[{"type":"text","text":"Hello"}],"stop_reason":"max_tokens","usage":{"input_tokens":5,"output_tokens":7}}`
	w := httptest.NewRecorder()
	if err := translateJSON(w, strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message      map[string]string `json:"message"`
			FinishReason string            `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Object != "chat.completion" || got.Model != "claude-sonnet-5" {
		t.Errorf("object/model = %q/%q", got.Object, got.Model)
	}
	if got.Choices[0].Message["content"] != "Hello" {
		t.Errorf("content = %q, want Hello", got.Choices[0].Message["content"])
	}
	if got.Choices[0].FinishReason != "length" {
		t.Errorf("finish_reason = %q, want length (from max_tokens)", got.Choices[0].FinishReason)
	}
	if got.Usage["total_tokens"] != 12 {
		t.Errorf("total_tokens = %d, want 12", got.Usage["total_tokens"])
	}
}

func TestTranslateStream(t *testing.T) {
	in := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-5"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	w := httptest.NewRecorder()
	if err := translateStream(w, strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	var content, finish string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta        map[string]string `json:"delta"`
				FinishReason string            `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("bad chunk %q: %v", data, err)
		}
		if chunk.Model != "claude-sonnet-5" {
			t.Errorf("chunk model = %q", chunk.Model)
		}
		content += chunk.Choices[0].Delta["content"]
		if fr := chunk.Choices[0].FinishReason; fr != "" {
			finish = fr
		}
	}
	if content != "Hello" {
		t.Errorf("assembled content = %q, want Hello", content)
	}
	if finish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finish)
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Error("missing [DONE] terminator")
	}
}
