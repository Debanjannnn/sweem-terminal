.PHONY: deps abi client-abi test test-integration up down logs infra e2e

BINDINGS := internal/chain/bindings

deps:
	cd contracts && forge install --no-git foundry-rs/forge-std OpenZeppelin/openzeppelin-contracts@v5.7.0

abi:
	contracts/script/export-abi.sh
	mkdir -p $(BINDINGS)/factory $(BINDINGS)/basket
	go tool abigen --abi abi/SweemBasketFactory.json --pkg factory --type SweemBasketFactory --out $(BINDINGS)/factory/factory.go
	go tool abigen --abi abi/SweemBasket.json --pkg basket --type SweemBasket --out $(BINDINGS)/basket/basket.go

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

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

infra:
	docker compose up -d --wait postgres redis
	docker compose run --rm migrate

# real testnet txs against `make up`: deployer key only from .env.wallet, Privy e2e user from .env.e2e
e2e:
	set -a && . ./.env && . ./.env.e2e && set +a && \
	PRIVATE_KEY=$$(sed -n 's/^PRIVATE_KEY=//p' .env.wallet) E2E_DEPLOYMENTS_PATH=$(CURDIR)/contracts/deployments/10143.json \
	go test -tags e2e -count=1 -v -timeout 15m ./e2e/
