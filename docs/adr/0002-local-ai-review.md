# 0002. AI review runs locally in pre-push, not in CI

Date: 2026-08-02
Status: accepted

## Context

The flywheel spec originally put an AI review step in the PR pipeline. In CI
that means an `ANTHROPIC_API_KEY` secret per repo, per-token API billing on
every PR, and paid Actions minutes — recurring cost and secret management for
a team of one whose machine already runs an authenticated Claude Code.

## Decision

AI review (ponytail-review) runs in the lefthook pre-push hook on the
developer's machine, advisory-only. CI stays lint + tests: free, secretless.

## Consequences

Zero API/CI cost and no secrets to rotate. Review happens before the PR
exists, which is earlier anyway. Trade-off: nothing enforces it server-side —
a push from a machine without `claude` skips review silently.
