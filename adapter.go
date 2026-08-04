package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
)

const chatPath = "/v1/chat/completions"

// chatMeta is the little we need from the client's request to route it.
type chatMeta struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// adapter translates between the OpenAI wire shape the client speaks and a
// provider's native API. passthrough covers every OpenAI-compatible provider;
// anthropicAdapter is the one real translation.
type adapter interface {
	// roundTrip sends the client's request to upstream u, provider-native.
	roundTrip(c *http.Client, r *http.Request, u Upstream, body []byte, meta chatMeta) (*http.Response, error)
	// writeResponse streams resp to the client in OpenAI wire form. A non-nil
	// error means the upstream died mid-response (counts as a hard failure).
	writeResponse(w http.ResponseWriter, resp *http.Response, meta chatMeta) error
}

func adapterFor(u Upstream) adapter {
	if u.Kind == "anthropic" {
		return anthropicAdapter{}
	}
	return passthrough{}
}

type passthrough struct{}

func (passthrough) roundTrip(c *http.Client, r *http.Request, u Upstream, body []byte, meta chatMeta) (*http.Response, error) {
	if r.URL.Path == chatPath && u.Models[meta.Model] != "" {
		body = remapModel(body, u.Models[meta.Model])
	}
	target, _ := url.Parse(u.URL) // validated in loadConfig
	out, err := http.NewRequestWithContext(r.Context(), r.Method, target.JoinPath(r.URL.Path).String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.URL.RawQuery = r.URL.RawQuery
	out.Header = r.Header.Clone()
	if u.APIKeyEnv != "" {
		out.Header.Set("Authorization", "Bearer "+os.Getenv(u.APIKeyEnv))
	}
	return c.Do(out)
}

func (passthrough) writeResponse(w http.ResponseWriter, resp *http.Response, _ chatMeta) error {
	return copyResponse(w, resp)
}

// copyResponse streams resp to w, flushing per chunk so SSE tokens arrive in
// real time. A read error means the upstream died mid-response and is
// returned; a write error means the client went away — not the upstream's
// fault, so it isn't.
func copyResponse(w http.ResponseWriter, resp *http.Response) error {
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	f, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil
			}
			if f != nil {
				f.Flush()
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func remapModel(body []byte, model string) []byte {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	m["model"] = model
	b, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return b
}
