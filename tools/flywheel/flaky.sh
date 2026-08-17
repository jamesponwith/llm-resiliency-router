#!/usr/bin/env bash
# Flaky test detection (fw-l8k.6).
#
# The router already ate one decision-log flake, fixed by hand. A flake fixed by
# hand once is a flake that comes back, because nothing recorded that it was
# ever flaky.
#
# Detection is the honest part: a test that FAILS and then PASSES on an
# unchanged tree is flaky by definition. Everything else is a guess.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

LEDGER=".flywheel/flaky.jsonl"
CMD="${FLAKY_CMD:-}"
if [ -z "$CMD" ]; then
  if [ -f go.mod ]; then CMD="go test ./..."
  elif [ -f pyproject.toml ]; then CMD="uv run pytest -q"
  else echo "set FLAKY_CMD" >&2; exit 2; fi
fi

case "${1:-detect}" in
detect)
  if $CMD >/tmp/flaky-1.out 2>&1; then
    echo "suite green — nothing to detect"; exit 0
  fi
  echo "suite red; re-running on the SAME tree to tell flaky from broken"
  if ! $CMD >/tmp/flaky-2.out 2>&1; then
    echo "red twice on an unchanged tree — this is broken, not flaky" >&2
    exit 1
  fi
  # Red then green with nothing changed: flaky by definition.
  mkdir -p .flywheel
  names=$(grep -oE '^--- FAIL: [A-Za-z0-9_/]+|^FAILED [^ ]+' /tmp/flaky-1.out |
          sed 's/^--- FAIL: //; s/^FAILED //' | sort -u)
  [ -z "$names" ] && names="(unidentified)"
  for n in $names; do
    printf '{"ts":"%s","test":"%s","commit":"%s","deadline":"%s"}\n' \
      "$(date -u +%FT%TZ)" "$n" "$(git rev-parse --short HEAD)" \
      "$(date -u -d '+14 days' +%F)" >> "$LEDGER"
    echo "  flaky: $n (quarantine deadline $(date -u -d '+14 days' +%F))"
  done
  echo
  echo "Recorded in $LEDGER. Per the bypassed-twice rule, a test still"
  echo "quarantined at its deadline gets deleted rather than tolerated."
  ;;

overdue)
  # A quarantine with no deadline is just a disabled test nobody owns.
  [ -f "$LEDGER" ] || { echo "no flakes recorded"; exit 0; }
  today=$(date -u +%F); n=0
  while IFS= read -r line; do
    d=$(printf '%s' "$line" | grep -oE '"deadline":"[0-9-]+"' | cut -d'"' -f4)
    t=$(printf '%s' "$line" | grep -oE '"test":"[^"]+"' | cut -d'"' -f4)
    if [ -n "$d" ] && [ "$d" \< "$today" ]; then
      echo "  OVERDUE since $d: $t — delete it or fix it"; n=$((n+1))
    fi
  done < "$LEDGER"
  [ "$n" -eq 0 ] && echo "no overdue quarantines" || exit 1
  ;;

*) echo "usage: flaky.sh [detect|overdue]" >&2; exit 2 ;;
esac
