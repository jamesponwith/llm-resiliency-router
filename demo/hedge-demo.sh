#!/bin/bash
# Hedging demo, sized for a terminal recording (docs/hedge-demo.gif).
# Expects prebuilt binaries: BIN=dir with `router` and `chaos`, default ./bin
#   go build -o bin/router . && go build -o bin/chaos ./cmd/chaos
cd "$(dirname "$0")/.." || exit 1
BIN=${BIN:-./bin}
PIDS=""
trap 'kill $PIDS 2>/dev/null' EXIT
spawn() { "$@" 2>/dev/null & PIDS="$PIDS $!"; disown; }

spawn "$BIN/chaos" -name primary -listen :9001 -profile stall -profiles chaos/profiles.yaml
spawn "$BIN/chaos" -name backup -listen :9002 -profiles chaos/profiles.yaml
spawn "$BIN/router" -config demo/hedge-demo.yaml
until curl -s -o /dev/null localhost:8484/status.json; do sleep 0.2; done

echo '$ # hedged requests: primary is STALLED - it sends 200 + headers,'
echo '$ # then never a single token. timeouts never fire; clients just hang.'
echo '$ # hedge_after: 1s - no first token in 1s -> race the backup.'
echo
for _ in 1 2 3; do
	resp=$(curl -s -w '|%{time_total}' localhost:8484/v1/chat/completions -d '{"model":"m"}')
	model=$(printf '%s' "${resp%|*}" | sed -E 's/.*"model":"([^"]*)".*/\1/')
	printf '    answered by: %s  (%.1fs)\n' "$model" "${resp##*|}"
	sleep 0.3
done
echo '  → 1s instead of forever; the cancelled loser is not blamed'
echo
echo '$ curl -s localhost:8484/metrics | grep hedge'
curl -s localhost:8484/metrics | grep '^router_hedge' | sed 's/^/    /'
