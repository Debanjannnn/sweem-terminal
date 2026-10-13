#!/usr/bin/env bash
# Real-money mainnet e2e: tops the e2e Privy wallet up from the treasury (deployer key in .env.wallet), then runs `make e2e`.
# Usage: scripts/real-money-e2e.sh   (from main/; stack must be up: `make up`). See ../tasks/real-money-test.md.
set -euo pipefail
cd "$(dirname "$0")/.."

RPC=${MAINNET_RPC_URL:-https://rpc.monad.xyz}
USDC=$(python3 -c "import json;print(json.load(open('contracts/deployments/143.json'))['tokens']['USDC']['address'])")
WALLET=$(sed -n 's/^E2E_PRIVY_WALLET_ADDRESS=//p' .env.e2e)
KEY=$(sed -n 's/^PRIVATE_KEY=//p' .env.wallet)
WANT_USDC=10000000           # 10 USDC (atoms)
WANT_MON=1500000000000000000 # 1.5 MON (wei)

usdc_of() { cast call "$USDC" 'balanceOf(address)(uint256)' "$1" --rpc-url "$RPC" | awk '{print $1}'; }
mon_of() { cast balance "$1" --rpc-url "$RPC"; }
show() { echo "$1: $(cast from-wei "$(mon_of "$2")") MON, $(usdc_of "$2") USDC atoms"; }

TREASURY=$(cast wallet address --private-key "$KEY")
show treasury "$TREASURY"
show "e2e wallet" "$WALLET"

need_usdc=$(python3 -c "print(max(0, $WANT_USDC - $(usdc_of "$WALLET")))")
need_mon=$(python3 -c "print(max(0, $WANT_MON - $(mon_of "$WALLET")))")
if [ "$need_usdc" != 0 ]; then
  echo "funding $need_usdc USDC atoms"
  cast send "$USDC" 'transfer(address,uint256)' "$WALLET" "$need_usdc" --private-key "$KEY" --rpc-url "$RPC" | grep -E '^(status|transactionHash)'
fi
if [ "$need_mon" != 0 ]; then
  echo "funding $need_mon wei MON"
  cast send "$WALLET" --value "$need_mon" --private-key "$KEY" --rpc-url "$RPC" | grep -E '^(status|transactionHash)'
fi
show "e2e wallet" "$WALLET"

for p in 8080 8081 8082 8084 8085 8087; do curl -fsS -o /dev/null "http://localhost:$p/healthz" || { echo "service :$p down, run make up"; exit 1; }; done

mkdir -p e2e/logs
log=e2e/logs/run-$(date -u +%Y%m%dT%H%M%SZ).log
E2E_REAL_MONEY=1 make e2e 2>&1 | tee "$log"
status=${PIPESTATUS[0]}
show "e2e wallet after" "$WALLET"
echo "log: $log"
exit "$status"
