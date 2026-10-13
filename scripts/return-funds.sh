#!/usr/bin/env bash
# Returns e2e funds: e2e wallet margin + USDC -> treasury (executor, inside the Privy policy), then treasury USDC + MON -> $1.
# MON in the e2e wallet stays (policy has no native-transfer rule): export the embedded key to recover it.
# Usage: scripts/return-funds.sh <to-address>   (from main/, stack up)
set -euo pipefail
cd "$(dirname "$0")/.."
TO=${1:?usage: return-funds.sh <to-address>}
cast to-checksum "$TO" >/dev/null

RPC=${MAINNET_RPC_URL:-https://rpc.monad.xyz}
dep() { python3 -c "import json;d=json.load(open('contracts/deployments/143.json'));print($1)"; }
USDC=$(dep "d['tokens']['USDC']['address']")
MA=$(dep "d['kuru']['marginAccount']")
WALLET=$(sed -n 's/^E2E_PRIVY_WALLET_ADDRESS=//p' .env.e2e)
USER_ID=$(sed -n 's/^E2E_PRIVY_USER_ID=//p' .env.e2e)
KEY=$(sed -n 's/^PRIVATE_KEY=//p' .env.wallet)
SECRET=$(sed -n 's/^INTERNAL_SECRET=//p' .env.executor)
TREASURY=$(cast wallet address --private-key "$KEY")
PGU=$(docker compose exec -T postgres printenv POSTGRES_USER)
PGD=$(docker compose exec -T postgres printenv POSTGRES_DB)
sql() { docker compose exec -T postgres psql -U "$PGU" -d "$PGD" -At -v ON_ERROR_STOP=1 -c "$1"; }
num() { awk '{print $1}'; }
usdc_of() { cast call "$USDC" 'balanceOf(address)(uint256)' "$1" --rpc-url "$RPC" | num; }
show() { echo "$1: $(cast balance --ether "$2" --rpc-url "$RPC") MON, $(usdc_of "$2") USDC atoms"; }

show "e2e wallet" "$WALLET"
show treasury "$TREASURY"

# 1. e2e wallet -> treasury via one executor run: margin withdraw, then USDC transfers <= 9.9 (MAX_STEP_USD=10)
margin=$(cast call "$MA" 'getBalance(address,address)(uint256)' "$WALLET" "$USDC" --rpc-url "$RPC" | num)
total=$(( $(usdc_of "$WALLET") + margin ))
steps=$(python3 -c "
import json
m, t, cap = $margin, $total, 9_900_000
s = [{'action': 'kuru.marginWithdraw', 'token': 'USDC', 'amount': str(m)}] if m else []
while t > 0:
    s.append({'action': 'fee.transfer', 'amount': str(min(t, cap))}); t -= min(t, cap)
print(json.dumps(s))")
echo "steps: $steps"
if [ "$total" -gt 0 ]; then
  deleg=$(sql "select id from delegations where user_id = '$USER_ID' order by created_at desc limit 1")
  restore() { sql "update delegations set active = false, revoked_at = now() where id = '$deleg'" >/dev/null; echo "delegation revoked"; }
  trap restore EXIT
  sql "update delegations set active = true, revoked_at = null where id = '$deleg'" >/dev/null
  exec_id=$(sql "insert into executions (user_id, kind, slot, steps) values ('$USER_ID', 'return_funds', 0, '$steps'::jsonb) returning id" | head -1)
  echo "execution $exec_id"
  docker run --rm --network sweem-baskets_default curlimages/curl:8.10.1 -sS -X POST http://executor:8083/api/v1/user/steps \
    -H 'Content-Type: application/json' -H "x-internal-secret: $SECRET" -H 'x-caller: api' -d "{\"executionId\":\"$exec_id\"}"
  echo
  sql "select status, coalesce(error,''), array_to_string(tx_hashes, ' ') from executions where id = '$exec_id'"
  [ "$(sql "select status from executions where id = '$exec_id'")" = confirmed ] || { echo "executor run not confirmed, stopping"; exit 1; }
fi

# 2. treasury -> TO: all USDC, MON minus gas for both transfers
u=$(usdc_of "$TREASURY")
if [ "$u" -gt 0 ]; then
  cast send "$USDC" 'transfer(address,uint256)' "$TO" "$u" --private-key "$KEY" --rpc-url "$RPC" | grep -E '^(status|transactionHash)'
fi
# TO may be an EIP-7702 / contract account (code runs on receive), so estimate gas instead of assuming 21000
gl=$(( $(cast estimate "$TO" --value 1 --from "$TREASURY" --rpc-url "$RPC") * 2 ))
gas=$(( $(cast gas-price --rpc-url "$RPC") * gl * 3 ))
mon=$(( $(cast balance "$TREASURY" --rpc-url "$RPC") - gas ))
if [ "$mon" -gt 0 ]; then
  cast send "$TO" --value "$mon" --gas-limit "$gl" --private-key "$KEY" --rpc-url "$RPC" | grep -E '^(status|transactionHash)'
fi

show "e2e wallet" "$WALLET"
show treasury "$TREASURY"
show "$TO" "$TO"
