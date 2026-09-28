import { parseUnits } from "viem";
import type { Position, Transfer } from "@/lib/types";

export type Pnl = { basket: string; costRaw: bigint; valueRaw: bigint; pnlRaw: bigint; mintedRaw: bigint };

// USDC raw 6dp. cost = minted − redeemed (net invested); pnl = value − cost, so realized redeems count too.
export function pnlByBasket(history: readonly Transfer[], positions: readonly Position[]): Map<string, Pnl> {
  const out = new Map<string, Pnl>();
  const get = (basket: string) => {
    let p = out.get(basket);
    if (!p) out.set(basket, (p = { basket, costRaw: BigInt(0), valueRaw: BigInt(0), pnlRaw: BigInt(0), mintedRaw: BigInt(0) }));
    return p;
  };
  for (const t of history) {
    const p = get(t.basket);
    const usdc = BigInt(t.usdcRaw);
    if (t.kind === "mint") {
      p.costRaw += usdc;
      p.mintedRaw += usdc;
    } else p.costRaw -= usdc;
  }
  for (const pos of positions) if (pos.valueUsdc) get(pos.basket).valueRaw = parseUnits(pos.valueUsdc, 6);
  for (const p of out.values()) p.pnlRaw = p.valueRaw - p.costRaw;
  return out;
}
