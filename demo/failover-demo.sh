#!/bin/bash
# M2 failover demo, sized for a terminal recording (docs/failover-demo.gif).
# Expects prebuilt binaries: BIN=dir with `router` and `chaos`, default ./bin
#   go build -o bin/router . && go build -o bin/chaos ./cmd/chaos
cd "$(dirname "$0")/.." || exit 1
BIN=${BIN:-./bin}
PIDS=""
trap 'kill $PIDS 2>/dev/null' EXIT
spawn() { "$@" 2>/dev/null & PIDS="$PIDS $!"; disown; } # disown: no "Killed" noise in the recording

spawn "$BIN/chaos" -name primary -listen :9001 -profiles chaos/profiles.yaml
PRIMARY=$!
spawn "$BIN/chaos" -name backup -listen :9002 -profiles chaos/profiles.yaml
spawn "$BIN/router" -config config.demo.yaml
until curl -s -o /dev/null localhost:8484/v1/models; do sleep 0.2; done

ask() {
	curl -s localhost:8484/v1/chat/completions -d '{"model":"m"}' |
		sed -E 's/.*"model":"([^"]*)".*/answered by: \1/'
}

echo '$ # llm-resiliency-router M2: priority failover demo'
echo '$ # router :8484 → primary :9001, backup :9002 (chaos providers)'
for _ in 1 2 3 4; do ask; sleep 0.4; done
echo
echo '$ kill -9 <primary>   # hard-kill the primary, mid-traffic'
kill -9 $PRIMARY
for _ in 1 2 3 4 5 6; do ask; sleep 0.4; done
echo '  → dial failures absorbed, cell EJECTED after 3; zero client errors'
echo
echo '$ # restart primary; ejected cell gets a half-open probe within 15s'
spawn "$BIN/chaos" -name primary -listen :9001 -profiles chaos/profiles.yaml
sleep 15
for _ in 1 2 3 4; do ask; sleep 0.4; done
echo
echo '$ # probe succeeded → healthy: traffic is back on primary.'
