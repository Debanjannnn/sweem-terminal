// Package rates polls venue yields (Aave/Neverland reserves, ERC-4626 and shMON share prices) and Perpl funding.
package rates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const (
	year     = 365 * 24 * time.Hour
	window   = 24 * time.Hour // trailing share-price window; public RPC keeps ~4 days of state
	lpWindow = 72 * time.Hour // ActiveVault NAV is noisy; widest window the public RPC state allows
	blockSec = 0.302          // Monad mainnet block time estimate, only used to pick the past block
)

type Poller struct {
	ETH      *ethclient.Client
	RDB      redis.Cmdable
	Dep      *config.Deployments143
	PerplURL string // https://app.perpl.xyz
	HTTP     *http.Client
}

func (p *Poller) Run(ctx context.Context, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := p.Publish(ctx); err != nil {
			slog.Warn("rates", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Publish polls everything once; a failing venue is logged and skipped, not fatal.
func (p *Poller) Publish(ctx context.Context) error {
	s := p.Poll(ctx)
	if len(s.Rates)+len(s.Funding) == 0 {
		return fmt.Errorf("no rates")
	}
	for _, r := range s.Rates {
		if err := streams.Publish(ctx, p.RDB, "", streams.Rate(r.Source, r.Venue, r.Asset), r); err != nil {
			return err
		}
	}
	for _, f := range s.Funding {
		if err := streams.Publish(ctx, p.RDB, "", streams.Funding(f.PerpID), f); err != nil {
			return err
		}
	}
	return streams.Publish(ctx, p.RDB, streams.Rates, streams.RatesLatest, s)
}

func (p *Poller) Poll(ctx context.Context) streams.RatesSnapshot {
	now := time.Now()
	s := streams.RatesSnapshot{Ts: now.UnixMilli(), Rates: []streams.RateInfo{}, Funding: []streams.FundingInfo{}}
	add := func(r streams.RateInfo, err error) {
		if err != nil {
			slog.Warn("rate", "venue", r.Venue, "asset", r.Asset, "err", err)
			return
		}
		r.Ts = s.Ts
		s.Rates = append(s.Rates, r)
	}
	for venue, pool := range map[string]config.AavePool{"aave": p.Dep.Lending.AaveV3, "neverland": p.Dep.Lending.Neverland} {
		for _, asset := range sortedKeys(pool.ATokens) {
			apy, err := p.reserveAPY(ctx, pool.Pool, p.Dep.Tokens[asset].Address)
			add(streams.RateInfo{Source: "lending", Venue: venue, Asset: asset, Address: streams.Addr(pool.Pool), Apy: apy, Kind: "supply"}, err)
		}
	}
	if av := p.Dep.Kuru.ActiveVault; av != "" {
		r := streams.RateInfo{Source: "lp", Venue: "kuru-activevault", Asset: "MON", Address: streams.Addr(av), Kind: "estimated"}
		var err error
		r.Apy, err = p.activeVaultAPY(ctx, av)
		add(r, err)
	}
	past, dt, perr := p.pastBlock(ctx, window)
	share := func(r streams.RateInfo) {
		if perr != nil {
			add(r, perr)
			return
		}
		var err error
		r.Apy, err = p.trailingAPY(ctx, string(r.Address), past, dt)
		add(r, err)
	}
	for _, v := range p.Dep.Lending.ERC4626 {
		share(streams.RateInfo{Source: "lending", Venue: slug(v.Name), Asset: v.Asset, Address: streams.Addr(v.Vault), Kind: "trailing24h"})
	}
	if p.Dep.Staking.Shmonad != "" {
		share(streams.RateInfo{Source: "staking", Venue: "shmonad", Asset: "MON", Address: streams.Addr(p.Dep.Staking.Shmonad), Kind: "trailing24h"})
	}
	f, err := p.funding(ctx, now)
	if err != nil {
		slog.Warn("perpl funding", "err", err)
	}
	s.Funding = append(s.Funding, f...)
	return s
}

// slug turns "Euler k3USDC" into "euler-k3usdc".
func slug(name string) string { return strings.ToLower(strings.Join(strings.Fields(name), "-")) }

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sel(sig string) []byte { return crypto.Keccak256([]byte(sig))[:4] }

func word(v []byte) []byte { return common.LeftPadBytes(v, 32) }

func (p *Poller) call(ctx context.Context, to string, data []byte, block *big.Int) ([]byte, error) {
	a := common.HexToAddress(to)
	return p.ETH.CallContract(ctx, ethereum.CallMsg{To: &a, Data: data}, block)
}

// reserveAPY reads Pool.getReserveData(asset).currentLiquidityRate (word 2, ray APR).
func (p *Poller) reserveAPY(ctx context.Context, pool, asset string) (decimal.Decimal, error) {
	out, err := p.call(ctx, pool, append(sel("getReserveData(address)"), word(common.HexToAddress(asset).Bytes())...), nil)
	if err != nil {
		return decimal.Zero, err
	}
	if len(out) < 96 {
		return decimal.Zero, fmt.Errorf("getReserveData: %d bytes", len(out))
	}
	return AaveAPY(new(big.Int).SetBytes(out[64:96])), nil
}

// AaveAPY converts a ray APR to APY with per-second compounding, as the Aave UI does.
func AaveAPY(ray *big.Int) decimal.Decimal {
	apr, _ := new(big.Float).Quo(new(big.Float).SetInt(ray), big.NewFloat(1e27)).Float64()
	s := year.Seconds()
	return round(math.Expm1(s * math.Log1p(apr/s)))
}

// TrailingAPY annualises the share-price growth p0→p1 over dt.
func TrailingAPY(p0, p1 *big.Int, dt time.Duration) decimal.Decimal {
	if p0.Sign() <= 0 || dt <= 0 {
		return decimal.Zero
	}
	r, _ := new(big.Float).Quo(new(big.Float).SetInt(p1), new(big.Float).SetInt(p0)).Float64()
	return round(math.Expm1(math.Log(r) * float64(year) / float64(dt)))
}

func round(f float64) decimal.Decimal { return decimal.NewFromFloat(f).Round(6) }

// pastBlock picks the block ~w ago and returns it with the exact elapsed time to head.
func (p *Poller) pastBlock(ctx context.Context, w time.Duration) (*big.Int, time.Duration, error) {
	head, err := p.ETH.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	n := new(big.Int).Sub(head.Number, big.NewInt(int64(w.Seconds()/blockSec)))
	old, err := p.ETH.HeaderByNumber(ctx, n)
	if err != nil {
		return nil, 0, err
	}
	return n, time.Duration(head.Time-old.Time) * time.Second, nil
}

func (p *Poller) sharePrice(ctx context.Context, vault string, one *big.Int, block *big.Int) (*big.Int, error) {
	out, err := p.call(ctx, vault, append(sel("convertToAssets(uint256)"), word(one.Bytes())...), block)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(out), nil
}

func (p *Poller) trailingAPY(ctx context.Context, vault string, past *big.Int, dt time.Duration) (decimal.Decimal, error) {
	out, err := p.call(ctx, vault, sel("decimals()"), nil)
	if err != nil {
		return decimal.Zero, err
	}
	one := new(big.Int).Exp(big.NewInt(10), new(big.Int).SetBytes(out), nil)
	p1, err := p.sharePrice(ctx, vault, one, nil)
	if err != nil {
		return decimal.Zero, err
	}
	p0, err := p.sharePrice(ctx, vault, one, past)
	if err != nil {
		return decimal.Zero, err
	}
	return TrailingAPY(p0, p1, dt), nil
}

// activeVaultAPY: USD value of one share's (MON, USDC) withdraw amounts over lpWindow.
// Both ends are valued at the current Kuru mid, so MON's own price move is excluded and only the vault's MM PnL remains.
func (p *Poller) activeVaultAPY(ctx context.Context, vault string) (decimal.Decimal, error) {
	m := p.Dep.Kuru.Markets["MON"]
	book, err := streams.Get[streams.BookSnapshot](ctx, p.RDB, streams.BookLatest(m.Orderbook))
	if err != nil {
		return decimal.Zero, fmt.Errorf("mon book: %w", err)
	}
	if len(book.Bids) == 0 || len(book.Asks) == 0 {
		return decimal.Zero, fmt.Errorf("mon book one-sided")
	}
	mid := book.Bids[0][0].Add(book.Asks[0][0]).Div(decimal.NewFromInt(2))
	past, dt, err := p.pastBlock(ctx, lpWindow)
	if err != nil {
		return decimal.Zero, err
	}
	bd, qd := int32(p.Dep.Tokens[m.Base].Decimals), int32(p.Dep.Tokens[m.Quote].Decimals)
	nav := func(block *big.Int) (*big.Int, error) {
		out, err := p.call(ctx, vault, append(sel("previewWithdrawInAmounts(uint256)"), word(big.NewInt(1e18).Bytes())...), block)
		if err != nil {
			return nil, err
		}
		if len(out) < 64 {
			return nil, fmt.Errorf("previewWithdrawInAmounts: %d bytes", len(out))
		}
		return VaultNAV(new(big.Int).SetBytes(out[:32]), new(big.Int).SetBytes(out[32:64]), mid, bd, qd), nil
	}
	n1, err := nav(nil)
	if err != nil {
		return decimal.Zero, err
	}
	n0, err := nav(past)
	if err != nil {
		return decimal.Zero, err
	}
	return TrailingAPY(n0, n1, dt), nil
}

// VaultNAV is base×mid + quote in quote atoms.
func VaultNAV(base, quote *big.Int, mid decimal.Decimal, bd, qd int32) *big.Int {
	v := decimal.NewFromBigInt(base, -bd).Mul(mid).Shift(qd).Floor().BigInt()
	return v.Add(v, quote)
}

func (p *Poller) getJSON(ctx context.Context, u string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	res, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

type fundingPoint struct {
	At struct {
		B uint64 `json:"b"`
		T int64  `json:"t"`
	} `json:"at"`
	Rate int64 `json:"rate"` // 1e-6 of notional per interval
}

// FundingAPR annualises a per-interval rate (1e-6 units); positive = shorts receive.
func FundingAPR(rates []int64, span time.Duration) decimal.Decimal {
	if span <= 0 {
		return decimal.Zero
	}
	var sum int64
	for _, r := range rates {
		sum += r
	}
	return decimal.NewFromInt(sum).Shift(-6).Mul(decimal.NewFromInt(int64(year))).Div(decimal.NewFromInt(int64(span))).Round(6)
}

func (p *Poller) funding(ctx context.Context, now time.Time) ([]streams.FundingInfo, error) {
	var c struct {
		Markets []struct {
			PerpID      int          `json:"perpetual_id"`
			Name        string       `json:"name"`
			IntervalSec int          `json:"funding_interval_sec"`
			Funding     fundingPoint `json:"funding"`
		} `json:"markets"`
	}
	if err := p.getJSON(ctx, p.PerplURL+"/api/v1/pub/context", &c); err != nil {
		return nil, err
	}
	want := map[int]bool{}
	for _, id := range p.Dep.Perpl.PerpIds {
		want[id] = true
	}
	var out []streams.FundingInfo
	for _, m := range c.Markets {
		if !want[m.PerpID] || m.IntervalSec <= 0 {
			continue
		}
		f := streams.FundingInfo{PerpID: m.PerpID, Name: m.Name, Rate: decimal.NewFromInt(m.Funding.Rate).Shift(-6), IntervalSec: m.IntervalSec,
			AprShort: FundingAPR([]int64{m.Funding.Rate}, time.Duration(m.IntervalSec)*time.Second), Block: m.Funding.At.B, Ts: m.Funding.At.T}
		var h struct {
			D []fundingPoint `json:"d"`
		}
		from := now.Add(-7 * 24 * time.Hour)
		if err := p.getJSON(ctx, fmt.Sprintf("%s/api/v1/market-data/%d/funding/%d-%d", p.PerplURL, m.PerpID, from.UnixMilli(), now.UnixMilli()), &h); err != nil {
			slog.Warn("perpl funding history", "perp", m.PerpID, "err", err)
		} else {
			rs := make([]int64, len(h.D))
			for i, x := range h.D {
				rs[i] = x.Rate
			}
			f.AprShort7d = FundingAPR(rs, 7*24*time.Hour)
		}
		out = append(out, f)
	}
	return out, nil
}
