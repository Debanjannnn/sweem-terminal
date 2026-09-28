package perpl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// trimmed verbatim from https://testnet.perpl.xyz/api/v1/pub/context
const contextPayload = `{
 "chain":{"chain_id":10143,"name":"Monad Testnet"},
 "instances":[{"ver":245,"id":12,"address":"0x1964c32f0be608e7d29302aff5e61268e72080cc","collateral_token_id":1}],
 "tokens":[{"id":1,"address":"0xa9012a055bd4e0edff8ce09f960291c09d5322dc","symbol":"AUSD","decimals":6}],
 "markets":[
  {"id":16,"instance_id":12,"symbol":"BTC",
   "config":{"is_open":true,"price_decimals":1,"size_decimals":5,"min_posting_amount":"0","maker_fee":45,"taker_fee":345,"initial_margin":1500},
   "state":{"orl":817616,"mrk":817779,"lst":817733}},
  {"id":32,"instance_id":12,"symbol":"ETH",
   "config":{"is_open":true,"price_decimals":2,"size_decimals":3,"min_posting_amount":"1000000","maker_fee":45,"taker_fee":345},
   "state":{"orl":268984,"mrk":268786,"lst":268788}}
 ]}`

func apiServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PERPL_API_URL", srv.URL)
	return srv
}

// Market ids and decimals differ per network, so everything here is read out of
// the document rather than assumed.
func TestContextIsResolvedAtRuntime(t *testing.T) {
	apiServer(t, map[string]string{"/v1/pub/context": contextPayload})
	pc, err := New().Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if pc.ChainID != 10143 {
		t.Errorf("chainID = %d", pc.ChainID)
	}
	if pc.CollateralSymbol != "AUSD" || pc.CollateralDecimals != 6 {
		t.Errorf("collateral = %s/%d", pc.CollateralSymbol, pc.CollateralDecimals)
	}
	btc, ok := pc.Market(16)
	if !ok {
		t.Fatalf("BTC is 16 on testnet and 1 on mainnet; it must come from the context: %v", pc.IDs())
	}
	cases := []struct{ field, got, want string }{
		{"id", btc.ID, "16"},
		{"symbol", btc.Symbol, "BTC"},
		{"quote", btc.Quote, "AUSD"},
		{"venue", btc.Venue, "perpl"},
		{"kind", btc.Kind, "perp"},
		// mrk 817779 at price_decimals 1 is $81,777.9
		{"mark", btc.Mark, "81777900000000000000000"},
		// the live BTC perp caps leverage at 15x
		{"max leverage", strconv.Itoa(btc.MaxLeverage), "1500"},
		{"last", btc.Last, "81773300000000000000000"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", c.field, c.got, c.want)
		}
	}
	if btc.PriceDecimals != 1 || btc.SizeDecimals != 5 || btc.TakerFeeMicros != 345 {
		t.Errorf("btc scaling = %+v", btc)
	}
	eth, _ := pc.Market(32)
	if eth.PriceDecimals != 2 || eth.Mark != "2687860000000000000000" {
		t.Errorf("eth = %+v", eth)
	}
	if eth.MinSize != "1000000000000000000" { // 1000000 at 6dp collateral = 1.0
		t.Errorf("eth minSize = %s", eth.MinSize)
	}
	if _, ok := pc.Market(1); ok {
		t.Error("market 1 is BTC on mainnet only; it must not resolve against a testnet context")
	}
}

func TestCandlesAreScaledAndClamped(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pub/context" {
			w.Write([]byte(contextPayload))
			return
		}
		gotPath = r.URL.Path
		w.Write([]byte(`{"mt":11,"r":60,"d":[{"t":1700000040000,"o":817000,"c":817500,"h":818000,"l":816000,"v":"2500000","n":3}]}`))
	}))
	defer srv.Close()
	t.Setenv("PERPL_API_URL", srv.URL)
	c := New()
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	to := int64(1_700_000_100_000)
	cs, err := c.Candles(context.Background(), "16", 60, 0, to)
	if err != nil {
		t.Fatalf("Candles: %v", err)
	}
	// 0 would ask for millions of candles; the 1024 cap must move `from` up
	wantFrom := to - MaxCandles*60*1000
	if want := "/v1/market-data/16/candles/60/" + itoa(wantFrom) + "-" + itoa(to); gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	if len(cs) != 1 {
		t.Fatalf("got %d candles", len(cs))
	}
	got := cs[0]
	if got.OpenTime != 1_700_000_040 || got.Resolution != 60 {
		t.Errorf("bucket = %d/%d", got.OpenTime, got.Resolution)
	}
	if got.Open != "81700000000000000000000" || got.High != "81800000000000000000000" {
		t.Errorf("ohlc = %+v", got)
	}
	// volume is a collateral Amount at 6dp, not a base size
	if got.Volume != "2500000000000000000" {
		t.Errorf("volume = %s", got.Volume)
	}
	if _, err := c.Candles(context.Background(), "1", 60, 0, to); err == nil {
		t.Error("a market id absent from the context must not be queryable")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return strings.Trim(string(b), `"`)
}
