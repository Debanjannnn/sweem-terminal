import { TOTAL_BPS } from "@/lib/config";

// n equal bps weights summing to 10000; remainder goes to the first legs
export const evenSplit = (n: number): number[] =>
  Array.from({ length: n }, (_, i) => Math.floor(TOTAL_BPS / n) + (i < TOTAL_BPS % n ? 1 : 0));

// "12.5" → 1250; null when not a 0–100 percent with ≤2 decimals
export function pctToBps(s: string): number | null {
  if (!/^\d{1,3}(\.\d{0,2})?$/.test(s.trim())) return null;
  const bps = Math.round(Number(s) * 100);
  return bps <= TOTAL_BPS ? bps : null;
}

// sets w[i] to bps, capped at what the other legs leave of 100%
export const setWeight = (w: number[], i: number, bps: number): number[] => {
  const others = w.reduce((a, b, j) => (j === i ? a : a + b), 0);
  return w.map((x, j) => (j === i ? Math.max(0, Math.min(bps, TOTAL_BPS - others)) : x));
};

export const weightsError = (w: number[]): string | null => {
  if (w.length === 0) return "Pick at least one market";
  if (w.some((x) => x <= 0)) return "Every weight must be above 0%";
  const sum = w.reduce((a, b) => a + b, 0);
  return sum === TOTAL_BPS ? null : `Weights sum to ${(sum / 100).toFixed(2)}%, need 100%`;
};
