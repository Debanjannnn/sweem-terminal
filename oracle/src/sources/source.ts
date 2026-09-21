export interface Sample {
  // WAD
  price: bigint;
  // as reported by the source, not by us: staleness is measured on this
  sourceTs: number;
  roundId?: bigint;
}

export interface PriceSource {
  readonly name: "pyth" | "chainlink";
  read(): Promise<Sample>;
}

export function assertFresh(name: string, s: Sample, staleMs: number, now = Date.now()): Sample {
  const age = now - s.sourceTs;
  if (age > staleMs) throw new Error(`${name}: stale sample, ${age}ms old (max ${staleMs}ms)`);
  return s;
}
