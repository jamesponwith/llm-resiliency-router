# llm-resiliency-router

A Go reverse proxy that sits in front of multiple LLM inference endpoints and
keeps requests flowing through provider outages, latency spikes, and quality
regressions. Point any OpenAI SDK at it by changing `base_url`; failover —
including mid-stream — is the router's problem, not the client's.

Full design: [SPEC.md](SPEC.md). Status: pre-M1.

Pilot project of the [personal agentic flywheel](https://github.com/jamesponwith/agentic-flywheel) —
built end-to-end through its Intent → Build → Validate → Release → Learn loop,
from the [flywheel-template](https://github.com/jamesponwith/flywheel-template).
