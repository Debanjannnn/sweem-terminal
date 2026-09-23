import { describe, expect, it } from "vitest";
import { decodeBookUpdates, decodeTrades } from "./kuru-packed.js";

// payloads below are verbatim from HyperSync, WETH/USDC 0xa9C2...cbE7.
// the assertions are the Data Source API's row for the same trade, so this
// test fails if the bit layout drifts — not just if the decoder does.

const TRADE_64699421_20 =
  "0x00000000480505000431c4000000000000000013ab6680000000000002e82700000000000000000000000000000fa00000000000000000000000000000003d8f";

const BOOK_64699421_10 =
  "0x00000000480080000433c800000000000000003178cb80000000000002e8280000000000000fa0";

// two maker legs of one taker match in a single log — the case log-level
// ordering cannot resolve
const TRADE_64694038_111 =
  "0x0000000048000100042cb7000000000000000013ab6680000000000002e60200000000000000000000000000000fa00000000000000000000000000000003c6e" +
  "0000000048020500042b7400000000000000000a21fe80000000000002e5f80000000000000000002756cd00000fa00000000000000000000000000000003c6f";

describe("TradesPacked", () => {
  it("decodes block 64699421 log 20 to the Data Source row for tradeId 15759", () => {
    expect(decodeTrades(TRADE_64699421_20)).toEqual([
      {
        makerId: 72n,
        slotIdx: 5,
        makerIsBuy: true,
        passive: false,
        matchEnd: true,
        price: 274884n, // api "price": "274884"
        filledSize: 330000000n, // api "filledSize": "330000000"
        orderId: 190503n,
        updatedSize: 0n, // api "remainingSize": "0"
        makerFeePps: 4000, // market makerFeePps
        tradeId: 15759n, // api "tradeId": "15759"
      },
    ]);
  });

  it("keeps two records in one log distinct and in byte order", () => {
    const r = decodeTrades(TRADE_64694038_111);
    expect(r.map((x) => x.tradeId)).toEqual([15470n, 15471n]);
    expect(r.map((x) => x.slotIdx)).toEqual([0, 2]);
    expect(r.map((x) => x.matchEnd)).toEqual([false, true]);
    expect(r[1]!.updatedSize).toBe(660000000n);
  });

  it("fails closed on a length that is not a multiple of 64", () => {
    expect(() => decodeTrades(TRADE_64699421_20 + "00")).toThrow(/% 64/);
  });

  it("fails closed on an unknown flag bit", () => {
    const bad = `0x${TRADE_64699421_20.slice(2, 14)}ff${TRADE_64699421_20.slice(16)}`;
    expect(() => decodeTrades(bad)).toThrow(/unknown flags/);
  });

  it("fails closed on a set reserved bit", () => {
    const bad = `0x${TRADE_64699421_20.slice(2, 64)}01${TRADE_64699421_20.slice(66)}`;
    expect(() => decodeTrades(bad)).toThrow(/reserved bits/);
  });
});

describe("BookUpdatesPacked", () => {
  it("decodes block 64699421 log 10 to the level the gateway served", () => {
    expect(decodeBookUpdates(BOOK_64699421_10)).toEqual([
      {
        userId: 72n,
        slotIdx: 0,
        isBuy: false,
        live: true,
        price: 275400n,
        size: 830000000n, // byte-identical to the gateway's total_base for this level
        orderId: 190504n,
        minSizeAfterBlock: 0n,
        makerFeePps: 4000,
      },
    ]);
  });

  it("reads bit7 as the live flag", () => {
    const del = `0x${BOOK_64699421_10.slice(2, 12)}0000${BOOK_64699421_10.slice(16, 74)}000000`;
    const [r] = decodeBookUpdates(del);
    expect(r!.live).toBe(false);
    expect(r!.orderId).toBe(190504n);
  });

  it("fails closed on a length that is not a multiple of 39", () => {
    expect(() => decodeBookUpdates(BOOK_64699421_10 + "00")).toThrow(/% 39/);
  });

  it("fails closed when a delete record carries order economics", () => {
    const bad = `0x${BOOK_64699421_10.slice(2, 12)}0000${BOOK_64699421_10.slice(16)}`;
    expect(() => decodeBookUpdates(bad)).toThrow(/order economics/);
  });
});
