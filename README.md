# llm-resiliency-router

A Go reverse proxy that sits in front of multiple LLM inference endpoints and
keeps requests flowing through provider outages, latency spikes, and quality
regressions. Point any OpenAI SDK at it by changing `base_url`; failover —
including mid-stream — is the router's problem, not the client's.

Full design: [SPEC.md](SPEC.md). Released: [v0.1.0](https://github.com/jamesponwith/llm-resiliency-router/releases).

```
client (any OpenAI SDK, base_url → the router)
   │  POST /v1/chat/completions (JSON + SSE)          /status  /metrics
   ▼                                                     ▼
┌───────────────────────── router ─────────────────────────────────┐
│ modes: LEARN (observe + log only) / ACTION (shift traffic)       │
│                                                                  │
│ policy: priority failover ──► first allowed cell, next on hard   │
│         hedging (opt-in) ──► no first token by hedge_after?      │
│                              race the next cell, loser cancelled │
│                                                                  │
│ health cell per upstream (ring of outcomes, hysteresis):         │
│   healthy → degraded → ejected → half-open probe → healthy       │
│      ▲ fed by real traffic AND background canary evals           │
│        (fixed prompts, graded: a 200-with-garbage gets ejected)  │
│                                                                  │
│ every decision → decisions.jsonl (audit) + /status + /metrics    │
└───────┬──────────────────┬──────────────────┬────────────────────┘
        ▼                  ▼                  ▼
   anthropic            openai            ollama (local, free
  (translated to    (passthrough)         last-resort failover)
   /v1/messages)
```

## Run

```sh
go run . -config config.yaml   # defaults to ollama on localhost:11434
```

Or grab a release binary: `./deploy.sh` fetches the latest
[GitHub Release](https://github.com/jamesponwith/llm-resiliency-router/releases)
for this platform into `/usr/local/bin` (set `DEPLOY_DIR` to override) and
restarts the `llm-resiliency-router` systemd unit if one is enabled.

## Modes

The router ships in **learn** mode: the decision engine runs — health cells,
would-be skips and failovers — but traffic is never shifted; every decision
lands in `decisions.jsonl` with the inputs behind it. Watch that log until
its judgment matches yours, then set `mode: action` and it starts acting.
Same rollout story as the Capital One resiliency engine this mirrors:
automation earns trust by showing its work first.

## Canary evals

![canary demo: a lobotomized provider answering 200 OK with garbage is ejected on quality within one canary cycle](docs/canary-demo.gif)

Availability checks can't see a provider that answers `200 OK` with garbage —
truncating, refusing, or hallucinating structure. With `canary:` configured,
a background loop sends every upstream a small fixed prompt set
([canary/prompts.yaml](canary/prompts.yaml): arithmetic, JSON compliance)
each interval and grades the answers. Failures feed the same health cells as
real traffic, so a lobotomized provider is ejected within one cycle and a
recovered one earns its way back. This is what separates the router from a
generic load balancer: it routes on *quality*, not just liveness. Run the
demo above yourself: `go build -o bin/router . && go build -o bin/chaos
./cmd/chaos && bash demo/canary-demo.sh`.

## Hedged requests

![hedging demo: a stalled primary sends 200 + headers then nothing; the hedge races the backup after 1s and the client gets an answer in 1s instead of hanging](docs/hedge-demo.gif)

With `hedge_after: 1500ms`, a request whose first token hasn't arrived by
then is raced against the next healthy upstream — first token wins, the
loser is cancelled and not blamed by the health model. Tail-latency
insurance that costs duplicate requests, so it's off by default.

## Telemetry

Three router-local endpoints (everything else is proxied): Prometheus
`/metrics` (requests, latencies, cell states, failovers, hedge and canary
outcomes), `/status.json`, and `/status` — a zero-dependency terminal-styled
page showing live cell states and the last 20 routing decisions.

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

**Why this exists:** [What a $780B payment system taught me about serving
LLMs](docs/blog-post.md) — the resiliency patterns this router borrows from
payments, and where the analogy stops.

Pilot project of the [personal agentic flywheel](https://github.com/jamesponwith/agentic-flywheel) —
built end-to-end through its Intent → Build → Validate → Release → Operate →
Learn loop, from the [flywheel-template](https://github.com/jamesponwith/flywheel-template).
