import { UNIT, mulDiv18, sd, ud } from "./prb.ts";

// 365d. any contract annualizing a rate must use the same literal or the two
// annualizations disagree by 0.14%.
export const SECONDS_PER_YEAR = 31_536_000n;

export const samplesPerYear = (intervalMs: number): bigint =>
  (SECONDS_PER_YEAR * 1000n * UNIT) / BigInt(intervalMs);

export interface EwmaState {
  variance: bigint;
  lastPrice: bigint | null;
  samples: number;
}

export const initialEwma = (): EwmaState => ({ variance: 0n, lastPrice: null, samples: 0 });

export const logReturn = (price: bigint, prev: bigint): bigint => sd.ln(sd.div(price, prev));

export function ewmaVariance(prevVariance: bigint, lambda: bigint, r: bigint): bigint {
  if (lambda < 0n || lambda > UNIT) throw new RangeError(`lambda out of [0,1]: ${lambda}`);
  const abs = r < 0n ? -r : r;
  const rSquared = mulDiv18(abs, abs);
  return mulDiv18(lambda, prevVariance) + mulDiv18(UNIT - lambda, rSquared);
}

// annualized realized vol, WAD. what protection is priced off.
export const annualizedSigma = (variance: bigint, perYear: bigint): bigint =>
  ud.sqrt(mulDiv18(variance, perYear));

// the first sample only seeds lastPrice; there is no return yet
export function stepEwma(state: EwmaState, price: bigint, lambda: bigint): EwmaState {
  if (price <= 0n) throw new RangeError(`non-positive price: ${price}`);
  if (state.lastPrice === null) return { variance: 0n, lastPrice: price, samples: 1 };
  return {
    variance: ewmaVariance(state.variance, lambda, logReturn(price, state.lastPrice)),
    lastPrice: price,
    samples: state.samples + 1,
  };
}

// |next - last| / last. undefined against a zero baseline, so treated as 0.
export function deviation(next: bigint, last: bigint): bigint {
  if (last === 0n) return 0n;
  const d = next > last ? next - last : last - next;
  return ud.div(d, last < 0n ? -last : last);
}
