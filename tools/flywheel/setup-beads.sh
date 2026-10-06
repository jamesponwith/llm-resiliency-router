#!/usr/bin/env bash
# Put beads on the footing the flywheel already decided (agentic-flywheel ADRs
# 0011 and 0012). Run once per clone, right after `bd init`. Idempotent.
#
#   - The Dolt database is the durable copy, pushed to refs/dolt/data on origin.
#   - The JSONL exports are derived and stay local. Tracked, a stale export on a
#     feature branch is re-imported on merge and silently reopens closed beads
#     (card-auth-saga cas-edd); it also blocks `git pull --rebase` whenever dirty.
#   - lefthook owns .git/hooks. `bd init` points core.hooksPath at .beads/hooks;
#     install-hooks.sh takes it back.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
[ -d .beads ] || { echo "no .beads/ — run 'bd init' first" >&2; exit 1; }
# bd commits config.yaml by itself (during a later bd call), sweeping in
# whatever is staged at that moment. Start from a clean index so its commit
# can only hold this script's .beads/ changes, never your work.
git diff --cached --quiet ||
  { echo "unstage or commit your changes first: bd commits config.yaml on its own" >&2; exit 1; }

# 1. Config, while the index is still clean. Set via bd, not by editing YAML:
#    a dotted key beside a nested block of the same name is silently ignored.
origin=$(git remote get-url origin)
case "$origin" in
  git@*:*) dolt_url="git+ssh://${origin/://}" ;; # git@host:org/repo -> git+ssh://git@host/org/repo
  /*)      dolt_url="git+file://$origin" ;;
  *)       dolt_url="git+$origin" ;;
esac
bd config set export.git-add false >/dev/null # don't stage a file git will ignore
bd config set sync.remote "$dolt_url" >/dev/null

# 2. Exports are local-only (ADR 0011).
for f in issues.jsonl interactions.jsonl; do
  grep -qxF "$f" .beads/.gitignore || echo "$f" >> .beads/.gitignore
  git rm -q --cached --ignore-unmatch ".beads/$f"
done

# 3. The database is the durable copy (ADR 0011).
bd dolt remote list 2>/dev/null | grep -q '^origin' || bd dolt remote add origin "$dolt_url" >/dev/null
bd dolt push >/dev/null
git ls-remote --exit-code origin refs/dolt/data >/dev/null ||
  { echo "FAIL: bd dolt push did not create refs/dolt/data on origin" >&2; exit 1; }

# 4. lefthook owns the hooks (ADR 0012).
tools/flywheel/install-hooks.sh

# Prove it, rather than trusting that it would.
if git ls-files --error-unmatch .beads/issues.jsonl >/dev/null 2>&1; then
  echo "FAIL: .beads/issues.jsonl is still tracked" >&2; exit 1
fi
[ -z "$(git config --get core.hooksPath || true)" ] ||
  { echo "FAIL: core.hooksPath is still set" >&2; exit 1; }
echo "verified: exports untracked, database pushed to refs/dolt/data, lefthook owns .git/hooks"
echo "commit whatever .beads/ changes git status now shows"
