import { DEPLOYMENTS } from "@/lib/config";
import type { BacktestBody } from "@/lib/api";
import type { Rates, Rules, Source, SourceKind } from "@/lib/types";

export const ASSETS = ["USDC", "WETH", "cbBTC", "MON", "XAUt0"] as const;
export const LP_ASSUMED_APR = 0.13;

// basis hedge on Perpl: asset → perp
export const PERP: Record<string, { id: number; name: string; maxLev: number }> = {
  WETH: { id: 20, name: "ETH", maxLev: 12 },
  cbBTC: { id: 1, name: "BTC", maxLev: 15 },
  MON: { id: 10, name: "MON", maxLev: 10 },
};

const slug = (name: string) => name.toLowerCase().replace(/ /g, "-");
const VAULTS = DEPLOYMENTS.lending.erc4626.map((v) => ({ venue: slug(v.name), name: v.name, asset: v.asset }));

// MON lending would need WMON wrapping, which the executor lacks (api: unsupported_source)
export function lendingVenues(asset: string): { venue: string; name: string }[] {
  if (asset === "MON") return [];
  const out: { venue: string; name: string }[] = [];
  if (asset in DEPLOYMENTS.lending.aaveV3.aTokens) out.push({ venue: "aave", name: "Aave V3" });
  if (asset in DEPLOYMENTS.lending.neverland.aTokens) out.push({ venue: "neverland", name: "Neverland" });
  return [...out, ...VAULTS.filter((v) => v.asset === asset)];
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
  const r = rates.rates.find((x) => x.venue === s.venue && (s.kind === "staking" || x.asset === asset));
  return r ? Number(r.apy) : null;
}

export const blendedApy = (sources: Source[], asset: string, rates: Rates | null | undefined) => {
  const parts = sources.map((s) => sourceApy(s, asset, rates));
  return parts.every((p) => p === null) ? null : parts.reduce<number>((a, p, i) => a + (p ?? 0) * (sources[i].weightBps / 10000), 0);
};

// strategy → history POST /backtest body
export function toBacktest(asset: string, sources: Source[], rules: Rules, amount: string, from?: string): BacktestBody {
  const basis = sources.find((s) => s.kind === "basis");
  return {
    asset,
    amount,
    from,
    legs: sources.map((s) =>
      s.kind === "basis"
        ? { kind: "basis", perpId: PERP[asset]?.id, weight: String(s.weightBps / 10000) }
        : { kind: s.kind, venue: s.venue, weight: String(s.weightBps / 10000) },
    ),
    rules: {
      driftBps: String(rules.driftBps ?? 500),
      minIntervalDays: Math.round((rules.minRebalanceSeconds ?? 86400) / 86400),
      ...(rules.fundingGate ? { fundingGate: rules.fundingGate } : {}),
      ...(basis ? { basisLeverage: String(leverageOf(basis)) } : {}),
    },
  };
}
