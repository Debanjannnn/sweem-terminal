.PHONY: abi test test-integration up down logs infra e2e

BINDINGS := internal/chain/bindings

abi:
	contracts/script/export-abi.sh
	mkdir -p $(BINDINGS)/factory $(BINDINGS)/basket
	go tool abigen --abi abi/SweemBasketFactory.json --pkg factory --type SweemBasketFactory --out $(BINDINGS)/factory/factory.go
	go tool abigen --abi abi/SweemBasket.json --pkg basket --type SweemBasket --out $(BINDINGS)/basket/basket.go

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

e2e:
	@echo "e2e: not implemented yet" && exit 1
