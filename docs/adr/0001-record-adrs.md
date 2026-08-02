# 0001. Record architecture decisions

Date: 2026-08-02
Status: accepted

## Context

Decisions made mid-build evaporate. Re-deriving "why stdlib-only" or "why this
routing order" costs more than writing it down once — especially when the next
reader is an AI agent with no memory of the conversation.

## Decision

Record any decision that would take >5 minutes to re-derive as an ADR in this
directory, numbered sequentially, using `template.md` (~20 lines max).

## Consequences

Agents and humans can read intent instead of guessing it. Slight writing
overhead per decision; if an ADR grows past 20 lines it belongs in SPEC.md.
