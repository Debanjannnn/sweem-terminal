package main

import (
	"testing"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
)

// the WETH/USDC market as market-data lists it: price 2dp, size 10dp
var weth = store.Market{ID: "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7", Venue: store.VenueKuru,
	PriceDecimals: 2, SizeDecimals: 10}

// A fill from the live indexer is in raw book units. It must land in a candle in
// WAD, the scale every live candle and the chart use, never as 255179.
func TestBackfillScalesIndexerFillsToWAD(t *testing.T) {
	cs, skipped := backfillCandles(weth, []indexerclient.KuruTrade{
		{Price: "255179", Size: "80000000", Ts: 1788275816, MakerIsBuy: true},
		{Price: "not-a-number", Size: "1", Ts: 1788275817},
	})
	if skipped != 1 {
		t.Fatalf("skipped = %d, want the one unparseable fill", skipped)
	}
	if len(cs) == 0 {
		t.Fatal("no candles")
	}
	for _, c := range cs {
		if c.Open != "2551790000000000000000" || c.Volume != "8000000000000000" {
			t.Fatalf("candle %+v: want open 2551.79 and volume 0.008 in WAD", c)
		}
	}
}
