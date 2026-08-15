# What a $780B payment system taught me about serving LLMs

> Target home: jamesponwith.github.io, linked from the repo README.

At Capital One I worked on a resiliency engine that moved payment traffic
between cells the moment one misbehaved. $780B a year flowed through
decisions like "this cell is lying about being healthy." When I started
routing my own LLM traffic across providers, the same failure shapes showed
up within a week.

So I rebuilt the same answers, small:
[llm-resiliency-router](https://github.com/jamesponwith/llm-resiliency-router),
an OpenAI-compatible proxy in ~1,500 lines of mostly-stdlib Go. Point any
SDK at it by changing `base_url`. Five ideas carried over from payments.

## 1. Health is a state machine, not a boolean

Each upstream lives in a cell: a ring buffer of outcomes driving
`healthy → degraded → ejected → half-open → healthy`, with hysteresis both
ways. Ejection takes consecutive hard failures; recovery must be *earned*
through a probe. Kill a provider mid-traffic and requests shift before any
client sees an error — the [failover demo](../docs/failover-demo.gif) is
eight seconds long and shows the whole thing.

## 2. "Up" is not "working"

The failure availability checks cannot see: a provider returning `200 OK`
full of garbage. A background canary asks every upstream a fixed prompt set
— arithmetic, JSON compliance — and grades the answers. Canary failures
feed the *same* health cells as real traffic, so a lobotomized provider is
ejected within one cycle without ever returning an error
([recorded](../docs/canary-demo.gif)). This is the line between a router
and a load balancer: routing on quality, not liveness.

## 3. Automation earns trust by showing its work

The C1 engine shipped in observe mode before it touched traffic. The router
does the same. In `learn` mode the full decision loop runs — would have
skipped, would have failed over — but traffic never shifts, and every
decision lands in `decisions.jsonl` with its inputs. When the log's judgment
matches yours, one config line turns it on. There is no decision the router
can't explain.

## 4. Tail latency is a race you can enter twice

Some hangs no timeout catches: `200`, headers, then never a first token.
With `hedge_after: 1s`, a request with no first token by the deadline races
the next healthy upstream — first token wins, the loser is cancelled and
*not* blamed by the health model. We killed it; it didn't fail. The
[hedging demo](../docs/hedge-demo.gif) shows a stalled primary answered in
one second instead of never. Off by default: hedging spends money.

## 5. Chaos is a first-class artifact

The fake provider speaks OpenAI wire format with scripted failure profiles —
`flaky`, `slow`, `hang`, `stall`, `lobotomized` — as YAML. The same profiles
drive the component tests and the recorded demos, so the tests and the
marketing can't drift apart.

---

The router was built end to end inside a
[personal agentic development flywheel](https://github.com/jamesponwith/agentic-flywheel):
spec first, an AI pair with hard local hooks, a 30-second PR gate, goreleaser
releases, weekly public DORA metrics. Eleven PRs, every one gated. That
story has its own writeup.

*Numbers current as of v0.1.0.*
