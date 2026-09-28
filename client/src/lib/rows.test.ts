import { describe, expect, test } from "bun:test";
import type { Market, PortfolioPosition } from "./api";
import { marketIndex, positionRows } from "./rows";

// the ETH perp as market-data lists it
const eth = {
  id: "32", venue: "perpl", kind: "perp", symbol: "ETH", base: "ETH", quote: "AUSD",
  baseDecimals: 3, quoteDecimals: 6, priceDecimals: 2, sizeDecimals: 3, tickSize: "1",
  minSize: "0", minIsQuoteNotional: true, takerFeeMicros: 345, makerFeeMicros: 45,
  status: "active", address: "", maxLeverage: 1200,
} as Market;

// exactly what GET /v1/portfolio serves for a perpl position (wallet perpl_test.go pins it)
const fromWallet: PortfolioPosition = {
  market_id: "32", side: "long", size: "1000000000000000000",
  entry_price: "2600000000000000000000", collateral: "52000000000000000000",
  mark: "2689.5", pnl: "-1500000000000000000", liquidation_price_est: "2150000000000000000000",
  source: "perpl", reconciled: true,
};

describe("perpl positions", () => {
  test("read in WAD, with the venue's pnl and a close handle", () => {
    const [p] = positionRows([fromWallet], marketIndex([eth]));
    expect(p.symbol).toBe("ETH");
    expect(p.size).toBe(1);
    expect(p.entryPrice).toBe(2600);
    expect(p.markPrice).toBe(2689.5);
    expect(p.pnl).toBe(-1.5);
    expect(p.collateral).toBe(52);
    expect(p.liquidationEst).toBe(2150);
    expect(p.leverage).toBe(50); // 2600 notional on 52 margin
    expect(p.closeMarketId).toBe("32");
  });

  test("a kuru row is untouched: no close handle", () => {
    const [p] = positionRows([{ ...fromWallet, source: "indexer", pnl: undefined }], marketIndex([eth]));
    expect(p.closeMarketId).toBeUndefined();
  });
});
