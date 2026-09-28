#!/usr/bin/env bash
# Writes ../abi: our contract ABIs via forge inspect, Kuru V2 ABIs from @toxicflow-labs/ts-sdk.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=../abi
mkdir -p "$OUT/kuru"
for c in SweemBasketFactory SweemBasket; do forge inspect "$c" abi --json > "$OUT/$c.json"; done

SDK_VERSION=0.1.1
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
(cd "$TMP" && npm pack --silent "@toxicflow-labs/ts-sdk@$SDK_VERSION" >/dev/null && tar xzf ./*.tgz)
# the bundle imports viem only for types; drop it so plain node can load the ABI arrays
sed "s/^require('viem');//" "$TMP/package/dist/abi/index.cjs" > "$TMP/abi.cjs"
node -e '
const a = require(process.argv[1]), fs = require("fs"), d = process.argv[2];
fs.writeFileSync(d + "/AccountCore.json", JSON.stringify(a.accountCoreAbi, null, 2) + "\n");
fs.writeFileSync(d + "/OrderBook.json", JSON.stringify(a.orderBookAbi, null, 2) + "\n");
' "$TMP/abi.cjs" "$OUT/kuru"
