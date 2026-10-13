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

// basis hedge on Perpl: asset → perp (ids from deployments.json)
const PERP_IDS: Record<string, number> = DEPLOYMENTS.perpl.perpIds;
export const PERP: Record<string, { id: number; name: string; maxLev: number }> = {
  WETH: { id: PERP_IDS.ETH, name: "ETH", maxLev: 12 },
  cbBTC: { id: PERP_IDS.BTC, name: "BTC", maxLev: 15 },
  MON: { id: PERP_IDS.MON, name: "MON", maxLev: 10 },
};

const slug = (name: string) => name.toLowerCase().replace(/ /g, "-");
const VAULTS = DEPLOYMENTS.lending.erc4626.map((v) => ({ venue: slug(v.name), name: v.name, asset: v.asset, protocol: v.protocol }));

// venue id → display name + protocol (ProtocolIcon key); "perpl" basis legs trade spot on Kuru and short on Perpl
const VENUES: Record<string, { name: string; protocol: string }> = {
  ...Object.fromEntries(VAULTS.map((v) => [v.venue, { name: v.name, protocol: v.protocol }])),
  aave: { name: "Aave V3", protocol: "aave" },
  neverland: { name: "Neverland", protocol: "neverland" },
  shmonad: { name: "shMonad", protocol: "shmonad" },
  "kuru-activevault": { name: "Kuru ActiveVault", protocol: "kuru" },
  perpl: { name: "Kuru + Perpl", protocol: "perpl" },
};
export const protocolOf = (venue: string) => VENUES[venue]?.protocol ?? venue;
export const venueName = (venue: string) => VENUES[venue]?.name ?? venue;

export function lendingVenues(asset: string): { venue: string; name: string }[] {
  const a = lendAsset(asset);
  const out: { venue: string; name: string }[] = [];
  if (a in DEPLOYMENTS.lending.aaveV3.aTokens) out.push({ venue: "aave", name: VENUES.aave.name });
  if (a in DEPLOYMENTS.lending.neverland.aTokens) out.push({ venue: "neverland", name: VENUES.neverland.name });
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

// Σ weight × apy; null = no apy anywhere
const weighted = <T extends { weightBps: number }>(xs: T[], apy: (x: T) => number | null) => {
  const ps = xs.map(apy);
  return ps.every((p) => p === null) ? null : ps.reduce<number>((a, p, i) => a + (p ?? 0) * (xs[i].weightBps / TOTAL_BPS), 0);
};

// Σ sleeve weight × Σ source weight × live rate
export const blendedApy = (sleeves: Sleeve[], rates: Rates | null | undefined) =>
  weighted(sleeves, (sl) => weighted(sl.sources, (s) => sourceApy(s, sl.asset, rates)));

// api estimate when present, else blended from live rates
export const liveApy = (s: Strategy, rates: Rates | null | undefined) => (s.apy?.estimate != null ? Number(s.apy.estimate) : blendedApy(parts(s), rates));

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
