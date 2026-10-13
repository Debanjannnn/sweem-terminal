import { DEPLOYMENTS, FEE_BPS, TOTAL_BPS } from "@/lib/config";
import { marketOf } from "@/lib/tokens";
import type { BacktestBody, BacktestLeg } from "@/lib/api";
import type { Rates, Rules, Sleeve, Source, SourceKind, Strategy } from "@/lib/types";

export const ASSETS = ["USDC", "WETH", "cbBTC", "MON", "XAUt0"] as const;
export const LP_ASSUMED_APR = 0.13;
export const MAX_TOKENS = 5;

// strategy → its token parts; single-token rows are one sleeve at 100%
export const parts = (s: Pick<Strategy, "asset" | "sources" | "sleeves">): Sleeve[] =>
  s.sleeves?.length ? s.sleeves : [{ asset: s.asset, weightBps: TOTAL_BPS, sources: s.sources }];

// MON is lent as WMON (planner wraps)
const lendAsset = (asset: string) => (asset === "MON" ? "WMON" : asset);

// basis hedge on Perpl: asset → perp
export const PERP: Record<string, { id: number; name: string; maxLev: number }> = {
  WETH: { id: 20, name: "ETH", maxLev: 12 },
  cbBTC: { id: 1, name: "BTC", maxLev: 15 },
  MON: { id: 10, name: "MON", maxLev: 10 },
};

const slug = (name: string) => name.toLowerCase().replace(/ /g, "-");
const VAULTS = DEPLOYMENTS.lending.erc4626.map((v) => ({ venue: slug(v.name), name: v.name, asset: v.asset, protocol: v.protocol }));

// venue id → protocol (ProtocolIcon key); "perpl" basis legs trade spot on Kuru and short on Perpl
export const protocolOf = (venue: string) =>
  ({ aave: "aave", neverland: "neverland", shmonad: "shmonad", "kuru-activevault": "kuru", perpl: "perpl" })[venue] ??
  VAULTS.find((v) => v.venue === venue)?.protocol ??
  venue;

export function lendingVenues(asset: string): { venue: string; name: string }[] {
  const a = lendAsset(asset);
  const out: { venue: string; name: string }[] = [];
  if (a in DEPLOYMENTS.lending.aaveV3.aTokens) out.push({ venue: "aave", name: "Aave V3" });
  if (a in DEPLOYMENTS.lending.neverland.aTokens) out.push({ venue: "neverland", name: "Neverland" });
  return [...out, ...VAULTS.filter((v) => v.asset === a)];
}

export const KIND_LABEL: Record<SourceKind, string> = { lending: "Lending", staking: "Staking", lp: "Liquidity", basis: "Funding basis" };

export function kindsFor(asset: string): SourceKind[] {
  const k: SourceKind[] = [];
  if (lendingVenues(asset).length) k.push("lending");
  if (asset === "MON") k.push("staking", "lp");
  if (PERP[asset]) k.push("basis");
  return k;
}

export const defaultVenue = (kind: SourceKind, asset: string) =>
  kind === "lending" ? (lendingVenues(asset)[0]?.venue ?? "") : kind === "staking" ? "shmonad" : kind === "lp" ? "kuru-activevault" : "perpl";

export const venueName = (venue: string) =>
  ({ aave: "Aave V3", neverland: "Neverland", shmonad: "shMonad", "kuru-activevault": "Kuru ActiveVault", perpl: "Kuru + Perpl" })[venue] ??
  VAULTS.find((v) => v.venue === venue)?.name ??
  venue;

export const leverageOf = (s: Source) => Number(s.params?.leverage ?? 2);

// live APY per source; null = no rate. basis = trailing-7d short APR × L/(L+1) (spot share earns funding)
export function sourceApy(s: Source, asset: string, rates: Rates | null | undefined): number | null {
  if (!rates) return null;
  if (s.kind === "lp") return LP_ASSUMED_APR;
  if (s.kind === "basis") {
    const f = rates.funding.find((x) => x.perpId === PERP[asset]?.id);
    const L = leverageOf(s);
    return f ? (Number(f.aprShort7d) * L) / (L + 1) : null;
  }
  const r = rates.rates.find((x) => x.venue === s.venue && (s.kind === "staking" || x.asset === lendAsset(asset)));
  return r ? Number(r.apy) : null;
}

const sleeveApy = (sl: Sleeve, rates: Rates | null | undefined) => {
  const ps = sl.sources.map((s) => sourceApy(s, sl.asset, rates));
  return ps.every((p) => p === null) ? null : ps.reduce<number>((a, p, i) => a + (p ?? 0) * (sl.sources[i].weightBps / TOTAL_BPS), 0);
};

// Σ sleeve weight × Σ source weight × live rate; null = no rate anywhere
export const blendedApy = (sleeves: Sleeve[], rates: Rates | null | undefined) => {
  const ps = sleeves.map((sl) => sleeveApy(sl, rates));
  return ps.every((p) => p === null) ? null : ps.reduce<number>((a, p, i) => a + (p ?? 0) * (sleeves[i].weightBps / TOTAL_BPS), 0);
};

const ruleBody = (rules: Rules, basis?: Source) => ({
  driftBps: String(rules.driftBps ?? 500),
  minIntervalDays: Math.round((rules.minRebalanceSeconds ?? 86400) / 86400),
  ...(rules.fundingGate ? { fundingGate: rules.fundingGate } : {}),
  ...(basis ? { basisLeverage: String(leverageOf(basis)) } : {}),
});

// history takes raw venue ids (series_rate venue = aave | neverland | vault slug); lp = kuru-activevault
function sleeveBody(sl: Sleeve, rules: Rules, amount: string) {
  const legs: BacktestLeg[] = sl.sources.map((s) =>
    s.kind === "basis"
      ? { kind: "basis", perpId: PERP[sl.asset]?.id, weight: String(s.weightBps / TOTAL_BPS) }
      : { kind: s.kind, venue: s.venue, weight: String(s.weightBps / TOTAL_BPS) },
  );
  return { asset: sl.asset, amount, legs, rules: ruleBody(rules, sl.sources.find((s) => s.kind === "basis")) };
}

// strategy → history POST /backtest body. 1 sleeve: amount in asset units (today's body); more: amount in USD
export function toBacktest(sleeves: Sleeve[], rules: Rules, amount: string, from?: string): BacktestBody {
  if (sleeves.length === 1) return { ...sleeveBody(sleeves[0], rules, amount), from };
  return {
    amount,
    from,
    sleeves: sleeves.map((sl) => ({ ...sleeveBody(sl, rules, "1"), weight: String(sl.weightBps / TOTAL_BPS) })),
    rules: ruleBody(rules),
  };
}

// share of a sleeve's USDC that market-buys the token: lp buys half, basis buys spot L/(L+1)
const buyShare = (sl: Sleeve) =>
  sl.sources.reduce((a, s) => {
    const L = leverageOf(s);
    return a + (s.weightBps / TOTAL_BPS) * (s.kind === "lp" ? 0.5 : s.kind === "basis" ? L / (L + 1) : 1);
  }, 0);

// smallest USDC deposit whose per-token buy clears the Kuru min order (+5% buffer, after open fee).
// Mirrors planner minDeposit; UI uses ticker mid, server check is authoritative. null = price unknown.
export function minDeposit(sleeves: Sleeve[], prices: Record<string, number | null | undefined>) {
  const per: Record<string, number | null> = {};
  for (const sl of sleeves) {
    const m = marketOf(sl.asset);
    const share = buyShare(sl) * (sl.weightBps / TOTAL_BPS) * (1 - FEE_BPS / TOTAL_BPS);
    const px = prices[sl.asset];
    per[sl.asset] = sl.asset === "USDC" || !m || share <= 0 ? 0 : px ? (m.minSize * px * 1.05) / share : null;
  }
  const vals = Object.values(per);
  return { per, total: vals.some((v) => v === null) ? null : Math.max(0, ...(vals as number[])) };
}
