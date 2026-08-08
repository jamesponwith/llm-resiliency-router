package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollect(t *testing.T) {
	// two releases a week apart; v2's earliest commit 48h before publish;
	// two incidents (one closed after 6h, one open) and one PR mislabeled
	// incident that must be excluded.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/releases"):
			io.WriteString(w, `[
			  {"tag_name":"v0.2.0","published_at":"2026-01-08T12:00:00Z"},
			  {"tag_name":"v0.1.0","published_at":"2026-01-01T12:00:00Z"}]`)
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/v0.1.0...v0.2.0"):
			io.WriteString(w, `{"commits":[
			  {"commit":{"author":{"date":"2026-01-06T12:00:00Z"}}},
			  {"commit":{"author":{"date":"2026-01-07T12:00:00Z"}}}]}`)
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/issues"):
			io.WriteString(w, `[
			  {"created_at":"2026-01-02T00:00:00Z","closed_at":"2026-01-02T06:00:00Z"},
			  {"created_at":"2026-01-03T00:00:00Z","closed_at":null},
			  {"created_at":"2026-01-04T00:00:00Z","closed_at":"2026-01-04T01:00:00Z","pull_request":{}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	now := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	m, err := collect(fake.URL, "o/r", now)
	if err != nil {
		t.Fatal(err)
	}

	df := m["deploy_frequency"].(map[string]any)
	if df["releases_total"].(int) != 2 || df["releases_last_30d"].(int) != 2 {
		t.Errorf("deploy_frequency = %+v, want 2 total, 2 in 30d", df)
	}
	if pw := df["per_week"].(float64); pw < 0.99 || pw > 1.01 {
		t.Errorf("per_week = %v, want ~1 (two releases, 7 days apart)", pw)
	}
	lt := m["lead_time_hours"].(map[string]any)
	if lt["samples"].(int) != 1 || lt["median"].(float64) != 48 {
		t.Errorf("lead_time = %+v, want median 48h from 1 sample", lt)
	}
	cf := m["change_failure_rate"].(map[string]any)
	if cf["incidents"].(int) != 2 || cf["rate"].(float64) != 1.0 {
		t.Errorf("cfr = %+v, want 2 incidents (PR excluded), rate 1.0", cf)
	}
	mt := m["mttr_hours"].(map[string]any)
	if mt["samples"].(int) != 1 || mt["median"].(float64) != 6 {
		t.Errorf("mttr = %+v, want median 6h from the 1 closed incident", mt)
	}
}
