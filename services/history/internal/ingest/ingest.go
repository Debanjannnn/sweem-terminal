// Package ingest pulls daily history (DefiLlama, Kintsu, Perpl funding, Kuru trades via HyperSync) into Postgres.
// Every job upserts, so reruns are safe; funding and trades resume from their last stored point.
package ingest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const (
	LlamaURL     = "https://yields.llama.fi/chart/"
	KintsuURL    = "https://kintsu.xyz/api/public/apy"
	PerplURL     = "https://app.perpl.xyz/api/v1/market-data/%d/funding/%d-%d"
	HyperSyncURL = "https://monad.hypersync.xyz/query"
	TradeTopic   = "0xf16924fba1c18c108912fcacaac7450c98eb3f2d8c0a3cdf3df7066c08f21581"
	day          = "2006-01-02"
)

// Pools: DefiLlama pool ids on Monad. Venue/asset match the publisher rate poller (rate:{source}:{venue}:{asset}):
// aave | neverland | shmonad | slug of the 4626 vault name in deployments/143.json; asset is the token symbol.
var Pools = []struct{ ID, Venue, Asset string }{
	{"0a1131e6-a59d-537c-b18f-826b9583f01f", "aave", "USDC"},
	{"d795cb4c-e30f-52c0-8447-bf0853763a20", "aave", "AUSD"},
	{"7909e3f2-f00f-5e8e-9655-0d7e189794a3", "aave", "WETH"},
	{"a9b33b07-df1e-5e92-b8b4-0c4c501097d0", "aave", "cbBTC"},
	{"1d60f574-c41c-444f-8604-462eb7b714a5", "neverland", "USDC"},
	{"ba0f675f-9f14-407b-b278-66d8f59f49ca", "neverland", "AUSD"},
	{"226ca3b7-162a-441c-ad87-047fcf2a1fc7", "neverland", "WETH"},
	{"65f6c228-5ff0-53ba-bf57-ea2c5027a417", "neverland", "cbBTC"},
	{"6dcbb786-4627-5af3-af96-7decfd18d829", "neverland", "XAUt0"},
	{"81967ed7-de53-4108-b9eb-3565249a7459", "neverland", "WMON"},
	{"79832e00-2523-4fc2-bf48-8a3f6049403f", "euler-k3usdc", "USDC"},
	{"d711af54-e736-470f-8a22-431a23de76df", "euler-csusdc", "USDC"}, // Clearstar Earn USDC
	{"f234d4dc-9b36-4b23-b1ad-ab73e8286b56", "euler-k3ausd", "AUSD"},
	{"951e4e49-9760-49a3-aea9-bd832384219f", "euler-k3weth", "WETH"},
	{"76b02552-cda7-4767-a251-47225302e14f", "euler-k3wmon", "WMON"},
	{"ec6a71a7-41ae-4e72-942f-b4bc99c3b3cb", "morpho-hyperusdca", "USDC"},
	{"01af270b-f10c-57b4-8ae2-34c9c6f085ad", "morpho-sbausd-tori-mon", "AUSD"},
	{"ee40513c-9356-4c53-9f26-446b484a8ae2", "shmonad", "MON"},
	{"73c511a9-4dc0-4397-babe-e578fd75f0dd", "kintsu", "MON"},
}

var PerpIDs = []int{1, 10, 20} // BTC, MON, ETH

// Perpl funding history starts 2026-02-16.
var FundingStart = time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)

type Market struct {
	Asset, Address string
	SizePrecision  float64
	FirstBlock     int64
}

var Markets = []Market{
	{"MON", "0x065c9d28e428a0db40191a54d33d5b7c71a9c394", 1e10, 43922596},
	{"WETH", "0xa6afd386135b7d41a6c40c525abc4a1019b0d132", 1e10, 70212319},
	{"cbBTC", "0x40c49f171202f91ff5d2fae34c22dd2bfdd22af0", 1e8, 73881022},
	{"XAUt0", "0x851145eaefdc37956b08da829fa31722199f3f07", 1e6, 76485775},
}

func MarketFor(assetOrAddr string) (Market, bool) {
	for _, m := range Markets {
		if m.Asset == assetOrAddr || m.Address == strings.ToLower(assetOrAddr) {
			return m, true
		}
	}
	return Market{}, false
}

type Ingester struct {
	DB         *pgxpool.Pool
	HTTP       *http.Client
	EnvioToken string
	Now        func() time.Time
}

// RunAll runs every job once; one failing source does not stop the others.
func (g *Ingester) RunAll(ctx context.Context) {
	for name, job := range map[string]func(context.Context) error{
		"llama": g.Llama, "kintsu": g.Kintsu, "funding": g.Funding,
		"prices": func(ctx context.Context) error { _, err := g.Prices(ctx, 0); return err },
	} {
		t := time.Now()
		if err := job(ctx); err != nil {
			slog.Error("ingest", "job", name, "err", err)
			continue
		}
		slog.Info("ingest", "job", name, "took", time.Since(t).String())
	}
}

func (g *Ingester) getJSON(ctx context.Context, url string, v any) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return resp.StatusCode, fmt.Errorf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	return 200, json.NewDecoder(resp.Body).Decode(v)
}

type rate struct {
	source, venue, asset, day string
	apy, apyBase              float64
}

func (g *Ingester) putRates(ctx context.Context, rs []rate) error {
	b := &pgx.Batch{}
	for _, r := range rs {
		b.Queue(`insert into series_rate(source,venue,asset,day,apy,apy_base) values($1,$2,$3,$4,$5,$6)
			on conflict (source,venue,asset,day) do update set apy=excluded.apy, apy_base=excluded.apy_base`,
			r.source, r.venue, r.asset, r.day, r.apy, r.apyBase)
	}
	return g.DB.SendBatch(ctx, b).Close()
}

type llamaChart struct {
	Data []struct {
		Timestamp time.Time `json:"timestamp"`
		APY       *float64  `json:"apy"`
		APYBase   *float64  `json:"apyBase"`
	} `json:"data"`
}

// parseLlama converts percent to fractions; the last point of a day wins.
func parseLlama(c llamaChart, venue, asset string) []rate {
	byDay := map[string]rate{}
	for _, p := range c.Data {
		if p.APY == nil {
			continue
		}
		base := *p.APY
		if p.APYBase != nil {
			base = *p.APYBase
		}
		d := p.Timestamp.UTC().Format(day)
		byDay[d] = rate{"defillama", venue, asset, d, *p.APY / 100, base / 100}
	}
	out := make([]rate, 0, len(byDay))
	for _, r := range byDay {
		out = append(out, r)
	}
	return out
}

func (g *Ingester) Llama(ctx context.Context) error {
	var errs []error
	for _, p := range Pools {
		var c llamaChart
		if _, err := g.getJSON(ctx, LlamaURL+p.ID, &c); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, g.putRates(ctx, parseLlama(c, p.Venue, p.Asset)))
	}
	return errors.Join(errs...)
}

type kintsuResp struct {
	Data []struct {
		Date string  `json:"date"`
		APY  float64 `json:"apy"`
	} `json:"data"`
}

// Kintsu 404s when `days` exceeds its history, so step down until it answers.
func (g *Ingester) Kintsu(ctx context.Context) error {
	for days := 365; days > 0; days -= 30 {
		var k kintsuResp
		code, err := g.getJSON(ctx, fmt.Sprintf("%s?days=%d", KintsuURL, days), &k)
		if code == 404 {
			continue
		}
		if err != nil {
			return err
		}
		var rs []rate
		for _, p := range k.Data {
			if p.APY != 0 { // ponytail: zeros are pre-launch days; a real 0% day is dropped and forward-filled
				rs = append(rs, rate{"kintsu", "kintsu", "MON", p.Date, p.APY, p.APY})
			}
		}
		return g.putRates(ctx, rs)
	}
	return errors.New("kintsu: no history")
}

type perplResp struct {
	D []struct {
		At   struct{ T int64 } `json:"at"`
		Rate int64             `json:"rate"`
	} `json:"d"`
}

// Funding pages ≤30d windows from the last stored interval (API rejects windows ending in the future).
func (g *Ingester) Funding(ctx context.Context) error {
	now := g.Now()
	for _, id := range PerpIDs {
		from := FundingStart
		var last *time.Time
		if err := g.DB.QueryRow(ctx, `select max(ts) from series_funding where perp_id=$1`, id).Scan(&last); err != nil {
			return err
		}
		if last != nil {
			from = last.Add(time.Millisecond)
		}
		for from.Before(now) {
			to := from.Add(30 * 24 * time.Hour)
			if to.After(now) {
				to = now
			}
			var p perplResp
			if _, err := g.getJSON(ctx, fmt.Sprintf(PerplURL, id, from.UnixMilli(), to.UnixMilli()), &p); err != nil {
				return err
			}
			b := &pgx.Batch{}
			for _, x := range p.D {
				b.Queue(`insert into series_funding(perp_id,ts,rate) values($1,$2,$3) on conflict do nothing`,
					id, time.UnixMilli(x.At.T).UTC(), decimal.New(x.Rate, -6).String()) // api rate is 1e-6 of notional per interval
			}
			if err := g.DB.SendBatch(ctx, b).Close(); err != nil {
				return err
			}
			from = to
		}
	}
	return nil
}

// Trade is one decoded Kuru `Trade` log.
type Trade struct {
	Market   string
	Block    int64
	LogIndex int64
	Time     time.Time
	Price    float64 // USD
	Size     float64 // base units
}

// decodeTrade reads Trade(uint40,address,bool,uint256 price1e18,uint96,address,address,uint96 filledSize); nothing indexed.
func decodeTrade(data string, sizePrecision float64) (price, size float64, err error) {
	b, err := hex.DecodeString(strings.TrimPrefix(data, "0x"))
	if err != nil || len(b) != 8*32 {
		return 0, 0, fmt.Errorf("bad trade data len %d: %v", len(b), err)
	}
	p, _ := new(big.Float).SetInt(new(big.Int).SetBytes(b[3*32 : 4*32])).Float64()
	s, _ := new(big.Float).SetInt(new(big.Int).SetBytes(b[7*32 : 8*32])).Float64()
	return p / 1e18, s / sizePrecision, nil
}

type hsResp struct {
	Data []struct {
		Logs []struct {
			BlockNumber int64  `json:"block_number"`
			LogIndex    int64  `json:"log_index"`
			Address     string `json:"address"`
			Data        string `json:"data"`
		} `json:"logs"`
		Blocks []struct {
			Number    int64  `json:"number"`
			Timestamp string `json:"timestamp"`
		} `json:"blocks"`
	} `json:"data"`
	NextBlock     int64 `json:"next_block"`
	ArchiveHeight int64 `json:"archive_height"`
}

func parseTrades(r hsResp) ([]Trade, error) {
	ts := map[int64]time.Time{}
	for _, d := range r.Data {
		for _, b := range d.Blocks {
			sec, err := strconv.ParseInt(strings.TrimPrefix(b.Timestamp, "0x"), 16, 64)
			if err != nil {
				return nil, err
			}
			ts[b.Number] = time.Unix(sec, 0).UTC()
		}
	}
	var out []Trade
	for _, d := range r.Data {
		for _, l := range d.Logs {
			m, ok := MarketFor(l.Address)
			if !ok {
				continue
			}
			p, s, err := decodeTrade(l.Data, m.SizePrecision)
			if err != nil {
				return nil, err
			}
			t, ok := ts[l.BlockNumber]
			if !ok {
				return nil, fmt.Errorf("no timestamp for block %d", l.BlockNumber)
			}
			out = append(out, Trade{m.Address, l.BlockNumber, l.LogIndex, t, p, s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Block != out[j].Block {
			return out[i].Block < out[j].Block
		}
		return out[i].LogIndex < out[j].LogIndex
	})
	return out, nil
}

// Candle matches the `candles` table: display units, v base size, q quote (USD) volume, t epoch-aligned bucket start.
type Candle struct {
	Market           string
	T                time.Time
	O, H, L, C, V, Q float64
	N                int
}

var Intervals = []struct {
	Name string
	D    time.Duration
}{{"1h", time.Hour}, {"1d", 24 * time.Hour}}

// Candles folds block-ordered trades into buckets of d.
func Candles(ts []Trade, d time.Duration) []Candle {
	var out []Candle
	idx := map[string]int{}
	for _, t := range ts {
		b := t.Time.Truncate(d)
		k := t.Market + b.String()
		i, ok := idx[k]
		if !ok {
			i, idx[k] = len(out), len(out)
			out = append(out, Candle{Market: t.Market, T: b, O: t.Price, H: t.Price, L: t.Price})
		}
		c := &out[i]
		c.H, c.L, c.C = max(c.H, t.Price), min(c.L, t.Price), t.Price
		c.V, c.Q, c.N = c.V+t.Size, c.Q+t.Price*t.Size, c.N+1
	}
	return out
}

func (g *Ingester) hypersync(ctx context.Context, from, to int64) (hsResp, error) {
	addrs := make([]string, len(Markets))
	for i, m := range Markets {
		addrs[i] = m.Address
	}
	q := map[string]any{
		"from_block": from,
		"logs":       []any{map[string]any{"address": addrs, "topics": [][]string{{TradeTopic}}}},
		"field_selection": map[string]any{
			"log":   []string{"block_number", "log_index", "address", "data"},
			"block": []string{"number", "timestamp"},
		},
	}
	if to > 0 {
		q["to_block"] = to
	}
	body, _ := json.Marshal(q)
	for tries := 1; ; tries++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", HyperSyncURL, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+g.EnvioToken)
		resp, err := g.HTTP.Do(req)
		if err != nil {
			if tries == 5 || ctx.Err() != nil {
				return hsResp{}, err
			}
			slog.Warn("hypersync retry", "err", err)
			if err := sleep(ctx, time.Duration(tries)*5*time.Second); err != nil {
				return hsResp{}, err
			}
			continue
		}
		// 30k cost/min shared with the indexer, 1k per query: wait out the window instead of failing.
		reset, _ := strconv.Atoi(resp.Header.Get("X-Ratelimit-Reset"))
		wait := time.Duration(max(reset, 1)) * time.Second
		if resp.StatusCode == 429 {
			resp.Body.Close()
			if err := sleep(ctx, wait); err != nil {
				return hsResp{}, err
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
			return hsResp{}, fmt.Errorf("hypersync %d: %s", resp.StatusCode, b)
		}
		var r hsResp
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return r, err
		}
		if left, err := strconv.Atoi(resp.Header.Get("X-Ratelimit-Remaining")); err == nil && left < 1000 {
			err = sleep(ctx, wait)
			return r, err
		}
		return r, nil
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// Prices backfills Kuru trades into 1h/1d `candles` from the cursor (or first-trade block) up to toBlock (0 = head).
// Only whole UTC days are written, and they overwrite: HyperSync is complete, while the publisher owns the open bucket.
// The cursor points at the first block of the unwritten day, so a restart rebuilds that day from scratch.
func (g *Ingester) Prices(ctx context.Context, toBlock int64) (int, error) {
	const cursor = "kuru_trades"
	from := Markets[0].FirstBlock
	if err := g.DB.QueryRow(ctx, `select block from series_cursor where name=$1`, cursor).Scan(&from); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	n := 0
	var pending []Trade
	for toBlock == 0 || from < toBlock {
		r, err := g.hypersync(ctx, from, toBlock)
		if err != nil {
			return n, err
		}
		trades, err := parseTrades(r)
		if err != nil {
			return n, err
		}
		pending = append(pending, trades...)
		var done []Trade
		mark := r.NextBlock
		if len(pending) > 0 {
			cut := pending[len(pending)-1].Time.Truncate(24 * time.Hour)
			i := sort.Search(len(pending), func(i int) bool { return !pending[i].Time.Before(cut) })
			done, pending = pending[:i], append([]Trade(nil), pending[i:]...)
			if len(pending) > 0 {
				mark = pending[0].Block
			}
		}
		if err := pgx.BeginFunc(ctx, g.DB, func(tx pgx.Tx) error {
			for _, iv := range Intervals {
				for _, c := range Candles(done, iv.D) {
					if _, err := tx.Exec(ctx, `insert into candles(market,interval,t,o,h,l,c,v,q,n) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
						on conflict (market,interval,t) do update set o=excluded.o, h=excluded.h, l=excluded.l, c=excluded.c,
						v=excluded.v, q=excluded.q, n=excluded.n`, c.Market, iv.Name, c.T, c.O, c.H, c.L, c.C, c.V, c.Q, c.N); err != nil {
						return err
					}
				}
			}
			_, err := tx.Exec(ctx, `insert into series_cursor(name,block) values($1,$2) on conflict (name) do update set block=excluded.block`, cursor, mark)
			return err
		}); err != nil {
			return n, err
		}
		n += len(done)
		slog.Debug("kuru trades", "from", from, "next", r.NextBlock, "head", r.ArchiveHeight, "written", len(done), "pending", len(pending))
		if r.NextBlock <= from || (toBlock == 0 && r.NextBlock >= r.ArchiveHeight) {
			return n, nil
		}
		from = r.NextBlock
	}
	return n, nil
}
