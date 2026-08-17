#!/usr/bin/env bash
# Territory conformance (fw-wb2.8).
#
# blackbird exposes reservations over MCP only — the local REST surface has
# agents, events and messages, but no reservation routes — so the Go
# conformance suite cannot drive this the way it drives leases. It has to be an
# agent, because only an agent has an MCP client.
#
# The property under test is the one everything else rests on: two agents must
# not hold overlapping ground. It was verified by hand once; this makes it
# repeatable, so a regression is caught rather than remembered.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

AGENT_CMD="${FLYWHEEL_AGENT_CMD:-claude}"
command -v "$AGENT_CMD" >/dev/null || { echo "no agent CLI ($AGENT_CMD); skipping" >&2; exit 0; }

read -r -d '' PROMPT <<'PROMPT_END' || true
Run a territory conformance drill against the local blackbird daemon. Report
PASS or FAIL for each step and nothing else — no code, no fixes, no edits.

1. Register two agents via blackbird_agent_register with project_key set to
   this repository's absolute path: names drill/alpha and drill/beta.
2. As alpha, blackbird_reservation_acquire an EXCLUSIVE subtree reservation on
   "docs" with a 120s TTL. Expect success; note the lease_id and fences.
3. As beta, attempt an EXCLUSIVE exact reservation on "docs/adr/template.md" —
   a path INSIDE alpha's subtree. Expect LEASE_CONFLICT. If beta succeeds,
   that is a FAIL and the most serious possible result: it means two builders
   can edit the same ground.
4. As alpha, blackbird_reservation_release using the lease_id and fences.
5. As beta, retry the same exact reservation. Expect success now.
6. Release beta's reservation.

Then print one final line: "TERRITORY DRILL: PASS" only if steps 2, 3, 5 all
met expectations, otherwise "TERRITORY DRILL: FAIL <which step>".
PROMPT_END

echo "running territory drill via $AGENT_CMD…"
out=$("$AGENT_CMD" -p "$PROMPT" </dev/null 2>&1) || true
echo "$out" | tail -20

if printf '%s' "$out" | grep -q "TERRITORY DRILL: PASS"; then
  echo
  echo "territory conformance: PASS"
  exit 0
fi
echo
echo "territory conformance: FAIL or inconclusive — read the output above." >&2
echo "An inconclusive drill is NOT a pass: overlapping territory is the one" >&2
echo "property that makes parallel builders safe." >&2
exit 1
