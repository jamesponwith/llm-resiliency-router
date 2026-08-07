# 0003: allow prometheus/client_golang

## Context

The repo is stdlib-first (see SPEC "Tech choices"); new dependencies need an
ADR. M4 needs `/metrics` for the status story: request counts, latencies,
cell states, failovers, hedge and canary outcomes.

## Decision

Add `github.com/prometheus/client_golang`. It is the wire format every
scraper speaks; hand-rolling the exposition format is more code than the
dependency and gets histograms wrong. Already allowed by SPEC alongside
`yaml.v3`.

## Consequences

Two allowed deps total. Metrics live on a dedicated registry (no default
global registry noise). Anything beyond counters/gauges/histograms still
needs its own justification.
