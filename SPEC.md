# LLM Resiliency Router — Spec

**One-liner:** A Go reverse proxy that sits in front of multiple LLM inference endpoints and keeps
requests flowing through provider outages, latency spikes, and quality regressions — the Capital One
resiliency engine pattern applied to AI serving.

**Why this project:** It's the C1 Resiliency Engine story ($780B, MTTR ↓95%, cell-based routing)
retold in territory a DeepMind reviewer lives in: reliable model serving. Almost no applicant has
production traffic-shifting experience; this makes that experience legible in an open repo.

## Goals

1. OpenAI-compatible proxy: point any client at it by changing `base_url`, nothing else.
2. Automatic failover between providers in under 1 second from failure detection, including
   mid-conversation (streaming reconnect on a fresh upstream).
3. Health decisions based on *quality* as well as availability (canary evals, not just 5xx counts).
4. Learn/Action modes — observe-and-log vs. actually-shift-traffic — mirroring the C1 engine's
   safe-automation design.
5. A demo that sells itself: kill a provider mid-stream on video, watch the conversation continue.

## Non-goals (v1)

- Auth, multi-user, rate limiting, billing — it runs in a trusted personal environment.
- Response caching, semantic caching.
- Kubernetes operator / horizontal scale — single binary, single host.
- Supporting every provider — three adapters prove the abstraction.

## Architecture

```
client (any OpenAI SDK)
   │  POST /v1/chat/completions (+ SSE streaming)
   ▼
┌──────────────── router ────────────────┐
│  ingress: OpenAI-compatible HTTP API   │
│  policy engine: pick upstream(s)       │
│    - priority failover                 │
│    - latency-aware weighting           │
│    - hedged requests (optional)        │
│  health model (per upstream "cell")    │
│    - rolling p50/p95 latency           │
│    - error/timeout rates               │
│    - canary eval scores                │
│    - state: healthy → degraded →       │
│      ejected → half-open probe         │
│  modes: LEARN (log only) / ACTION      │
└───────┬──────────┬──────────┬──────────┘
        ▼          ▼          ▼
   anthropic     openai     ollama (local, free failover of last resort)
```

### Components

- **Ingress** — `net/http` stdlib. Implements `/v1/chat/completions` (streaming + non-streaming)
  and `/v1/models`. OpenAI's API shape is the lingua franca; implementing it means zero client work.
- **Adapters** — one per provider, translating the OpenAI request/response shape and re-emitting SSE
  chunks. Interface: `type Upstream interface { Chat(ctx, req) (Stream, error); Name() string }`.
  Providers v1: Anthropic, OpenAI, Ollama.
- **Health model** — per-upstream ring buffer of request outcomes (latency, status, timeout).
  Cell states with hysteresis: `healthy → degraded` (soft signal: p95 breach, elevated errors) →
  `ejected` (hard signal: consecutive failures) → `half-open` (probe with real or synthetic traffic)
  → `healthy`. Thresholds in config, not code.
- **Canary evals** — a background loop sends a small fixed prompt set (exact-match / contains
  checks, e.g. arithmetic, JSON-format compliance) to every upstream every N minutes. A provider
  that is "up" but degraded (truncating, refusing, garbling) gets marked degraded. This is the
  feature that separates the project from a generic load balancer.
- **Policy engine** — v1 policies: `priority` (ordered failover), `latency` (weighted by rolling
  p95). Optional per-request hedging: fire to backup if no first token within `hedge_after`
  (config), first token wins, loser cancelled. Hedging spends money — off by default, per-route
  opt-in.
- **Modes** — global `mode: learn | action`. Learn mode runs the full decision loop and logs/records
  what it *would* have done. Ship Learn first; flip to Action when the decisions look right.
  (Same rollout story as the C1 engine — say so in the README.)
- **Telemetry** — Prometheus `/metrics` (request counts, latencies, cell states, failovers,
  hedge wins) + structured logs (`log/slog`) + a single-page status UI reusing the terminal
  aesthetic from jamesponwith.github.io. Decision log: every routing decision with inputs, as
  JSONL — auditability, like the DynamoDB state in the C1 engine.

### Config (single YAML)

```yaml
mode: learn                # learn | action
listen: :8484
upstreams:
  - name: anthropic
    kind: anthropic
    api_key_env: ANTHROPIC_API_KEY
    models: { "gpt-4o": "claude-sonnet-5" }   # requested-model → provider-model mapping
    priority: 1
  - name: openai
    kind: openai
    api_key_env: OPENAI_API_KEY
    priority: 2
  - name: local
    kind: ollama
    url: http://localhost:11434
    priority: 3            # last resort: free, always on
policy:
  type: priority           # priority | latency
  hedge_after: 0           # 0 = disabled; e.g. 1500ms
health:
  window: 60s
  eject_after: 3           # consecutive hard failures
  degrade_p95: 8s
  probe_interval: 15s
canary:
  interval: 5m
  prompts: canary/prompts.yaml
```

## Tech choices

- **Go, stdlib-first.** `net/http`, `log/slog`, `encoding/json`. Allowed deps: `prometheus/client_golang`,
  `gopkg.in/yaml.v3`. No web framework, no DI framework — the restraint is part of the portfolio signal.
- **SSE streaming passthrough** is the hard core of M1 — flush per chunk, propagate cancellation,
  translate chunk formats between providers.
- **State is in-memory.** Health windows rebuild in seconds after restart; persistence is a non-goal.

## Testing (mirror the C1 story: unit / component / live-dep)

- **Unit** — table-driven tests for health-state transitions, policy decisions, adapter translation.
- **Component** — `httptest` fake providers with scripted failure profiles (healthy, flaky-20%,
  latency-spike, hang, garbage-output). The **chaos harness** is a first-class package: profiles are
  YAML, reusable by tests and by the live demo.
- **Live-dep** — opt-in smoke suite (`-tags=live`) hitting real providers with $0.01 of traffic.
- CI: GitHub Actions — lint (golangci-lint), unit + component on PR; this repo is the pilot tenant
  for the agentic-flywheel project (see ../agentic-flywheel/SPEC.md).

## Milestones

- **M1 — passthrough proxy** (weekend 1): single upstream, OpenAI-compatible, streaming works with a
  real client (Claude Code / any SDK pointed at it). *Demo: chat through the proxy.*
- **M2 — failover** (weekend 2): three adapters, health model, priority policy, ejection + half-open
  probes, chaos harness. *Demo: kill a fake provider under load, watch traffic shift.*
- **M3 — the interesting parts**: Learn/Action modes, decision log, canary evals, hedging.
- **M4 — the artifact**: Prometheus + status page, README with architecture diagram and chaos-test
  graphs, terminal-recorded failover gif, short blog post ("What a $780B payment system taught me
  about serving LLMs"). Add project card + writeup link to the website.

## Success criteria

- Failover < 1s from detection with zero dropped requests in the chaos suite (in-flight request on
  a killed upstream is retried on the next cell before the client sees an error).
- A stranger can run it in < 5 minutes: `go install`, one YAML, done.
- Canary demo: a "lobotomized" fake provider (200s but garbage output) gets ejected within one
  canary cycle.

## Open questions

- Name — repo needs a real one before publishing (`cutover`? `cellgate`?). Decide at M4, not now.
- Anthropic-native ingress (`/v1/messages`) as a second front door — only if a client needs it.
