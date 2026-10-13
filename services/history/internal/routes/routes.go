// Package routes serves the stored daily series and the backtest endpoint.
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/snehendu098/sweem-terminal/internal/httpx"
	"github.com/snehendu098/sweem-terminal/services/history/internal/backtest"
	"github.com/snehendu098/sweem-terminal/services/history/internal/ingest"
)

const day = backtest.Day

type Server struct {
	DB  *pgxpool.Pool
	Now func() time.Time
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/series/rates", s.rates)
	mux.HandleFunc("GET /api/v1/series/funding", s.funding)
	mux.HandleFunc("GET /api/v1/series/prices", s.prices)
	mux.HandleFunc("POST /api/v1/backtest", s.backtest)
}

func fail(w http.ResponseWriter, status int, code, detail string) {
	httpx.JSON(w, status, map[string]string{"error": code, "detail": detail})
}

func internal(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "err", err)
	httpx.Error(w, http.StatusInternalServerError, "internal")
}

// window parses from/to (YYYY-MM-DD); defaults to the year ending yesterday, the last complete UTC day.
func (s *Server) window(fromS, toS string) (from, to time.Time, err error) {
	to = s.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	if toS != "" {
		if to, err = time.Parse(day, toS); err != nil {
			return
		}
	}
	from = to.AddDate(-1, 0, 0)
	if fromS != "" {
		if from, err = time.Parse(day, fromS); err != nil {
			return
		}
	}
	if from.After(to) || to.Sub(from) > 3*366*24*time.Hour {
		err = errors.New("from must be <= to and span <= 3y")
	}
	return
}

// rows runs q and returns each row as column->text, so numerics stay exact decimal strings.
func (s *Server) rows(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rs, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rs, pgx.RowToMap)
	if out == nil {
		out = []map[string]any{}
	}
	return out, err
}

func (s *Server) rates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, err := s.window(q.Get("from"), q.Get("to"))
	if err != nil {
		fail(w, 400, "invalid_range", err.Error())
		return
	}
	data, err := s.rows(r.Context(), `select source, venue, asset, to_char(day,'YYYY-MM-DD') as day, apy::text as apy, apy_base::text as "apyBase"
		from series_rate where day between $1 and $2 and ($3='' or venue=$3) and ($4='' or asset=$4) order by venue, asset, source, day`,
		from, to, q.Get("venue"), q.Get("asset"))
	if err != nil {
		internal(w, "rates", err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"data": data})
}

func (s *Server) funding(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := strconv.Atoi(q.Get("perpId"))
	if _, ok := backtest.MaxLeverage[id]; err != nil || !ok {
		fail(w, 400, "invalid_perp_id", "perpId must be 1, 10 or 20")
		return
	}
	from, to, err := s.window(q.Get("from"), q.Get("to"))
	if err != nil {
		fail(w, 400, "invalid_range", err.Error())
		return
	}
	data, err := s.rows(r.Context(), `select to_char(day,'YYYY-MM-DD') as day, rate::text as rate, (rate*365)::text as apr, n as intervals
		from series_funding_daily where perp_id=$1 and day between $2 and $3 order by day`, id, from, to)
	if err != nil {
		internal(w, "funding", err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"perpId": id, "data": data})
}

func (s *Server) prices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	m, ok := ingest.MarketFor(q.Get("market"))
	if !ok {
		fail(w, 400, "invalid_market", "market must be MON, WETH, cbBTC, XAUt0 or a Kuru market address")
		return
	}
	from, to, err := s.window(q.Get("from"), q.Get("to"))
	if err != nil {
		fail(w, 400, "invalid_range", err.Error())
		return
	}
	data, err := s.rows(r.Context(), `select to_char(t at time zone 'UTC','YYYY-MM-DD') as day, o::text as o, h::text as h, l::text as l, c::text as c,
		v::text as volume, q::text as "quoteVolume", n as trades
		from candles where market=$1 and interval='1d' and t >= $2 and t < $3 order by t`, m.Address, from, to.AddDate(0, 0, 1))
	if err != nil {
		internal(w, "prices", err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"market": m.Address, "asset": m.Asset, "data": data})
}

// BacktestRequest: decimals are strings; weights are fractions summing to 1.
// Multi-token: Sleeves (2..5, each a single-asset request with Weight) replace Asset/Legs; Amount is then USD.
type BacktestRequest struct {
	Weight  string            `json:"weight,omitempty"`
	Sleeves []BacktestRequest `json:"sleeves,omitempty"`
	Asset   string            `json:"asset"`
	Amount  string            `json:"amount"`
	From    string            `json:"from"`
	To      string            `json:"to"`
	Legs    []struct {
		Kind   string `json:"kind"`
		Venue  string `json:"venue"`
		PerpID int    `json:"perpId"`
		Weight string `json:"weight"`
	} `json:"legs"`
	Rules struct {
		DriftBps        string `json:"driftBps"`
		MinIntervalDays int    `json:"minIntervalDays"`
		FundingGate     *struct {
			APR          string `json:"apr"`
			LookbackDays int    `json:"lookbackDays"`
		} `json:"fundingGate"`
		BasisLeverage  string `json:"basisLeverage"`
		SlippageBps    string `json:"slippageBps"`
		LPAssumedAPR   string `json:"lpAssumedApr"`
		IncludeRewards bool   `json:"includeRewards"`
	} `json:"rules"`
}

func num(s, def, field string, errs *[]string) float64 {
	if s == "" {
		s = def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*errs = append(*errs, field)
	}
	return v
}

func (s *Server) input(req BacktestRequest) (backtest.Input, bool, error) {
	var errs []string
	in := backtest.Input{Asset: req.Asset, Amount: num(req.Amount, "", "amount", &errs)}
	var err error
	if in.From, in.To, err = s.window(req.From, req.To); err != nil {
		return in, false, err
	}
	for _, l := range req.Legs {
		in.Legs = append(in.Legs, backtest.Leg{Kind: l.Kind, Venue: l.Venue, PerpID: l.PerpID, Weight: num(l.Weight, "", "weight", &errs)})
	}
	rr := req.Rules
	in.Rules = backtest.Rules{
		DriftBps:        num(rr.DriftBps, "500", "driftBps", &errs),
		MinIntervalDays: rr.MinIntervalDays,
		BasisLeverage:   num(rr.BasisLeverage, "2", "basisLeverage", &errs),
		SlippageBps:     num(rr.SlippageBps, "10", "slippageBps", &errs),
		LPAssumedAPR:    num(rr.LPAssumedAPR, "0.13", "lpAssumedApr", &errs),
	}
	if g := rr.FundingGate; g != nil {
		in.Rules.GateAPR = num(g.APR, "0", "fundingGate.apr", &errs)
		in.Rules.GateLookbackDays = g.LookbackDays
		if g.LookbackDays < 1 {
			errs = append(errs, "fundingGate.lookbackDays")
		}
	}
	if len(errs) > 0 {
		return in, false, errors.New("invalid numbers: " + errs[0])
	}
	return in, rr.IncludeRewards, nil
}

func (s *Server) series(ctx context.Context, in backtest.Input, rewards bool) (backtest.Series, error) {
	out := backtest.Series{APY: map[string]map[string]float64{}, Funding: map[int]map[string]float64{}}
	put := func(m map[string]float64, k string, v float64) map[string]float64 {
		if m == nil {
			m = map[string]float64{}
		}
		m[k] = v
		return m
	}
	if mk, ok := ingest.MarketFor(in.Asset); ok && !backtest.Stables[in.Asset] {
		rs, _ := s.DB.Query(ctx, `select to_char(t at time zone 'UTC','YYYY-MM-DD'), c::float8 from candles where market=$1 and interval='1d'`, mk.Address)
		var d string
		var v float64
		if _, err := pgx.ForEachRow(rs, []any{&d, &v}, func() error { out.Price = put(out.Price, d, v); return nil }); err != nil {
			return out, err
		}
	}
	col := "apy_base"
	if rewards {
		col = "apy"
	}
	assets := []string{in.Asset}
	if in.Asset == "MON" {
		assets = append(assets, "WMON") // MON strategies lend WMON
	}
	// one row per venue/day, preferring a venue's own API (kintsu) over DefiLlama
	rs, _ := s.DB.Query(ctx, `select distinct on (venue, day) venue, to_char(day,'YYYY-MM-DD'), `+col+`::float8
		from series_rate where asset = any($1) order by venue, day, source='defillama'`, assets)
	var venue, d string
	var v float64
	if _, err := pgx.ForEachRow(rs, []any{&venue, &d, &v}, func() error {
		kind := "lending"
		if venue == "shmonad" || venue == "kintsu" {
			kind = "staking"
		}
		out.APY[kind+":"+venue] = put(out.APY[kind+":"+venue], d, v)
		return nil
	}); err != nil {
		return out, err
	}
	var perps []int
	for _, l := range in.Legs {
		if l.Kind == "basis" {
			perps = append(perps, l.PerpID)
		}
	}
	rs, _ = s.DB.Query(ctx, `select perp_id, to_char(day,'YYYY-MM-DD'), rate::float8 from series_funding_daily where perp_id = any($1)`, perps)
	var id int
	_, err := pgx.ForEachRow(rs, []any{&id, &d, &v}, func() error { out.Funding[id] = put(out.Funding[id], d, v); return nil })
	return out, err
}

var notes = []string{
	"historical, mainnet data; past performance does not predict future results",
	"daily steps at UTC close; APY and prices forward-filled across gaps, missing funding days count as 0",
	"lending/staking accrue (1+apy)^(1/365) daily; rewards excluded unless includeRewards",
	"basis = Kuru spot + Perpl short, capital split spot L/(L+1) + AUSD collateral 1/(L+1); hedge PnL 0; funding on spot notional; no liquidation modelled",
	"costs: Kuru taker 0 + slippageBps on swaps, Perpl taker 3.45 bps, shMON instant redeem 0.9%, Kintsu 0.1%; applied on entry, rebalance and final exit",
	"funding gate: basis weight moves to the highest-APY lending venue for the asset while trailing funding APR < threshold",
	"lp (Kuru ActiveVault) is ESTIMATED: 50/50 constant-product value from Kuru MON price + lpAssumedApr, no venue history",
}

func (s *Server) backtest(w http.ResponseWriter, r *http.Request) {
	var req BacktestRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, 400, "bad_request", err.Error())
		return
	}
	if len(req.Sleeves) > 0 {
		s.multi(w, r.Context(), req)
		return
	}
	in, rewards, err := s.input(req)
	if err != nil {
		fail(w, 400, "bad_request", err.Error())
		return
	}
	ser, err := s.series(r.Context(), in, rewards)
	if err != nil {
		internal(w, "load series", err)
		return
	}
	res, err := backtest.Run(in, ser)
	if runFailed(w, err) {
		return
	}
	httpx.JSON(w, 200, encode(in, res))
}

// runFailed writes the response for a backtest.Run error; false when err is nil.
func runFailed(w http.ResponseWriter, err error) bool {
	var be *backtest.Error
	if errors.As(err, &be) {
		status := 400
		if be.Code == "no_data" {
			status = 422
		}
		fail(w, status, be.Code, be.Msg)
		return true
	}
	if err != nil {
		internal(w, "backtest", err)
		return true
	}
	return false
}

// multi runs each sleeve at amount 1 over the parent window, aligns them to their common window, then combines in USD.
func (s *Server) multi(w http.ResponseWriter, ctx context.Context, req BacktestRequest) {
	bad := func(msg string) { fail(w, 400, "bad_request", msg) }
	n := len(req.Sleeves)
	if req.Asset != "" || len(req.Legs) > 0 || req.Weight != "" {
		bad("sleeves excludes asset, legs and weight")
		return
	}
	if n < 2 || n > 5 {
		bad("sleeves must have 2..5 entries")
		return
	}
	var errs []string
	usd := num(req.Amount, "1000", "amount", &errs)
	if len(errs) > 0 || !(usd > 0) {
		bad("amount must be a USD number > 0")
		return
	}
	from, to, err := s.window(req.From, req.To)
	if err != nil {
		bad(err.Error())
		return
	}
	ins, rewards, ws, sum := make([]backtest.Input, n), make([]bool, n), make([]float64, n), 0.0
	for i, sr := range req.Sleeves {
		if len(sr.Sleeves) > 0 {
			bad("nested sleeves")
			return
		}
		ws[i] = num(sr.Weight, "", "weight", &errs)
		if len(errs) > 0 || !(ws[i] > 0) {
			bad(fmt.Sprintf("sleeves[%d].weight must be > 0", i))
			return
		}
		sum += ws[i]
		sr.Amount, sr.From, sr.To = "1", req.From, req.To
		if ins[i], rewards[i], err = s.input(sr); err != nil {
			bad(fmt.Sprintf("sleeves[%d]: %v", i, err))
			return
		}
	}
	if math.Abs(sum-1) > 1e-9 {
		bad(fmt.Sprintf("sleeve weights sum to %g, want 1", sum))
		return
	}
	sers := make([]backtest.Series, n)
	for i := range ins {
		if sers[i], err = s.series(ctx, ins[i], rewards[i]); err != nil {
			internal(w, "load series", err)
			return
		}
	}
	rs := make([]*backtest.Result, n)
	var lo, hi string
	for i := range ins {
		if rs[i], err = backtest.Run(ins[i], sers[i]); runFailed(w, err) {
			return
		}
		if i == 0 {
			lo, hi = rs[i].From, rs[i].To
		}
		lo, hi = max(lo, rs[i].From), min(hi, rs[i].To)
	}
	if lo > hi {
		fail(w, 422, "no_data", "sleeves' series do not overlap the requested window")
		return
	}
	// re-run sleeves clamped wider than the common window so all start (and are scaled) on the same day
	for i, r := range rs {
		if r.From != lo || r.To != hi {
			ins[i].From, _ = time.Parse(day, lo)
			ins[i].To, _ = time.Parse(day, hi)
			if rs[i], err = backtest.Run(ins[i], sers[i]); runFailed(w, err) {
				return
			}
		}
	}
	httpx.JSON(w, 200, encode(backtest.Input{Asset: "MULTI", Amount: usd, From: from, To: to}, backtest.Combine(rs, ws, usd)))
}

func f(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }

func encode(in backtest.Input, r *backtest.Result) map[string]any {
	contrib := map[string]string{}
	for k, v := range r.Contributions {
		contrib[k] = f(v)
	}
	curve := make([]map[string]any, len(r.Curve))
	for i, p := range r.Curve {
		curve[i] = map[string]any{"day": p.Day, "usd": f(p.USD), "asset": f(p.Asset), "hodlUsd": f(p.HodlUSD), "basisActive": p.BasisActive}
	}
	return map[string]any{
		"label": "historical, mainnet data", "asset": in.Asset, "amount": f(in.Amount),
		"requested": map[string]string{"from": in.From.Format(day), "to": in.To.Format(day)},
		"from":      r.From, "to": r.To, "days": r.Days, "estimated": r.Estimated, "coverage": r.Coverage,
		"initialUsd": f(r.InitialUSD), "finalUsd": f(r.FinalUSD), "finalAsset": f(r.FinalAsset),
		"returnUsd": f(r.FinalUSD/r.InitialUSD - 1), "returnAsset": f(r.FinalAsset/in.Amount - 1),
		"apyUsd": f(r.APYUSD), "apyAsset": f(r.APYAsset),
		"maxDrawdownUsd": f(r.MaxDrawdownUSD), "maxDrawdownAsset": f(r.MaxDrawdownAsset),
		"hodl":       map[string]string{"finalUsd": f(r.HodlFinalUSD), "returnUsd": f(r.HodlFinalUSD/r.InitialUSD - 1), "apyUsd": f(r.HodlAPYUSD), "maxDrawdownUsd": f(r.HodlMaxDrawdownUSD)},
		"rebalances": r.Rebalances, "costsUsd": f(r.CostsUSD), "exitCostUsd": f(r.ExitCostUSD),
		"contributions": contrib, "curve": curve, "notes": notes,
	}
}
