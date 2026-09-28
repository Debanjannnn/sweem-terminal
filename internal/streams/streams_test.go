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
	got := []string{Book(m), BookLatest(m), Trades(m), Ref(m), Nav(m), NavLatest(m)}
	want := []string{"book:0xabcd", "book:0xabcd:latest", "trades:0xabcd", "ref:0xabcd", "nav:0xabcd", "nav:0xabcd:latest"}
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
	roundTrip(t, []BasketInfo{{Address: "0xA", Name: "Majors", Symbol: "MAJ", Components: []Component{{Market: "0xm", Token: "0xT", WeightBps: 5000}}}})
	roundTrip(t, Trade{Market: "0xa", Ts: 2, Px: d("3.5"), Sz: d("1"), Side: "buy"})
	roundTrip(t, RefPrice{Market: "0xa", Mid: d("1"), Ref: d("1.01"), Vol: d("0.2"), Ts: 3, Deviated: true})
	s = roundTrip(t, NavPoint{Basket: "0xB", Block: 9, Ts: 4, Supply: d("100"), TotalNav: d("101.5"), NavPerShare: d("1.015"),
		Components: []NavComponent{{Market: "0xm", Token: "0xt", Balance: d("2"), Mid: d("3"), Value: d("6")}}})
	if strings.Contains(s, `"navPerShare":1`) || !strings.Contains(s, `"basket":"0xb"`) {
		t.Fatal("numbers must be strings, addresses lowercase:", s)
	}
}
