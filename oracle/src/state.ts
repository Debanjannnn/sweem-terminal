import { type OracleEnv, powerMarketId, volMarketId } from "./config.ts";
import {
  type EwmaState,
  annualizedSigma,
  initialEwma,
  powerFundingTerm,
  powerIndex,
  samplesPerYear,
  stepEwma,
} from "./index-math.ts";
import type { Sample } from "./sources/source.ts";

export interface Feed {
  marketId: string;
  kind: "vol" | "power";
  // WAD
  value: bigint;
  // power only, signed WAD
  exponent?: bigint;
  // power only: k(k-1)/2 * sigma^2 off the same tick's sigma
  fundingTerm?: bigint;
}

// pure transitions, so the index pipeline is testable without chain, bus or db
export class IndexState {
  ewma: EwmaState = initialEwma();
  price0: bigint | null;
  lastSample: Sample | null = null;
  private readonly perYear: bigint;

  constructor(private readonly env: OracleEnv) {
    this.price0 = env.ORACLE_PRICE0;
    this.perYear = samplesPerYear(env.ORACLE_SAMPLE_INTERVAL_MS);
  }

  // from persisted history, so a restart is not a cold start and fakes no vol spike
  seed(prices: readonly bigint[]): void {
    for (const p of prices) this.ewma = stepEwma(this.ewma, p, this.env.ORACLE_LAMBDA);
    if (this.price0 === null && prices.length > 0) this.price0 = prices[0] ?? null;
  }

  ingest(sample: Sample): void {
    this.ewma = stepEwma(this.ewma, sample.price, this.env.ORACLE_LAMBDA);
    this.price0 ??= sample.price;
    this.lastSample = sample;
  }

  get warm(): boolean {
    return this.ewma.samples >= this.env.ORACLE_WARMUP_SAMPLES;
  }

  get sigma(): bigint {
    return annualizedSigma(this.ewma.variance, this.perYear);
  }

  // vol first: the power feeds carry a funding term derived from it
  feeds(): Feed[] {
    if (!this.warm || this.price0 === null || this.lastSample === null) return [];
    const sigma = this.sigma;
    const price = this.lastSample.price;
    const out: Feed[] = [{ marketId: volMarketId(this.env.ORACLE_UNDERLYING), kind: "vol", value: sigma }];
    for (const k of this.env.ORACLE_POWER_EXPONENTS) {
      out.push({
        marketId: powerMarketId(this.env.ORACLE_UNDERLYING, k),
        kind: "power",
        value: powerIndex(price, this.price0, k),
        exponent: k,
        fundingTerm: powerFundingTerm(k, sigma),
      });
    }
    return out;
  }
}
