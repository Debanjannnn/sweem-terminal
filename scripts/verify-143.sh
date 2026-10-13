#!/usr/bin/env bash
set -euo pipefail
RPC=${RPC_URL:-https://rpc.monad.xyz}
F="$(dirname "$0")/../deployments/143.json"
[ "$(cast chain-id -r "$RPC")" = 143 ]
fail=0
for a in $(jq -r '.. | strings | select(test("^0x[0-9a-fA-F]{40}$"))' "$F" | sort -u); do
  [ "$a" = 0x0000000000000000000000000000000000000000 ] && continue
  if [ "$(cast code "$a" -r "$RPC")" = 0x ]; then echo "NO CODE $a"; fail=1; else echo "ok $a"; fi
done
exit $fail
