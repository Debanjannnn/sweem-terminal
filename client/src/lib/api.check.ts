// bun run check — asserts unit helpers and hits the live backend fetchers.
import assert from "node:assert/strict";
import { getBasket, getBaskets, getBook, getLiveNav, getMarkets, getNavHistory, getQuote } from "./api";
import { baseToken, fmtUnits, parseAmount } from "./tokens";

assert.equal(fmtUnits(BigInt("1234567891"), 6), "1,234.5678");
assert.equal(fmtUnits("1000000000000000000", 18), "1");
assert.equal(fmtUnits(-BigInt("1500000"), 6, 1), "-1.5");
assert.equal(fmtUnits(BigInt("0"), 6), "0");
assert.equal(parseAmount("1.5", 6), BigInt("1500000"));
assert.equal(parseAmount("1.1234567", 6), null);
assert.equal(parseAmount("abc", 6), null);

const markets = await getMarkets();
assert.ok(markets.length > 0, "markets");
const baskets = await getBaskets();
assert.ok(baskets.length > 0, "baskets");
const b = baskets.find((x) => x.nav) ?? baskets[0];
assert.equal((await getBasket(b.address)).address, b.address);
await getNavHistory(b.address);
const nav = await getLiveNav(b.address);
console.log(b.symbol, "nav/share", nav.navPerShare, "stale", nav.stale);
console.log(b.components.map((c) => baseToken(c.market, markets)?.symbol).join(" "));
assert.ok((await getBook(markets[0].market)).bids);
const q = await getQuote(b.address, "mint", BigInt("1000000"), 100);
assert.ok(BigInt(q.minOut) <= BigInt(q.expectedOut));
console.log("ok");
