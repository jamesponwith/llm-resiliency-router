#!/bin/bash
# M3 canary demo, sized for a terminal recording (docs/canary-demo.gif).
# Expects prebuilt binaries: BIN=dir with `router` and `chaos`, default ./bin
#   go build -o bin/router . && go build -o bin/chaos ./cmd/chaos
cd "$(dirname "$0")/.." || exit 1
BIN=${BIN:-./bin}
PIDS=""
trap 'kill $PIDS 2>/dev/null' EXIT
spawn() { "$@" 2>/dev/null & PIDS="$PIDS $!"; disown; }
rm -f decisions.jsonl

spawn "$BIN/chaos" -name primary -listen :9001 -profile lobotomized -profiles chaos/profiles.yaml
spawn "$BIN/chaos" -name backup -listen :9002 -profile brilliant -profiles chaos/profiles.yaml
until curl -s -o /dev/null localhost:9002/v1/models; do sleep 0.2; done

content() {
	curl -s "$1/v1/chat/completions" -d '{"model":"m"}' |
		sed -E 's/.*"content":"([^"]*)".*/\1/'
}
who() {
	curl -s localhost:8484/v1/chat/completions -d '{"model":"m"}' |
		sed -E 's/.*"model":"([^"]*)".*/answered by: \1/'
}

echo '$ # M3 canary demo: a provider can be up and still be useless.'
echo '$ # primary :9001 is lobotomized: HTTP 200 on everything, but:'
echo "    primary says: \"$(content http://localhost:9001)\""
sleep 1
echo
echo '$ # the canary asks each upstream: 7x6? valid JSON? cat backwards?'
echo '$ # start the router (action mode, canary every 5s), wait one cycle'
spawn "$BIN/router" -config demo/canary-demo.yaml
sleep 6
echo
for _ in 1 2 3; do
	echo "    $(who)"
	sleep 0.4
done
echo '  → primary EJECTED on quality - it never returned a single error'
echo
echo '$ grep canary decisions.jsonl | tail -1   # the audit trail'
grep '"chose":"primary"' decisions.jsonl | grep canary | tail -1 |
	sed -E 's/.*"events":\[/    events: [/' | cut -c1-160
