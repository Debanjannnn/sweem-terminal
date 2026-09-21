// the 6 -> 18 decimal boundary. everything stored is WAD; conversion happens
// here and nowhere else.
export const WAD = 10n ** 18n;

export const USDC_DECIMALS = 6;

// SweemVault overrides _decimalsOffset() to 12 on a 6-decimal asset, so shares
// are already 18dp and sharesToWad is the identity. explicit so the assumption
// is visible; if the offset changes, this constant changes.
export const SHARE_DECIMALS = 18;

export const SECONDS_PER_YEAR = 365n * 24n * 60n * 60n;

// a left shift, so exact for every input
export const toWad = (amount: bigint, decimals: number): bigint =>
  decimals === 18 ? amount : amount * 10n ** BigInt(18 - decimals);

export const usdcToWad = (a: bigint) => toWad(a, USDC_DECIMALS);
export const sharesToWad = (a: bigint) => toWad(a, SHARE_DECIMALS);

export const mulWad = (a: bigint, b: bigint): bigint => (a * b) / WAD;

// zero denominator yields zero — the genesis case, not a fault
export const divWad = (a: bigint, b: bigint): bigint => (b === 0n ? 0n : (a * WAD) / b);

// per-second WAD rate -> annualized. 1e18 == 100%/yr.
export const toApr = (ratePerSecond: bigint): bigint => ratePerSecond * SECONDS_PER_YEAR;

export const abs = (x: bigint): bigint => (x < 0n ? -x : x);
