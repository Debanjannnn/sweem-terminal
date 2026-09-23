// kuru spot v2 packed payloads. every logical event rides inside one of two
// logs, so log-level ordering is not enough: records are applied in byte order,
// and recordIdx is part of a fill's identity.
//
// fail closed. a bad length, a reserved bit or an unknown flag means every
// later byte offset is a guess, so we throw rather than skip — the indexer
// stopping is recoverable, silently wrong fills are not.

// nibble offsets into a record's hex; all fields are big-endian and byte-aligned
const u = (hex: string, from: number, to: number): bigint => BigInt(`0x${hex.slice(from, to)}`);

const body = (hexData: string): string => {
  if (!hexData.startsWith("0x")) throw new Error(`kuru: payload is not hex: ${hexData}`);
  return hexData.slice(2);
};

export type TradeRecord = {
  makerId: bigint;
  slotIdx: number;
  makerIsBuy: boolean;
  passive: boolean;
  matchEnd: boolean;
  price: bigint;
  filledSize: bigint;
  orderId: bigint;
  updatedSize: bigint;
  makerFeePps: number;
  tradeId: bigint;
};

const TRADE_FLAGS = 0x07;

// 64-byte records: userId(u40) slotIdx(u8) flags(u8) price(u32) fillSize(u96)
// orderId(u64) resv(u8) | updatedSize(u96) makerFeePps(u24) resv(72b) tradeId(u64)
export function decodeTrades(hexData: string): TradeRecord[] {
  const hex = body(hexData);
  if (hex.length % 128 !== 0) throw new Error(`kuru: TradesPacked length ${hex.length / 2} % 64 != 0`);
  const out: TradeRecord[] = [];
  for (let i = 0; i < hex.length; i += 128) {
    const r = hex.slice(i, i + 128);
    if (u(r, 62, 64) !== 0n || u(r, 94, 112) !== 0n)
      throw new Error(`kuru: TradesPacked reserved bits set in record ${i / 128}`);
    const flags = Number(u(r, 12, 14));
    if (flags & ~TRADE_FLAGS) throw new Error(`kuru: TradesPacked unknown flags 0x${flags.toString(16)}`);
    out.push({
      makerId: u(r, 0, 10),
      slotIdx: Number(u(r, 10, 12)),
      makerIsBuy: (flags & 0x01) !== 0,
      passive: (flags & 0x02) !== 0,
      matchEnd: (flags & 0x04) !== 0,
      price: u(r, 14, 22),
      filledSize: u(r, 22, 46),
      orderId: u(r, 46, 62),
      updatedSize: u(r, 64, 88),
      makerFeePps: Number(u(r, 88, 94)),
      tradeId: u(r, 112, 128),
    });
  }
  return out;
}

export type BookRecord = {
  userId: bigint;
  slotIdx: number;
  isBuy: boolean;
  // false = the slot is vacated by this record (cancel or replacement removal)
  live: boolean;
  price: bigint;
  size: bigint;
  orderId: bigint;
  minSizeAfterBlock: bigint;
  makerFeePps: number;
};

const BOOK_FLAGS = 0x81;

// 39-byte records: the 32-byte header above, then minSizeAfterBlock(u32) makerFeePps(u24)
export function decodeBookUpdates(hexData: string): BookRecord[] {
  const hex = body(hexData);
  if (hex.length % 78 !== 0)
    throw new Error(`kuru: BookUpdatesPacked length ${hex.length / 2} % 39 != 0`);
  const out: BookRecord[] = [];
  for (let i = 0; i < hex.length; i += 78) {
    const r = hex.slice(i, i + 78);
    if (u(r, 62, 64) !== 0n)
      throw new Error(`kuru: BookUpdatesPacked reserved bits set in record ${i / 78}`);
    const flags = Number(u(r, 12, 14));
    if (flags & ~BOOK_FLAGS)
      throw new Error(`kuru: BookUpdatesPacked unknown flags 0x${flags.toString(16)}`);
    const live = (flags & 0x80) !== 0;
    const rec: BookRecord = {
      userId: u(r, 0, 10),
      slotIdx: Number(u(r, 10, 12)),
      isBuy: (flags & 0x01) !== 0,
      live,
      price: u(r, 14, 22),
      size: u(r, 22, 46),
      orderId: u(r, 46, 62),
      minSizeAfterBlock: u(r, 64, 72),
      makerFeePps: Number(u(r, 72, 78)),
    };
    // a delete carries no economics; anything there means we mis-read the layout
    if (!live && (rec.minSizeAfterBlock !== 0n || rec.makerFeePps !== 0))
      throw new Error(`kuru: BookUpdatesPacked delete record ${i / 78} carries order economics`);
    out.push(rec);
  }
  return out;
}
