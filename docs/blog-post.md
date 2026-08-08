# What a $780B payment system taught me about serving LLMs

> **DRAFT** — structure and evidence in place; voice pass pending.
> Target home: jamesponwith.github.io, linked from the repo README.

At Capital One I worked on a resiliency engine that moved payment traffic
between cells — isolated copies of a service — the moment one misbehaved.
$780B/year flowed through decisions like "this cell is lying about being
healthy." When I started routing my own LLM traffic across providers, the
same failure shapes appeared within a week. So I rebuilt the same answers,
small: [llm-resiliency-router](https://github.com/jamesponwith/llm-resiliency-router),
an OpenAI-compatible proxy in ~1,500 lines of mostly-stdlib Go.

Point any OpenAI SDK at it by changing `base_url`. Behind that: three
provider adapters (OpenAI, Ollama passthrough; Anthropic translated to
`/v1/messages` both directions, SSE included), and five ideas carried over
from payments.

## 1. Health is a state machine, not a boolean

Each upstream lives in a *cell*: a ring buffer of request outcomes driving
`healthy → degraded → ejected → half-open → healthy`, with hysteresis so one
blip doesn't eject a provider and one lucky success doesn't un-eject it.
Ejection needs consecutive hard failures; recovery must be *earned* through
a half-open probe. Kill a provider mid-traffic and requests shift before the
client sees an error — the [failover demo](../docs/failover-demo.gif) is the
recording.

## 2. "Up" is not "working" — grade the answers

The failure mode availability checks cannot see: a provider returning
`200 OK` full of garbage — truncating, refusing, hallucinating structure. A
background canary asks every upstream a fixed prompt set (arithmetic, JSON
compliance) and grades the responses. Canary failures feed the *same* health
cells as real traffic, so a lobotomized provider is ejected within one cycle
without ever returning an error — recorded in the
[canary demo](../docs/canary-demo.gif). This is the line between a router
and a load balancer: routing on quality, not liveness.

## 3. Automation earns trust by showing its work

The engine at Capital One shipped in observe mode before it was allowed to
touch traffic. The router does the same: in `learn` mode the full decision
loop runs — would-have-skipped, would-have-failed-over — but traffic never
shifts; every decision lands in `decisions.jsonl` with its inputs. When the
log's judgment matches yours, one config line (`mode: action`) turns it on.
The audit trail keeps existing either way; there is no decision the router
can't explain.

## 4. Tail latency is a race you can enter twice

Some hangs no timeout catches: the provider sends `200` and headers, then
never a first token. With `hedge_after: 1s`, a request with no first token
by the deadline is raced against the next healthy upstream — first token
wins, the loser is cancelled and *not* blamed by the health model (we killed
it; it didn't fail). The [hedging demo](../docs/hedge-demo.gif) shows a
stalled primary answered in 1s instead of never. Off by default: hedging
spends real money.

## 5. Chaos is a first-class artifact

The fake provider (`chaos/`) speaks OpenAI wire format with scripted failure
profiles — `flaky`, `slow`, `hang`, `stall`, `lobotomized` — as YAML. The
same profiles drive the component tests and the recorded demos, so the tests
and the marketing can't drift apart.

## The meta-story

The router was built end-to-end inside a
[personal agentic development flywheel](https://github.com/jamesponwith/agentic-flywheel)
— spec and issue tracking up front, an AI pair with hard local hooks, a PR
gate (lint + tests, ~30s), goreleaser releases, and weekly DORA metrics
committed back to the repo. Eleven PRs, every one gated. That story has its
own writeup.

*Numbers current as of v0.1.0.*
