import { UNIT, mulDiv18, sd, ud } from "./prb.ts";

// 365d. contracts/src/libraries/Accrual.sol must use the same literal or the two
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

// this value *is* the velocity market's index
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

// (P/P0)^k. negative k is expressed as (P0/P)^|k| exactly as PowerIndex.sol does,
// because inverting afterwards rounds differently.
export function powerIndex(price: bigint, price0: bigint, k: bigint): bigint {
  if (price === 0n || price0 === 0n) throw new RangeError("PowerIndex: ZeroPrice");
  if (k === 0n) return UNIT;
  const ratio = k > 0n ? ud.div(price, price0) : ud.div(price0, price);
  return ud.pow(ratio, k > 0n ? k : -k);
}

// k(k-1)/2 * sigma^2, off the same tick's sigma — that coupling is why velocity and
// power ship together. signed: negative for 0 < k < 1.
export function powerFundingTerm(k: bigint, sigma: bigint): bigint {
  const kk = k * (k - UNIT); // WAD^2
  const sigmaSquared = mulDiv18(sigma, sigma);
  const half = kk / (2n * UNIT); // back to WAD
  const abs = half < 0n ? -half : half;
  const term = mulDiv18(abs, sigmaSquared);
  return half < 0n ? -term : term;
}

// |next - last| / last. undefined against a zero baseline, so treated as 0.
export function deviation(next: bigint, last: bigint): bigint {
  if (last === 0n) return 0n;
  const d = next > last ? next - last : last - next;
  return ud.div(d, last < 0n ? -last : last);
}
