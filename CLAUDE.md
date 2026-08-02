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


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
