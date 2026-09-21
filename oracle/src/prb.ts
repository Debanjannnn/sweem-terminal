// prb-math v4.2.0 ported to bigint, line for line from
// contracts/lib/prb-math/src/{Common.sol, ud60x18/Math.sol, sd59x18/Math.sol}.
// these values settle on-chain against prb-math, so bit-exact agreement is the
// requirement: solidity truncation, floor shifts and overflow reverts are all
// reproduced. no floating point — `number` appears only as a bit index.

const U256_MAX = (1n << 256n) - 1n;

export const UNIT = 1_000_000_000_000_000_000n; // uUNIT
export const WAD = UNIT;
// 1e36. divisors of this form move a value back to WAD after a WAD*WAD product;
// ud.inv/ud.pow rely on it, so it is not interchangeable with UNIT*UNIT rounding.
export const UNIT_SQUARED = 1_000_000_000_000_000_000_000_000_000_000_000_000n;
export const HALF_UNIT = 500_000_000_000_000_000n;
export const LOG2_E = 1_442695040888963407n;
export const EXP_MAX_INPUT = 133_084258667509499440n;
export const EXP2_MAX_INPUT = 192n * UNIT - 1n;
export const MAX_UD60x18 = U256_MAX;
export const MAX_SD59x18 = (1n << 255n) - 1n;

export class PRBMathError extends Error {
  constructor(name: string, detail = "") {
    super(detail ? `${name}: ${detail}` : name);
    this.name = name;
  }
}

const requireUint256 = (x: bigint, where: string): bigint => {
  if (x < 0n || x > U256_MAX) throw new PRBMathError("PRBMath_Overflow", `${where} left uint256 range`);
  return x;
};

export function msb(x: bigint): bigint {
  let v = requireUint256(x, "msb");
  let r = 0n;
  for (const f of [128n, 64n, 32n, 16n, 8n, 4n, 2n]) {
    if (v > (1n << f) - 1n) {
      v >>= f;
      r |= f;
    }
  }
  if (v > 1n) r |= 1n;
  return r;
}

export function mulDiv(x: bigint, y: bigint, denominator: bigint): bigint {
  if (denominator === 0n) throw new PRBMathError("PRBMath_MulDiv_Overflow", "zero denominator");
  const p = x * y;
  const prod1 = p >> 256n;
  if (prod1 !== 0n && prod1 >= denominator) {
    throw new PRBMathError("PRBMath_MulDiv_Overflow", `${x}*${y}/${denominator}`);
  }
  return p / denominator;
}

export function mulDiv18(x: bigint, y: bigint): bigint {
  const p = x * y;
  const prod1 = p >> 256n;
  if (prod1 !== 0n && prod1 >= UNIT) {
    throw new PRBMathError("PRBMath_MulDiv18_Overflow", `${x}*${y}`);
  }
  return p / UNIT;
}

const EXP2_FACTORS: readonly bigint[] = [
  0x16A09E667F3BCC909n,
  0x1306FE0A31B7152DFn,
  0x1172B83C7D517ADCEn,
  0x10B5586CF9890F62An,
  0x1059B0D31585743AEn,
  0x102C9A3E778060EE7n,
  0x10163DA9FB33356D8n,
  0x100B1AFA5ABCBED61n,
  0x10058C86DA1C09EA2n,
  0x1002C605E2E8CEC50n,
  0x100162F3904051FA1n,
  0x1000B175EFFDC76BAn,
  0x100058BA01FB9F96Dn,
  0x10002C5CC37DA9492n,
  0x1000162E525EE0547n,
  0x10000B17255775C04n,
  0x1000058B91B5BC9AEn,
  0x100002C5C89D5EC6Dn,
  0x10000162E43F4F831n,
  0x100000B1721BCFC9An,
  0x10000058B90CF1E6En,
  0x1000002C5C863B73Fn,
  0x100000162E430E5A2n,
  0x1000000B172183551n,
  0x100000058B90C0B49n,
  0x10000002C5C8601CCn,
  0x1000000162E42FFF0n,
  0x10000000B17217FBBn,
  0x1000000058B90BFCEn,
  0x100000002C5C85FE3n,
  0x10000000162E42FF1n,
  0x100000000B17217F8n,
  0x10000000058B90BFCn,
  0x1000000002C5C85FEn,
  0x100000000162E42FFn,
  0x1000000000B17217Fn,
  0x100000000058B90C0n,
  0x10000000002C5C860n,
  0x1000000000162E430n,
  0x10000000000B17218n,
  0x1000000000058B90Cn,
  0x100000000002C5C86n,
  0x10000000000162E43n,
  0x100000000000B1721n,
  0x10000000000058B91n,
  0x1000000000002C5C8n,
  0x100000000000162E4n,
  0x1000000000000B172n,
  0x100000000000058B9n,
  0x10000000000002C5Dn,
  0x1000000000000162En,
  0x10000000000000B17n,
  0x1000000000000058Cn,
  0x100000000000002C6n,
  0x10000000000000163n,
  0x100000000000000B1n,
  0x10000000000000059n,
  0x1000000000000002Cn,
  0x10000000000000016n,
  0x1000000000000000Bn,
  0x10000000000000006n,
  0x10000000000000003n,
  0x10000000000000001n,
  0x10000000000000001n,
] as const;

export function commonExp2(x: bigint): bigint {
  // Start from 0.5 in 192.64 format.
  let result = 0x800000000000000000000000000000000000000000000000n;
  for (let i = 0; i < 64; i++) {
    const bit = 63n - BigInt(i);
    if ((x >> bit) & 1n) {
      result = (result * (EXP2_FACTORS[i] as bigint)) >> 64n;
    }
  }
  // 191 rather than 192 absorbs the initial 0.5 guess.
  // 191 rather than 192 absorbs the initial 0.5 guess.
  const shift = 191n - (x >> 64n);
  if (shift < 0n) throw new PRBMathError("PRBMath_Exp2_InputTooBig", `${x}`);
  result *= UNIT;
  return requireUint256(result >> shift, "exp2");
}

export function commonSqrt(x: bigint): bigint {
  requireUint256(x, "sqrt");
  if (x === 0n) return 0n;

  let xAux = x;
  let result = 1n;
  for (const [bits, shift] of [
    [128n, 64n],
    [64n, 32n],
    [32n, 16n],
    [16n, 8n],
    [8n, 4n],
    [4n, 2n],
  ] as const) {
    if (xAux >= 1n << bits) {
      xAux >>= bits;
      result <<= shift;
    }
  }
  if (xAux >= 4n) result <<= 1n;

  for (let i = 0; i < 7; i++) result = (result + x / result) >> 1n;

  // prb-math's final round-toward-zero correction; babylonian alone differs by 1 wei.
  const rounded = x / result;
  return result >= rounded ? rounded : result;
}

export const ud = {
  mul: (x: bigint, y: bigint): bigint => mulDiv18(x, y),

  div: (x: bigint, y: bigint): bigint => mulDiv(x, UNIT, y),

  inv: (x: bigint): bigint => {
    if (x === 0n) throw new PRBMathError("PRBMath_UD60x18_Inv_InputTooSmall");
    return UNIT_SQUARED / x;
  },

  log2: (x: bigint): bigint => {
    if (x < UNIT) throw new PRBMathError("PRBMath_UD60x18_Log_InputTooSmall", `${x}`);
    const n = msb(x / UNIT);
    let result = n * UNIT;
    let y = x >> n;
    if (y === UNIT) return result;
    const doubleUnit = 2n * UNIT;
    for (let delta = HALF_UNIT; delta > 0n; delta >>= 1n) {
      y = (y * y) / UNIT;
      if (y >= doubleUnit) {
        result += delta;
        y >>= 1n;
      }
    }
    return result;
  },

  ln: (x: bigint): bigint => (ud.log2(x) * UNIT) / LOG2_E,

  exp2: (x: bigint): bigint => {
    if (x > EXP2_MAX_INPUT) throw new PRBMathError("PRBMath_UD60x18_Exp2_InputTooBig", `${x}`);
    return commonExp2((x << 64n) / UNIT);
  },

  exp: (x: bigint): bigint => {
    if (x > EXP_MAX_INPUT) throw new PRBMathError("PRBMath_UD60x18_Exp_InputTooBig", `${x}`);
    return ud.exp2((x * LOG2_E) / UNIT);
  },

  sqrt: (x: bigint): bigint => {
    if (x > MAX_UD60x18 / UNIT) throw new PRBMathError("PRBMath_UD60x18_Sqrt_Overflow", `${x}`);
    return commonSqrt(x * UNIT);
  },

  powu: (x: bigint, y: bigint): bigint => {
    let xu = x;
    let result = y & 1n ? xu : UNIT;
    for (let e = y >> 1n; e > 0n; e >>= 1n) {
      xu = mulDiv18(xu, xu);
      if (e & 1n) result = mulDiv18(result, xu);
    }
    return result;
  },

  // the x < UNIT branch inverts first so log2's domain holds; that asymmetry moves
  // the last few wei, so it is reproduced verbatim.
  pow: (x: bigint, y: bigint): bigint => {
    if (x === 0n) return y === 0n ? UNIT : 0n;
    if (x === UNIT) return UNIT;
    if (y === 0n) return UNIT;
    if (y === UNIT) return x;

    if (x > UNIT) return ud.exp2(ud.mul(ud.log2(x), y));

    const i = UNIT_SQUARED / x;
    const w = ud.exp2(ud.mul(ud.log2(i), y));
    if (w === 0n) throw new PRBMathError("PRBMath_UD60x18_Pow_Underflow", `${x}^${y}`);
    return UNIT_SQUARED / w;
  },
} as const;

export const sd = {
  log2: (x: bigint): bigint => {
    if (x <= 0n) throw new PRBMathError("PRBMath_SD59x18_Log_InputTooSmall", `${x}`);
    let xi = x;
    let signFactor: bigint;
    if (xi >= UNIT) {
      signFactor = 1n;
    } else {
      signFactor = -1n;
      xi = UNIT_SQUARED / xi;
    }
    const n = msb(xi / UNIT);
    let result = n * UNIT;
    let y = xi >> n;
    if (y === UNIT) return result * signFactor;
    const doubleUnit = 2n * UNIT;
    for (let delta = HALF_UNIT; delta > 0n; delta >>= 1n) {
      y = (y * y) / UNIT;
      if (y >= doubleUnit) {
        result += delta;
        y >>= 1n;
      }
    }
    return result * signFactor;
  },

  ln: (x: bigint): bigint => (sd.log2(x) * UNIT) / LOG2_E,

  div: (x: bigint, y: bigint): bigint => {
    if (y === 0n) throw new PRBMathError("PRBMath_SD59x18_Div_InputTooSmall", "zero denominator");
    const sign = (x < 0n ? -1n : 1n) * (y < 0n ? -1n : 1n);
    const abs = mulDiv(x < 0n ? -x : x, UNIT, y < 0n ? -y : y);
    return sign * abs;
  },

  sqrt: (x: bigint): bigint => {
    if (x < 0n) throw new PRBMathError("PRBMath_SD59x18_Sqrt_NegativeInput", `${x}`);
    if (x > MAX_SD59x18 / UNIT) throw new PRBMathError("PRBMath_SD59x18_Sqrt_Overflow", `${x}`);
    return commonSqrt(x * UNIT);
  },
} as const;
