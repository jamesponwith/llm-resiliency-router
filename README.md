# llm-resiliency-router

A Go reverse proxy that sits in front of multiple LLM inference endpoints and
keeps requests flowing through provider outages, latency spikes, and quality
regressions. Point any OpenAI SDK at it by changing `base_url`; failover —
including mid-stream — is the router's problem, not the client's.

Full design: [SPEC.md](SPEC.md). Status: M2 — priority failover with health
cells (ejection + half-open probes), three provider kinds (`openai`, `ollama`
passthrough; `anthropic` translated to `/v1/messages`).

## Run

```sh
go run . -config config.yaml   # defaults to ollama on localhost:11434
```

## Failover demo

![failover demo: primary killed mid-traffic, requests shift to backup with zero client errors, half-open probe recovers primary](docs/failover-demo.gif)

Recorded with [phux](https://github.com/phall1/phux)'s built-in recorder from
`demo/failover-demo.sh`. To run it live — two fake providers (the chaos
harness), the router in front, kill one:

```sh
go run ./cmd/chaos -name primary -listen :9001   # terminal 1
go run ./cmd/chaos -name backup  -listen :9002   # terminal 2
go run . -config config.demo.yaml                # terminal 3
while true; do curl -s localhost:8484/v1/chat/completions \
  -d '{"model":"m"}' | jq -r .model; sleep 0.5; done   # terminal 4
```

Ctrl-C the primary: after 3 failed requests it's ejected and the model field
flips to `backup` — no client-visible errors. Restart it: a half-open probe
brings it back within 15s. Profiles in `chaos/profiles.yaml` (`flaky`, `slow`,
`hang`, `down`) script other failure modes for tests and demos.

Pilot project of the [personal agentic flywheel](https://github.com/jamesponwith/agentic-flywheel) —
built end-to-end through its Intent → Build → Validate → Release → Learn loop,
from the [flywheel-template](https://github.com/jamesponwith/flywheel-template).
