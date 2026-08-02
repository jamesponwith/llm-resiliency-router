# flywheel-template

Template repo for Go projects built inside a personal agentic flywheel:
**Intent → Build → Validate → Release → Learn**, where AI agents do the work
and every stage feeds the next. One clone onboards a new project (or a new
agent) into the full workflow.

## Use

1. "Use this template" on GitHub, then clone.
2. Edit the module path in `go.mod`; replace `main.go` / `main_test.go`.
3. `lefthook install` (pre-commit hooks) and `bd init` (issue tracking).
4. Fill in `SPEC.md`. Every feature starts as a `bd` issue.

## What's inside

- `CLAUDE.md` — agent conventions: intent sources, ponytail, test style, commit protocol
- `SPEC.md` + `docs/adr/` — where intent lives (Intent)
- `lefthook.yml` — pre-commit: gofmt, vet, short tests, <10s (Build); pre-push: local AI review (Validate)
- `.github/workflows/pr.yml` — lint + unit gate, no green no merge (Validate)
- `.claude/settings.json` — hooks: `bd prime` on session start, fmt+vet on stop

Release (goreleaser) and Learn (DORA collector + dashboard) land in later
milestones of the [flywheel spec](https://github.com/jamesponwith/agentic-flywheel).

