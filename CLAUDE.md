# CLAUDE.md

Go project built inside the personal agentic flywheel: Intent → Build → Validate → Release → Learn.

## Before writing code (Intent)

- Read SPEC.md and the open `bd` issue for the task. No bead, no build — create one first (`bd q "..."`).
- Any feature bigger than one session gets a SPEC.md section before code.
- Decisions that would take >5 minutes to re-derive get an ADR: `docs/adr/`, ~20 lines, copy `template.md`.

## Conventions (Build)

- Ponytail active: the laziest solution that works. stdlib first.
- No new dependency without an ADR justifying it.
- Test-first; table-driven tests (see `main_test.go` for the shape).
- Deliberate shortcuts get a `// ponytail:` comment naming the ceiling and the upgrade path.
- Pre-commit (lefthook) runs gofmt, go vet, `go test -short` — keep the whole hook under 10s.

## Commit protocol

- Small commits, imperative subject, reference the bead ID: `bd-12: add retry backoff`.
- Never bypass a failing pre-commit hook. If a gate gets skipped twice, delete it or automate it.
- Pre-push runs a local AI review (ponytail-review) — advisory findings, read them before opening the PR.
- PRs merge only on a green Validate pipeline; the human is the final approver.
