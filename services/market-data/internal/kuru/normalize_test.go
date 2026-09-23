package kuru

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
)

// verbatim from https://api.testnet.kuru.io/api/v1/markets
const testnetPayload = `{"feedEpoch":null,"data":[{
 "marketAddress":"0x5BDEa6F9f9ABA34F4eCb9b865646a792b835eF7f","symbol":"CBBTCUSDC",
 "baseToken":{"tokenAddress":"0xef2a20a161ac9ed1117d721336226b6399f15b4d","name":"Kuru Testnet Coinbase Wrapped BTC","symbol":"cbBTC","decimals":8},
 "quoteToken":{"tokenAddress":"0xee0722ead54f1b4fe97be399be43bc0226a6f97e","name":"Kuru Testnet USDC","symbol":"USDC","decimals":6},
 "status":"active","pricePrecision":"100","sizePrecision":"100000000","tickSize":"1",
 "minQuoteNotionalX18":"10000000000000000000","takerFeePps":7000,"makerFeePps":4000}]}`

// verbatim from https://api.kuru.io/api/v1/markets -- note the second `data`
const mainnetPayload = `{"success":true,"data":{"data":[{
 "baseasset":"0x0000000000000000000000000000000000000000","quoteasset":"0x754704bc059f8c67012fed69bc8a327a5aafb603",
 "market":"0x065C9d28e428a0Db40191a54D33d5b7c71a9c394","priceprecision":"100000000","sizeprecision":"10000000000",
 "ticksize":"100","minsize":"2000000000000","maxsize":"2000000000000000000","takerfeebps":"30","makerfeebps":"10",
 "basetoken":{"address":"0x0000000000000000000000000000000000000000","decimal":"18","ticker":"MON"},
 "quotetoken":{"address":"0x754704bc059f8c67012fed69bc8a327a5aafb603","decimal":"6","ticker":"USDC"}}]}}`

func serve(t *testing.T, chainID int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/markets" { // the client pins the version prefix
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KURU_API_URL_10143", "")
	t.Setenv("KURU_API_URL_143", "")
	t.Setenv("KURU_API_URL", "")
	switch chainID {
	case chains.MonadMainnet:
		t.Setenv("KURU_API_URL_143", srv.URL)
	default:
		t.Setenv("KURU_API_URL_10143", srv.URL)
	}
	return New(chainID)
}

// The two networks return different shapes under the same name. One normalizer,
// two wire structs: this test is what stops a single deserializer creeping back.
func TestNormalizeAcrossNetworks(t *testing.T) {
	cases := []struct {
		name    string
		chainID int
		body    string
		want    map[string]any
	}{
		{
			name: "testnet camelCase with parts-per-100k fees", chainID: chains.MonadTestnet, body: testnetPayload,
			want: map[string]any{
				"id": "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f", "symbol": "CBBTCUSDC",
				"base": "cbBTC", "quote": "USDC", "baseDecimals": 8, "quoteDecimals": 6,
				"priceDecimals": 2, "sizeDecimals": 8, "tickSize": "1",
				"minSize": "10000000000000000000", "minIsQuoteNotional": true,
				"takerFeeMicros": 70000, "makerFeeMicros": 40000, "status": "active",
			},
		},
		{
			name: "mainnet snake_case with bps fees", chainID: chains.MonadMainnet, body: mainnetPayload,
			want: map[string]any{
				"id": "0x065c9d28e428a0db40191a54d33d5b7c71a9c394", "symbol": "MONUSDC",
				"base": "MON", "quote": "USDC", "baseDecimals": 18, "quoteDecimals": 6,
				"priceDecimals": 8, "sizeDecimals": 10, "tickSize": "100",
				// 2000000000000 at 10dp = 200 MON, lifted to WAD
				"minSize": "200000000000000000000", "minIsQuoteNotional": false,
				"takerFeeMicros": 3000, "makerFeeMicros": 1000, "status": "active",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, err := serve(t, tc.chainID, tc.body).Markets(context.Background())
			if err != nil {
				t.Fatalf("Markets: %v", err)
			}
			if len(ms) != 1 {
				t.Fatalf("got %d markets, want 1", len(ms))
			}
			m := ms[0]
			got := map[string]any{
				"id": m.ID, "symbol": m.Symbol, "base": m.Base, "quote": m.Quote,
				"baseDecimals": m.BaseDecimals, "quoteDecimals": m.QuoteDecimals,
				"priceDecimals": m.PriceDecimals, "sizeDecimals": m.SizeDecimals,
				"tickSize": m.TickSize, "minSize": m.MinSize, "minIsQuoteNotional": m.MinIsQuoteNotional,
				"takerFeeMicros": m.TakerFeeMicros, "makerFeeMicros": m.MakerFeeMicros, "status": m.Status,
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s = %v, want %v", k, got[k], want)
				}
			}
			if m.Venue != "kuru" || m.Kind != "spot" {
				t.Errorf("venue/kind = %s/%s", m.Venue, m.Kind)
			}
		})
	}
}

func TestPrecisionDecimals(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"1", 0, false},
		{"100", 2, false},
		{"100000000", 8, false},
		{"10000000000", 10, false},
		{"0", 0, true},
		{"-100", 0, true},
		{"", 0, true},
		{"250", 0, true},  // not a power of ten
		{"1001", 0, true}, // looks like one, is not
		{"0x64", 0, true}, // hex is not accepted
		{"1e8", 0, true},  // exponent notation is not accepted
	}
	for _, tc := range cases {
		got, err := precisionDecimals("pricePrecision", tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("precisionDecimals(%q) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("precisionDecimals(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// Kuru's precisions are per-market scaled integers, never token decimals. This
// runs against the live list because that is the only place the claim is true
// or false; it skips when the API is unreachable rather than failing offline.
func TestLivePrecisionIsNotTokenDecimals(t *testing.T) {
	t.Setenv("KURU_API_URL_10143", "")
	t.Setenv("KURU_API_URL", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ms, err := New(chains.MonadTestnet).Markets(ctx)
	if err != nil {
		t.Skipf("kuru testnet api unreachable: %v", err)
	}
	differs := false
	for _, m := range ms {
		if m.PriceDecimals < 0 || m.SizeDecimals < 0 {
			t.Fatalf("%s has negative precision: %+v", m.Symbol, m)
		}
		if m.MinSize == "" || m.TickSize == "" {
			t.Errorf("%s is missing tickSize or minSize: %+v", m.Symbol, m)
		}
		// one tick of price must lift to exactly 10^(18-priceDecimals)
		unit := wad.Scale(big.NewInt(1), m.PriceDecimals, wad.Decimals).String()
		if want := "1" + strings.Repeat("0", wad.Decimals-m.PriceDecimals); unit != want {
			t.Errorf("%s price unit = %s, want %s", m.Symbol, unit, want)
		}
		if m.PriceDecimals != m.QuoteDecimals || m.SizeDecimals != m.BaseDecimals {
			differs = true
		}
	}
	if !differs {
		t.Error("every live market had precision equal to its token decimals, which would let the token-decimals bug pass unnoticed")
	}
}

// baseSizeMultiplier is not in the market list, so it is derived. These are the
// values read off each OrderBook on chain; the live arm re-derives the identity
// from whatever the API is serving today.
func TestBaseSizeMultiplier(t *testing.T) {
	tests := []struct {
		symbol                      string
		baseDecimals, sizePrecision string
		want                        string
	}{
		{"CBBTCUSDC", "8", "100000000", "1"},
		{"WETHUSDC", "18", "10000000000", "100000000"},
		{"MONUSDC", "18", "100000000", "10000000000"},
		{"XAUT0USDC", "6", "1000000", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			m, err := normalizeTestnet(testnetMarket{
				MarketAddress: "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61", Symbol: tt.symbol,
				PricePrecision: "100", SizePrecision: tt.sizePrecision,
				BaseToken:  testnetToken{Symbol: "BASE", Decimals: atoi(tt.baseDecimals)},
				QuoteToken: testnetToken{Symbol: "USDC", Decimals: 6},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := BaseSizeMultiplier(m)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Fatalf("baseSizeMultiplier = %s, want %s", got, tt.want)
			}
			// 10^baseDecimals / sizePrecision, spelled out
			pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(atoi(tt.baseDecimals))), nil)
			size, _ := new(big.Int).SetString(tt.sizePrecision, 10)
			if q, r := new(big.Int).QuoRem(pow, size, new(big.Int)); r.Sign() != 0 || q.Cmp(got) != 0 {
				t.Fatalf("10^%s / %s = %s rem %s, want %s exactly", tt.baseDecimals, tt.sizePrecision, q, r, got)
			}
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ms, err := New(chains.MonadTestnet).Markets(ctx)
	if err != nil {
		t.Skipf("kuru testnet api unreachable: %v", err)
	}
	for _, m := range ms {
		mult, err := BaseSizeMultiplier(m)
		if err != nil {
			t.Fatalf("%s: %v", m.Symbol, err)
		}
		pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(m.BaseDecimals)), nil)
		size := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(m.SizeDecimals)), nil)
		if q, r := new(big.Int).QuoRem(pow, size, new(big.Int)); r.Sign() != 0 || q.Cmp(mult) != 0 {
			t.Errorf("%s: 10^%d / 1e%d = %s rem %s, want %s", m.Symbol, m.BaseDecimals, m.SizeDecimals, q, r, mult)
		}
	}
}
