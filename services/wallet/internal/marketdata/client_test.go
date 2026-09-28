package marketdata

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// verbatim from GET /v1/markets on the running market-data service, trimmed to two
const liveMarkets = `{"data":[{"id":"0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61","venue":"kuru","kind":"spot","symbol":"MONUSDC","base":"MON","quote":"USDC","baseDecimals":18,"quoteDecimals":6,"priceDecimals":6,"sizeDecimals":8,"tickSize":"1","minSize":"10000000000000000000","minIsQuoteNotional":true,"takerFeeMicros":70000,"makerFeeMicros":40000,"status":"active","address":"0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61"},{"id":"32","venue":"perpl","kind":"perp","symbol":"ETH","base":"ETH","quote":"AUSD","baseDecimals":3,"quoteDecimals":6,"priceDecimals":2,"sizeDecimals":3,"tickSize":"1","minSize":"0","minIsQuoteNotional":true,"takerFeeMicros":345,"makerFeeMicros":45,"status":"active","address":"","mark":"2689500000000000000000"}]}`

func TestMarketsParsesTheRealListShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/markets" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(liveMarkets))
	}))
	defer srv.Close()

	ids, err := New(srv.URL).Markets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61", "kuru:monusdc", "32", "perpl:eth"} {
		if !slices.Contains(ids, want) {
			t.Errorf("ids %v missing %q", ids, want)
		}
	}
}

// verbatim from GET /v1/markets/32 on the running service
const liveEth = `{"data":{"id":"32","venue":"perpl","kind":"perp","symbol":"ETH","base":"ETH","quote":"AUSD","baseDecimals":3,"quoteDecimals":6,"priceDecimals":2,"sizeDecimals":3,"tickSize":"1","minSize":"0","minIsQuoteNotional":true,"takerFeeMicros":345,"makerFeeMicros":45,"status":"active","address":"","mark":"2689500000000000000000","last":"2690340000000000000000"}}`

func TestMarketParsesTheRealShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/markets/32" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(liveEth))
	}))
	defer srv.Close()
	m, err := New(srv.URL).Market(t.Context(), "32")
	if err != nil {
		t.Fatal(err)
	}
	if m.Venue != "perpl" || m.PriceDecimals != 2 || m.SizeDecimals != 3 || m.QuoteDecimals != 6 || m.Mark != "2689500000000000000000" {
		t.Fatalf("parsed %+v", m)
	}
}
