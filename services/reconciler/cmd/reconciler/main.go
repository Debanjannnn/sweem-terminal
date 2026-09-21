// reconciler: chain truth. it detects, it never signs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/natsbus"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/redisclient"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/chain"
	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/diff"
	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/risk"
	"github.com/sweem-terminal/sweem-terminal/services/reconciler/internal/store"
)

const (
	sweepEvery     = 1000 * time.Millisecond // ~3 monad blocks
	reconcileEvery = 5 * time.Second
	snapshotEvery  = 30 * time.Second
	panelEvery     = 60 * time.Second
	claimTTL       = 10 * time.Minute
)

type market struct {
	key   [32]byte
	hex   string
	label string // the indexer's string market id
}

func main() {
	config.LoadDotEnv(".env")
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level()}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	chainID := config.GetEnvInt("CHAIN_ID", chains.MonadTestnet)
	if !chains.IsSupported(chainID) {
		log.Error("unsupported chain", "chainId", chainID)
		os.Exit(1)
	}
	deploy := deployments(chainID)
	rpcURL := config.GetEnv(fmt.Sprintf("MONAD_RPC_URL_%d", chainID), "https://testnet-rpc.monad.xyz")

	db, err := store.New(ctx, config.GetEnv("DATABASE_URL", "postgres://sweem:sweem@localhost:5432/sweem?sslmode=disable"))
	if err != nil {
		log.Error("postgres is unreachable, reconciler has nothing to write to", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	bus, err := natsbus.New(config.GetEnv("NATS_URL", "nats://localhost:4222"))
	if err != nil {
		log.Error("nats is unreachable, alerts would go nowhere", "err", err)
		os.Exit(1)
	}
	defer bus.Close()

	rdb, err := redisclient.New()
	if err != nil {
		log.Error("redis url is invalid, liquidation dedup would not hold", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	r := &reconciler{
		log: log, db: db, bus: bus, rdb: rdb,
		chain:       chain.New(rpcURL),
		indexer:     indexerclient.New(),
		engine:      addr(deploy, "IndexPerpEngine", "ENGINE_ADDRESS"),
		registry:    addr(deploy, "MarketRegistry", "MARKET_REGISTRY_ADDRESS"),
		vault:       addr(deploy, "SweemVault", "VAULT_ADDRESS"),
		markets:     loadMarkets(deploy, log),
		owners:      map[string]map[string]bool{},
		vaultTolBps: int64(config.GetEnvInt("VAULT_TOLERANCE_BPS", 50)),
	}
	r.sweeper = &risk.Sweeper{
		Chain: r.chain, Engine: r.engine, Registry: r.registry, Vault: r.vault,
		Markets: sweepMarkets(r.markets), Owners: r.knownOwners, Log: log,
		Claim:   r.claim,
		Publish: bus.Publish,
		Save:    db.SaveAlert,
	}

	srv := &http.Server{Addr: ":" + config.GetEnv("RECONCILER_PORT", "8085"), Handler: routes(db, log)}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health server stopped", "err", err)
		}
	}()

	log.Info("reconciler up", "chainId", chainID, "markets", len(r.markets), "engine", r.engine)
	var wg sync.WaitGroup
	for _, l := range []struct {
		name string
		d    time.Duration
		fn   func(context.Context)
	}{
		{"risk-sweep", sweepEvery, func(c context.Context) { r.sweeper.Sweep(c) }},
		{"reconcile", reconcileEvery, r.reconcile},
		{"snapshot", snapshotEvery, r.snapshot},
		{"panel", panelEvery, r.panel},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop(ctx, l.name, l.d, l.fn, log)
		}()
	}

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	wg.Wait()
	log.Info("reconciler stopped")
}

func loop(ctx context.Context, name string, every time.Duration, fn func(context.Context), log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			start := time.Now()
			fn(ctx)
			if d := time.Since(start); d > every {
				log.Warn("loop overran its interval", "loop", name, "ms", d.Milliseconds(), "interval", every.String())
			}
		}
	}
}

type reconciler struct {
	log         *slog.Logger
	db          *store.Store
	bus         *natsbus.Bus
	rdb         *redis.Client
	chain       *chain.Reader
	indexer     *indexerclient.Client
	sweeper     *risk.Sweeper
	engine      string
	registry    string
	vault       string
	markets     []market
	vaultTolBps int64

	mu     sync.RWMutex
	owners map[string]map[string]bool
}

func (r *reconciler) claim(ctx context.Context, key string) (bool, error) {
	return r.rdb.SetNX(ctx, key, "1", claimTTL).Result()
}

func (r *reconciler) knownOwners(marketID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.owners[marketID]))
	for o := range r.owners[marketID] {
		out = append(out, o)
	}
	return out
}

func (r *reconciler) addOwners(marketID string, owners []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owners[marketID] == nil {
		r.owners[marketID] = map[string]bool{}
	}
	for _, o := range owners {
		r.owners[marketID][strings.ToLower(o)] = true
	}
}

// reconcile compares against `finalized` only.
func (r *reconciler) reconcile(ctx context.Context) {
	finalized, err := r.chain.BlockNumber(ctx, finality.Final)
	if err != nil {
		r.log.Warn("finalized head unavailable, skipping reconcile", "err", err)
		return
	}
	safe, err := r.chain.BlockNumber(ctx, finality.Safe)
	if err != nil {
		safe = finalized
	}
	r.settleExecutions(ctx, safe, finalized)
	r.diffPositions(ctx, finalized)
	r.diffVault(ctx, finalized)
}

func (r *reconciler) settleExecutions(ctx context.Context, safe, finalized uint64) {
	execs, err := r.db.PendingExecutions(ctx, 500)
	if err != nil {
		r.log.Warn("could not list pending executions", "err", err)
		return
	}
	for _, e := range execs {
		if err := r.settleOne(ctx, e, safe, finalized); err != nil {
			r.log.Warn("execution settle failed", "intentKey", e.IntentKey, "err", err)
		}
	}
}

func (r *reconciler) settleOne(ctx context.Context, e store.Execution, safe, finalized uint64) error {
	rc, err := r.chain.Receipt(ctx, e.TxHash)
	if err != nil {
		return err
	}
	var dr *diff.Receipt
	if rc != nil {
		dr = &diff.Receipt{BlockNumber: rc.BlockNumber, Success: rc.Success}
	}
	tier := tierName(diff.ExecutionTier(dr, safe, finalized))
	if tier == e.Confirmation {
		return nil
	}
	if err := r.db.SetConfirmation(ctx, e.IntentKey, tier); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"intentKey": e.IntentKey, "txHash": e.TxHash, "confirmation": tier,
		"blockNumber": blockOf(dr), "success": dr == nil || dr.Success,
	})
	return r.bus.Publish(ctx, natsbus.SubjExecutionSettled, e.IntentKey+":"+tier, payload)
}

func (r *reconciler) diffPositions(ctx context.Context, finalized uint64) {
	for _, m := range r.markets {
		for _, owner := range r.knownOwners(m.hex) {
			if err := r.diffPosition(ctx, m, owner, finalized); err != nil {
				r.log.Warn("position diff failed", "market", m.hex, "owner", owner, "err", err)
			}
		}
	}
}

func (r *reconciler) diffPosition(ctx context.Context, m market, owner string, finalized uint64) error {
	local, observedAt, err := r.db.LatestSnapshot(ctx, m.hex, owner)
	if err != nil {
		return nil // nothing recorded yet is not a divergence
	}
	// D+2 guard: never diff a block the chain can still change its mind about
	if !finality.Settled(observedAt, finalized) {
		return nil
	}
	onchain, err := r.chain.Position(ctx, r.engine, m.key, owner, finality.Final)
	if err != nil {
		return err
	}
	indexed := diff.Position{Size: new(big.Int), Collateral: new(big.Int)}
	positions, err := r.indexer.Positions(ctx, owner)
	if err != nil {
		return err
	}
	for _, p := range positions {
		if p.MarketID != m.label && p.MarketID != m.hex {
			continue
		}
		size := diff.Amount(p.Size)
		if !p.IsLong {
			size.Neg(size)
		}
		indexed = diff.Position{Size: size, Collateral: diff.Amount(p.Collateral)}
	}
	for _, d := range diff.Positions(m.hex, owner, local, indexed,
		diff.Position{Size: onchain.Size, Collateral: onchain.Collateral}, observedAt, finalized) {
		if err := r.db.SaveDivergence(ctx, d); err != nil {
			return err
		}
		r.log.Warn("divergence", "kind", d.Kind, "reason", d.Reason)
	}
	return nil
}

func (r *reconciler) diffVault(ctx context.Context, finalized uint64) {
	total, err := r.chain.TotalAssets(ctx, r.vault, finality.Final)
	if err != nil {
		r.log.Warn("vault totalAssets read failed", "err", err)
		return
	}
	// ponytail: flows summed over owners we have seen, switch to an indexer aggregate when owner count grows
	flows := new(big.Int)
	seen := map[string]bool{}
	for _, m := range r.markets {
		for _, owner := range r.knownOwners(m.hex) {
			if seen[owner] {
				continue
			}
			seen[owner] = true
			fs, err := r.indexer.VaultFlows(ctx, owner)
			if err != nil {
				r.log.Warn("vault flows unavailable", "owner", owner, "err", err)
				return
			}
			for _, f := range fs {
				if strings.EqualFold(f.Kind, "withdraw") {
					flows.Sub(flows, diff.Amount(f.Assets))
					continue
				}
				flows.Add(flows, diff.Amount(f.Assets))
			}
		}
	}
	if len(seen) == 0 {
		return
	}
	d := diff.Vault(total, flows, r.vaultTolBps, finalized, finalized)
	if d == nil {
		return
	}
	if err := r.db.SaveDivergence(ctx, *d); err != nil {
		r.log.Warn("divergence write failed", "err", err)
		return
	}
	r.log.Warn("divergence", "kind", d.Kind, "reason", d.Reason)
}

// snapshot records what `latest` says. never settled, only a baseline to diff against.
func (r *reconciler) snapshot(ctx context.Context) {
	block, err := r.chain.BlockNumber(ctx, finality.Seen)
	if err != nil {
		r.log.Warn("head unavailable, skipping snapshot", "err", err)
		return
	}
	for _, m := range r.markets {
		r.refreshOwners(ctx, m)
		params, err := r.chain.MarketParams(ctx, r.registry, m.key, finality.Seen)
		if err != nil {
			r.log.Warn("market params read failed", "market", m.hex, "err", err)
			continue
		}
		oracle, err := r.chain.Oracle(ctx, params.IndexOracle, finality.Seen)
		if err != nil {
			r.log.Warn("index oracle read failed", "market", m.hex, "err", err)
			continue
		}
		for _, owner := range r.knownOwners(m.hex) {
			if err := r.snapshotOne(ctx, m, owner, block, oracle.Value); err != nil {
				r.log.Warn("snapshot failed", "market", m.hex, "owner", owner, "err", err)
			}
		}
	}
}

func (r *reconciler) snapshotOne(ctx context.Context, m market, owner string, block uint64, idx *big.Int) error {
	pos, err := r.chain.Position(ctx, r.engine, m.key, owner, finality.Seen)
	if err != nil {
		return err
	}
	notional, ratio := new(big.Int), new(big.Int)
	if pos.Size.Sign() != 0 && idx.Sign() != 0 {
		notional = wad.MulWad(new(big.Int).Abs(pos.Size), idx)
		if notional.Sign() != 0 {
			ratio = wad.DivWad(pos.Collateral, notional)
		}
	}
	return r.db.SaveSnapshot(ctx, store.Snapshot{
		MarketID: m.hex, Owner: owner, Block: block, Size: pos.Size, Collateral: pos.Collateral,
		EntryIndex: pos.EntryIndex, Notional: notional, MarginRatio: ratio, Tier: finality.Seen.BlockTag(),
	})
}

func (r *reconciler) refreshOwners(ctx context.Context, m market) {
	if stored, err := r.db.Owners(ctx, m.hex); err == nil {
		r.addOwners(m.hex, stored)
	}
	// ponytail: replays from block 0 each pass, add a watermark when event counts grow
	events, err := r.indexer.PositionEvents(ctx, m.label, 0)
	if err != nil {
		r.log.Warn("indexer position events unavailable", "market", m.label, "err", err)
		return
	}
	owners := make([]string, 0, len(events))
	for _, e := range events {
		owners = append(owners, e.Owner)
	}
	r.addOwners(m.hex, owners)
}

func (r *reconciler) panel(ctx context.Context) {
	block, err := r.chain.BlockNumber(ctx, finality.Seen)
	if err != nil {
		r.log.Warn("head unavailable, skipping panel poll", "err", err)
		return
	}
	now := time.Now().Unix()
	for _, m := range r.markets {
		if points, err := r.indexer.FundingRatePoints(ctx, m.label, now-3600, now); err == nil && len(points) > 0 {
			last := points[len(points)-1]
			if err := r.db.SaveFunding(ctx, m.hex, last.Ts, last.Rate); err != nil {
				r.log.Warn("funding snapshot write failed", "market", m.hex, "err", err)
			}
		} else if err != nil {
			r.log.Warn("funding rate points unavailable", "market", m.label, "err", err)
		}
		st, err := r.chain.MarketState(ctx, r.engine, m.key, finality.Seen)
		if err != nil {
			r.log.Warn("market state read failed", "market", m.hex, "err", err)
			continue
		}
		params, err := r.chain.MarketParams(ctx, r.registry, m.key, finality.Seen)
		if err != nil {
			r.log.Warn("market params read failed", "market", m.hex, "err", err)
			continue
		}
		if err := r.db.SaveOI(ctx, m.hex, block, st.LongOI, st.ShortOI, params.MaxOiPerSide); err != nil {
			r.log.Warn("oi snapshot write failed", "market", m.hex, "err", err)
		}
	}
}

func routes(db *store.Store, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, req *http.Request) {
		if err := db.Ping(req.Context()); err != nil {
			httpx.Fail(w, http.StatusServiceUnavailable, "db_unavailable", "reconciler cannot reach postgres, so nothing it detects can be recorded")
			return
		}
		httpx.OK(w, map[string]string{"status": "ok", "service": "reconciler"})
	})
	return httpx.Logging(log, mux)
}

func tierName(t finality.Tier) string {
	switch t {
	case finality.Safe:
		return "safe"
	case finality.Final:
		return "final"
	default:
		return "seen"
	}
}

func blockOf(r *diff.Receipt) uint64 {
	if r == nil {
		return 0
	}
	return r.BlockNumber
}

func sweepMarkets(ms []market) []risk.Market {
	out := make([]risk.Market, len(ms))
	for i, m := range ms {
		out[i] = risk.Market{ID: m.key, Hex: m.hex}
	}
	return out
}

func deployments(chainID int) map[string]string {
	path := config.GetEnv("DEPLOYMENTS_PATH", fmt.Sprintf("contracts/deployments/%d.json", chainID))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func addr(deploy map[string]string, name, envKey string) string {
	return config.GetEnv(envKey, deploy[name])
}

// MARKET_IDS entries are `0x<32 byte key>` or `0x<key>=<indexer market id>`.
func loadMarkets(deploy map[string]string, log *slog.Logger) []market {
	var out []market
	for _, entry := range config.GetEnvList("MARKET_IDS", nil) {
		hexKey, label, _ := strings.Cut(entry, "=")
		key, err := chain.MarketKey(hexKey)
		if err != nil {
			log.Warn("skipping market id", "entry", entry, "err", err)
			continue
		}
		out = append(out, market{key: key, hex: strings.ToLower(hexKey), label: label})
	}
	if len(out) > 0 {
		return out
	}
	for name, value := range deploy {
		prefix, ok := strings.CutSuffix(name, "MarketKey")
		if !ok {
			continue
		}
		key, err := chain.MarketKey(value)
		if err != nil {
			continue
		}
		out = append(out, market{key: key, hex: strings.ToLower(value), label: deploy[prefix+"FeedMarketId"]})
	}
	if len(out) == 0 {
		log.Warn("no markets configured, risk sweep and reconcile will idle until MARKET_IDS is set")
	}
	return out
}

func level() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(config.GetEnv("LOG_LEVEL", "info"))); err != nil {
		return slog.LevelInfo
	}
	return l
}
