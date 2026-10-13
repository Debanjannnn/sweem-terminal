.PHONY: deps abi client-abi test test-integration up down logs infra e2e payout

deps:
	cd contracts && forge install --no-git foundry-rs/forge-std OpenZeppelin/openzeppelin-contracts@v5.7.0

abi:
	contracts/script/export-abi.sh

client-abi:
	mkdir -p client/src/abi
	cp contracts/deployments/143.json client/src/abi/deployments.json

test:
	go test ./...
	for s in executor indexer; do [ ! -f services/$$s/Cargo.toml ] || cargo test --manifest-path services/$$s/Cargo.toml || exit 1; done
	cd contracts && forge test

# needs `make infra`
test-integration:
	go test -tags integration -count=1 ./...

up:
	docker compose up -d --build

# creator fee payout from fee_ledger; dry run, `make payout ARGS=--send` pays (DATABASE_URL, + RPC_URL, TREASURY_PRIVATE_KEY)
payout:
	go run ./services/api/cmd/payout $(ARGS)

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

infra:
	docker compose up -d --wait postgres redis
	docker compose run --rm migrate

# REAL MAINNET MONEY against `make up` (tasks/real-money-test.md). Needs E2E_REAL_MONEY=1, else the test skips.
# Privy e2e user/wallet from .env.e2e; the wallet must already hold >= 1 MON + 15 USDC (no auto-funding).
e2e:
	@[ "$$E2E_REAL_MONEY" = 1 ] || [ "$$E2E_DRY" = 1 ] || { echo "refusing: spends real funds, run E2E_REAL_MONEY=1 make e2e (or E2E_DRY=1 for no-spend checks)"; exit 1; }
	set -a && . ./.env && . ./.env.e2e && set +a && \
	CHAIN_ID=143 E2E_DEPLOYMENTS_PATH=$(CURDIR)/contracts/deployments/143.json \
	go test -tags e2e -count=1 -v -timeout 45m ./e2e/
