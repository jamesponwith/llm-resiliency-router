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

With `hedge_after: 1500ms`, a request whose first token hasn't arrived by
then is raced against the next healthy upstream — first token wins, the
loser is cancelled and not blamed by the health model. Tail-latency
insurance that costs duplicate requests, so it's off by default.

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
