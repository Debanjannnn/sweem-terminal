package ingest

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// Real HyperSync response shape (first WETH/USDC trade, block 70212319; MON logs synthetic).
const hsFixture = `{"data":[
 {"logs":[{"log_index":35,"block_number":70212319,"address":"0xa6afd386135b7d41a6c40c525abc4a1019b0d132","data":"0x00000000000000000000000000000000000000000000000000000000000000180000000000000000000000007cfdc49bb9a248052f27c4c36cc972a965461fbd000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000007e32ef55f0765c000000000000000000000000000000000000000000000000000000000000916112f70000000000000000000000002f84fb8982073f39ba47c7fcc29119af074abbcb00000000000000000000000081b09916e77e7bd0c0dd160bac7ebc132bc3cda50000000000000000000000000000000000000000000000000000000008090631"}],
  "blocks":[{"number":70212319,"timestamp":"0x69eb5cd0"}]},
 {"logs":[
  {"log_index":9,"block_number":70212320,"address":"0x065c9d28e428a0db40191a54d33d5b7c71a9c394","data":"0x000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000006007a44c4760000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000a758d6a38000"},
  {"log_index":2,"block_number":70212320,"address":"0x065c9d28e428a0db40191a54d33d5b7c71a9c394","data":"0x0000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000064e5a6479f00000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000a758d6a38000"}],
  "blocks":[{"number":70212320,"timestamp":"0x69eb5cd1"}]}
],"next_block":70300000,"archive_height":109189286}`

func TestParseTradesAndCandles(t *testing.T) {
	var r hsResp
	if err := json.Unmarshal([]byte(hsFixture), &r); err != nil {
		t.Fatal(err)
	}
	ts, err := parseTrades(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 3 {
		t.Fatal(len(ts))
	}
	w := ts[0]
	if w.Market != Markets[1].Address || math.Abs(w.Price-2327.96) > 1e-12 || math.Abs(w.Size-0.0134809137) > 1e-12 ||
		!w.Time.Equal(time.Unix(0x69eb5cd0, 0)) {
		t.Fatalf("%+v", w)
	}
	// same block: log 2 (0.0284) must come before log 9 (0.02703)
	if ts[1].LogIndex != 2 || ts[2].LogIndex != 9 {
		t.Fatalf("order %+v", ts)
	}
	cs := Candles(ts, 24*time.Hour)
	if len(cs) != 2 {
		t.Fatal(cs)
	}
	m := cs[1]
	if m.Market != Markets[0].Address || !m.T.Equal(time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)) ||
		m.O != ts[1].Price || m.C != ts[2].Price || m.H != ts[1].Price || m.L != ts[2].Price || m.V != 36800 || m.N != 2 {
		t.Fatalf("%+v", m)
	}
	if want := (ts[1].Price + ts[2].Price) * 18400; math.Abs(m.Q-want) > 1e-9 {
		t.Fatalf("q %v want %v", m.Q, want)
	}
	// hour buckets split at the boundary
	x := []Trade{{Market: "m", Time: time.Date(2026, 1, 1, 9, 59, 59, 0, time.UTC), Price: 1, Size: 1},
		{Market: "m", Time: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), Price: 2, Size: 1}}
	if h := Candles(x, time.Hour); len(h) != 2 || h[1].T.Hour() != 10 || h[1].O != 2 {
		t.Fatalf("%+v", h)
	}
}

func TestParseLlama(t *testing.T) {
	var c llamaChart
	json.Unmarshal([]byte(`{"data":[
		{"timestamp":"2026-07-02T01:00:00.000Z","apy":9,"apyBase":1},
		{"timestamp":"2026-07-02T23:01:31.586Z","apy":6.02431,"apyBase":3.86581},
		{"timestamp":"2026-07-03T23:01:31.586Z","apy":5,"apyBase":null},
		{"timestamp":"2026-07-04T23:01:31.586Z","apy":null}]}`), &c)
	rs := parseLlama(c, "aave", "USDC")
	got := map[string]rate{}
	for _, r := range rs {
		got[r.day] = r
	}
	if len(got) != 2 || got["2026-07-02"].apy != 0.0602431 || got["2026-07-02"].apyBase != 0.0386581 || got["2026-07-03"].apyBase != 0.05 {
		t.Fatalf("%+v", got)
	}
}

func TestMarketFor(t *testing.T) {
	if m, ok := MarketFor("0xA6aFD386135B7D41A6C40C525abC4A1019b0D132"); !ok || m.Asset != "WETH" {
		t.Fatal(m)
	}
	if m, ok := MarketFor("MON"); !ok || m.SizePrecision != 1e10 {
		t.Fatal(m)
	}
}
