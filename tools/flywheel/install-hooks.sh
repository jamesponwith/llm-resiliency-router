#!/usr/bin/env bash
# Wire the Build gate. Run once per clone (ADR 0012).
#
# lefthook owns .git/hooks; beads is a declared step inside lefthook.yml.
# core.hooksPath is deliberately left unset — beads installs its hooks wherever
# it points, which is how a previous attempt lost its pre-commit hook entirely.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

if [ -n "$(git config core.hooksPath || true)" ]; then
  echo "unsetting core.hooksPath so lefthook owns .git/hooks"
  git config --unset core.hooksPath
fi

command -v lefthook >/dev/null || {
  echo "installing lefthook…"
  go install github.com/evilmartians/lefthook@latest
  export PATH="$(go env GOPATH)/bin:$PATH"
}
lefthook install

# Prove it rejects, rather than trusting that it would. A gate you have not
# watched fail is a gate you have not got.
tmp=probe_$$.go
printf 'package main\n\nfunc  Bad( X ) {}\n' > "$tmp"
git add "$tmp"
if git commit -q -m "probe: must be rejected" >/dev/null 2>&1; then
  git reset -q --hard HEAD~1
  echo "FAIL: the gate accepted deliberately unformatted code" >&2
  exit 1
fi
git reset -q HEAD "$tmp" 2>/dev/null || true
rm -f "$tmp"
echo "verified: the gate rejects unformatted code"
