# sweem-terminal
#
# ports: wallet 8080 · market-data 8081/ws 8082 · executor 8083 (internal)
#        scheduler 8084 · reconciler 8085 · client 3000 · indexer 8090
#
# secrets are scoped PER RECIPE, never exported globally. wallet refuses to boot
# if it can see PRIVY_APP_SECRET, and that guard is a feature.

SHELL := /bin/bash
.DEFAULT_GOAL := help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "};{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}'

# ── infra ────────────────────────────────────────────────────────────────
infra: ## start postgres, redis, nats
	docker compose up -d postgres redis nats
	docker compose up migrate

infra-down: ## stop infra
	docker compose down

# ── codegen ──────────────────────────────────────────────────────────────
proto: ## regenerate go/rust/ts from proto/
	buf generate proto --template proto/buf.gen.yaml

# ── go services ──────────────────────────────────────────────────────────
wallet: ## run wallet :8080
	@test -n "$$PRIVY_APP_ID" || { echo "PRIVY_APP_ID required"; exit 1; }
	go run ./services/wallet/cmd/api

market-data: ## run market-data api :8081
	go run ./services/market-data/cmd/api

ingest: ## run market-data ingest loop
	go run ./services/market-data/cmd/ingest

scheduler: ## run scheduler :8084
	go run ./services/scheduler/cmd/scheduler

reconciler: ## run reconciler :8085
	go run ./services/reconciler/cmd/reconciler

sync-policy: ## push the privy policy from its committed spec
	@test -f .env.executor || { echo ".env.executor required"; exit 1; }
	set -a; . ./.env.executor; set +a; go run ./services/wallet/cmd/sync-policy

# ── executor (rust) ──────────────────────────────────────────────────────
executor: ## run executor :8083
	@test -f .env.executor || { echo ".env.executor required"; exit 1; }
	cd executor && cargo run

# ── ts projects (each standalone, bun only) ──────────────────────────────
client: ## run client :3000
	cd client && bun install && bun dev

oracle: ## run oracle
	cd oracle && bun install && bun start

indexer: ## run envio indexer :8090
	@test -n "$$ENVIO_API_TOKEN" || { echo "ENVIO_API_TOKEN required (HyperSync has no unauthenticated mode)"; exit 1; }
	cd indexer && bun install && bunx envio dev

# ── contracts ────────────────────────────────────────────────────────────
build-contracts: ## forge build
	cd contracts && forge build

test-contracts: ## forge test incl. invariants
	cd contracts && forge test -vv

deploy: ## deploy contracts to $$CHAIN_ID
	@test -f .env.deploy || { echo ".env.deploy required"; exit 1; }
	set -a; . ./.env.deploy; set +a; cd contracts && \
	  forge script script/Deploy.s.sol --rpc-url $$MONAD_RPC_URL_$(CHAIN_ID) --broadcast

# ── dev ──────────────────────────────────────────────────────────────────
# nohup+disown, not a trailing foreground process. a `make dev` whose services
# survive only because a foreground step holds the shell open dies silently the
# day that step is removed.
dev: infra ## start every backend service in the background
	@mkdir -p .run
	@nohup go run ./services/market-data/cmd/api    > .run/market-data.log 2>&1 & disown
	@nohup go run ./services/market-data/cmd/ingest > .run/ingest.log      2>&1 & disown
	@nohup go run ./services/wallet/cmd/api         > .run/wallet.log      2>&1 & disown
	@nohup go run ./services/scheduler/cmd/scheduler> .run/scheduler.log   2>&1 & disown
	@nohup go run ./services/reconciler/cmd/reconciler > .run/reconciler.log 2>&1 & disown
	@echo "backend up. logs in .run/. now: make client"

dev-stop: ## stop background services
	-@pkill -f 'go-build.*sweem-terminal' 2>/dev/null || true
	-@pkill -f 'services/(wallet|market-data|scheduler|reconciler)' 2>/dev/null || true
	@echo stopped

status: ## curl /health on every service
	@for p in 8080 8081 8083 8084 8085; do \
	  printf "%-6s " $$p; curl -s -m 2 http://localhost:$$p/health || echo unreachable; echo; done

# ── checks ───────────────────────────────────────────────────────────────
test: ## go + rust + contracts
	go test ./...
	cd executor && cargo test
	cd contracts && forge test

check: ## vet + clippy + typecheck
	go vet ./...
	cd executor && cargo clippy -- -D warnings
	cd client && bun run build

routes: ## replay every client-built URL against the owning service
	go run ./scripts/route-contract-test.go

.PHONY: help infra infra-down proto wallet market-data ingest scheduler reconciler \
        sync-policy executor client oracle indexer build-contracts test-contracts \
        deploy dev dev-stop status test check routes
