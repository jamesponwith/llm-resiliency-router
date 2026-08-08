// dora: DORA-lite metrics from the GitHub API, no dependencies.
//   - deploy frequency: published releases (total, last 30d, per week)
//   - lead time: earliest commit in a release → its publish time, median
//   - change-failure rate: `incident`-labeled issues / releases
//   - mttr: incident open → close, median
//
// Run weekly by learn.yml, which commits docs/dora.json back to the repo —
// the git history of that file IS the trend line.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"
)

type release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
}

type issue struct {
	CreatedAt   time.Time  `json:"created_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	PullRequest *struct{}  `json:"pull_request"` // set when the "issue" is a PR — excluded
}

type compare struct {
	Commits []struct {
		Commit struct {
			Author struct {
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	} `json:"commits"`
}

func gh(api, path string, v any) error {
	req, err := http.NewRequest("GET", api+path, nil)
	if err != nil {
		return err
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func medianHours(ds []time.Duration) float64 {
	if len(ds) == 0 {
		return 0
	}
	slices.Sort(ds)
	return ds[len(ds)/2].Hours()
}

func collect(api, repo string, now time.Time) (map[string]any, error) {
	var releases []release
	if err := gh(api, "/repos/"+repo+"/releases?per_page=20", &releases); err != nil {
		return nil, err
	}
	slices.SortFunc(releases, func(a, b release) int { return a.PublishedAt.Compare(b.PublishedAt) })

	last30 := 0
	for _, r := range releases {
		if now.Sub(r.PublishedAt) <= 30*24*time.Hour {
			last30++
		}
	}
	perWeek := 0.0
	if len(releases) > 1 {
		span := releases[len(releases)-1].PublishedAt.Sub(releases[0].PublishedAt)
		perWeek = float64(len(releases)-1) / (span.Hours() / (24 * 7))
	}

	// lead time: earliest commit between the previous release and this one
	var leads []time.Duration
	for i := 1; i < len(releases); i++ {
		var cmp compare
		if err := gh(api, "/repos/"+repo+"/compare/"+releases[i-1].TagName+"..."+releases[i].TagName, &cmp); err != nil {
			return nil, err
		}
		var earliest time.Time
		for _, c := range cmp.Commits {
			if earliest.IsZero() || c.Commit.Author.Date.Before(earliest) {
				earliest = c.Commit.Author.Date
			}
		}
		if !earliest.IsZero() {
			leads = append(leads, releases[i].PublishedAt.Sub(earliest))
		}
	}

	var incidents []issue
	if err := gh(api, "/repos/"+repo+"/issues?labels=incident&state=all&per_page=100", &incidents); err != nil {
		return nil, err
	}
	incidents = slices.DeleteFunc(incidents, func(i issue) bool { return i.PullRequest != nil })
	var repairs []time.Duration
	for _, i := range incidents {
		if i.ClosedAt != nil {
			repairs = append(repairs, i.ClosedAt.Sub(i.CreatedAt))
		}
	}
	cfr := 0.0
	if len(releases) > 0 {
		cfr = float64(len(incidents)) / float64(len(releases))
	}

	return map[string]any{
		"repo":         repo,
		"generated_at": now.UTC().Format(time.RFC3339),
		"deploy_frequency": map[string]any{
			"releases_total": len(releases), "releases_last_30d": last30, "per_week": perWeek,
		},
		"lead_time_hours":     map[string]any{"median": medianHours(leads), "samples": len(leads)},
		"change_failure_rate": map[string]any{"incidents": len(incidents), "releases": len(releases), "rate": cfr},
		"mttr_hours":          map[string]any{"median": medianHours(repairs), "samples": len(repairs)},
	}, nil
}

func main() {
	repo := flag.String("repo", "", "owner/name (required)")
	out := flag.String("out", "docs/dora.json", "output path")
	api := flag.String("api", "https://api.github.com", "GitHub API base")
	flag.Parse()
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "-repo required")
		os.Exit(2)
	}
	m, err := collect(*api, *repo, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)
}
