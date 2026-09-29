export const RANGES = { "1D": 86400e3, "1W": 7 * 86400e3, "1M": 30 * 86400e3, "3M": 90 * 86400e3, "1Y": 365 * 86400e3, ALL: 730 * 86400e3 } as const;
export type Range = keyof typeof RANGES;

// Illustrative history: deterministic random walk per address+range, scaled so the last point equals `total`.
export function dummySeries(address: string, range: Range, total: number, now: number, points = 160) {
  let seed = 2166136261;
  for (const c of address.toLowerCase() + range) seed = Math.imul(seed ^ c.charCodeAt(0), 16777619);
  const rand = () => {
    seed = Math.imul(seed ^ (seed >>> 15), 2246822507) ^ Math.imul(seed ^ (seed >>> 13), 3266489909);
    return ((seed ^= seed >>> 16) >>> 0) / 4294967296;
  };
  const vol = 0.004 * Math.sqrt(RANGES[range] / RANGES["1D"]);
  const walk = [1];
  for (let i = 1; i < points; i++) walk.push(walk[i - 1] * (1 + (rand() - 0.5) * vol));
  const k = total / walk[points - 1];
  const step = RANGES[range] / (points - 1);
  return walk.map((w, i) => ({ t: now - (points - 1 - i) * step, v: w * k }));
}
