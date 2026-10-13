package streams

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestKeys(t *testing.T) {
	m := "0xABcd"
	got := []string{Book(m), BookLatest(m), Trades(m), Ref(m)}
	want := []string{"book:0xabcd", "book:0xabcd:latest", "trades:0xabcd", "ref:0xabcd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func d(s string) Dec { return decimal.RequireFromString(s) }

func roundTrip[T any](t *testing.T, in T) string {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	b2, _ := json.Marshal(out)
	if string(b) != string(b2) {
		t.Fatalf("round trip mismatch\n%s\n%s", b, b2)
	}
	return string(b)
}

func TestCodec(t *testing.T) {
	s := roundTrip(t, BookSnapshot{Market: "0xAB", Seq: 7, Ts: 1, Bids: []Level{{d("1.25"), d("10")}}, Asks: []Level{{d("1.3"), d("0.000000000000000001")}}})
	if s != `{"market":"0xab","seq":7,"ts":1,"bids":[["1.25","10"]],"asks":[["1.3","0.000000000000000001"]]}` {
		t.Fatal(s)
	}
	roundTrip(t, MarketInfo{Market: "0xa", Base: "0xB", Quote: "0xc", BaseDecimals: 18, QuoteDecimals: 6, PricePrecision: d("100"), SizePrecision: d("1e10"), TickSize: d("1"), MinSize: d("5")})
	roundTrip(t, Trade{Market: "0xa", Ts: 2, Px: d("3.5"), Sz: d("1"), Side: "buy"})
	s = roundTrip(t, RefPrice{Market: "0xA", Mid: d("1"), Ref: d("1.015"), Vol: d("0.2"), Ts: 3, Deviated: true})
	if strings.Contains(s, `"ref":1`) || !strings.Contains(s, `"market":"0xa"`) {
		t.Fatal("numbers must be strings, addresses lowercase:", s)
	}
}
