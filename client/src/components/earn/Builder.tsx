"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { createStrategy, errText, runBacktest } from "@/lib/api";
import type { Backtest, Rules, Source, SourceKind } from "@/lib/types";
import { useRates } from "@/lib/hooks";
import { useSession } from "@/lib/session";
import { Button, Divider, ErrorBox, Label, Notice, Panel } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { Field, inputCls } from "@/components/automation/ScheduleForm";
import { evenSplit, setWeight, weightsError } from "@/components/create/weights";
import { cn } from "@/lib/utils";
import { AllocationDonut, BacktestView, pct } from "./Backtest";
import { ASSETS, blendedApy, defaultVenue, KIND_LABEL, kindsFor, lendingVenues, PERP, sourceApy, toBacktest } from "./sources";

type Pick = { kind: SourceKind; venue: string; bps: number; leverage: string };

const HINT: Record<SourceKind, string> = {
  lending: "Supply to a money market / ERC-4626 vault. Instant exit.",
  staking: "Stake MON for shMON. Instant redeem, ~0.9% haircut.",
  lp: "Kuru ActiveVault MON/USDC. Estimated yield, 4-day lock.",
  basis: "Hold spot on Kuru + short on Perpl (AUSD collateral). Earns funding.",
};

export function StrategyBuilder() {
  const router = useRouter();
  const { ready, authenticated, login } = useSession();
  const { rates } = useRates();
  const [name, setName] = useState("");
  const [asset, setAsset] = useState<string>("USDC");
  const [picks, setPicks] = useState<Pick[]>([{ kind: "lending", venue: "aave", bps: 10000, leverage: "2" }]);
  const [drift, setDrift] = useState("5");
  const [minHours, setMinHours] = useState("24");
  const [gateOn, setGateOn] = useState(true);
  const [gateApr, setGateApr] = useState("5");
  const [gateDays, setGateDays] = useState("7");
  const [bt, setBt] = useState<{ data: Backtest | null; error: string | null; loading: boolean }>({ data: null, error: null, loading: false });
  const [busy, setBusy] = useState<"public" | "private" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const kinds = kindsFor(asset);
  const hasBasis = picks.some((p) => p.kind === "basis");
  const sources: Source[] = picks.map((p) => ({
    kind: p.kind,
    venue: p.venue,
    weightBps: p.bps,
    ...(p.kind === "basis" ? { params: { leverage: p.leverage } } : {}),
  }));
  const rules: Rules = {
    driftBps: Math.round(Number(drift) * 100),
    minRebalanceSeconds: Math.round(Number(minHours) * 3600),
    fundingGate: hasBasis && gateOn ? { apr: String(Number(gateApr) / 100), lookbackDays: Math.round(Number(gateDays)) } : null,
  };
  const basis = picks.find((p) => p.kind === "basis");
  const levErr = basis && !(Number(basis.leverage) >= 1 && Number(basis.leverage) <= PERP[asset].maxLev) ? `Leverage must be 1–${PERP[asset].maxLev}×` : null;
  const ruleErr =
    !(Number(drift) > 0) || !(Number(minHours) >= 0) || (hasBasis && gateOn && !(Number(gateApr) >= 0 && Number(gateDays) >= 1)) ? "Check the rules" : null;
  const invalid = weightsError(picks.map((p) => p.bps)) ?? levErr ?? ruleErr;

  const resplit = (next: Omit<Pick, "bps">[]) => {
    const w = evenSplit(next.length);
    setPicks(next.map((p, i) => ({ ...p, bps: w[i] })));
    setBt({ data: null, error: null, loading: false });
  };
  const toggle = (kind: SourceKind) =>
    resplit(picks.some((p) => p.kind === kind) ? picks.filter((p) => p.kind !== kind) : [...picks, { kind, venue: defaultVenue(kind, asset), bps: 0, leverage: "2" }]);
  const update = (i: number, patch: Partial<Pick>) => setPicks(picks.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  const chooseAsset = (a: string) => {
    setAsset(a);
    const k = kindsFor(a)[0];
    const w = evenSplit(1);
    setPicks(k ? [{ kind: k, venue: defaultVenue(k, a), bps: w[0], leverage: "2" }] : []);
    setBt({ data: null, error: null, loading: false });
  };

  async function backtest() {
    setBt({ data: null, error: null, loading: true });
    try {
      setBt({ data: await runBacktest(toBacktest(asset, sources, rules, asset === "USDC" ? "1000" : "1")), error: null, loading: false });
    } catch (e) {
      setBt({ data: null, error: errText(e), loading: false });
    }
  }
  async function publish(pub: boolean) {
    if (!authenticated) return login();
    if (name.trim() === "") return setError("Name your strategy");
    setBusy(pub ? "public" : "private");
    setError(null);
    try {
      const s = await createStrategy({ name: name.trim(), asset, sources, rules, public: pub });
      router.push(`/earn/${s.id}`);
    } catch (e) {
      setError(errText(e));
      setBusy(null);
    }
  }

  const est = blendedApy(sources, asset, rates);

  return (
    <main className="w-full px-4 pb-24 pt-8 md:px-6">
      <div className="mx-auto w-full space-y-6">
        <div>
          <h1 className="text-xl font-medium sm:text-2xl">Build a strategy</h1>
          <p className="mt-1 text-sm text-muted-foreground">One asset, a mix of yield sources and rules. Backtest on mainnet history, then publish for others to follow.</p>
        </div>

        <div className="grid gap-6 lg:grid-cols-[1fr_380px] lg:items-start">
          <Panel>
            <div className="space-y-4 p-5 sm:p-6">
              <Step n={1}>Name &amp; asset</Step>
              <input
                value={name}
                onChange={(e) => setName(e.target.value.slice(0, 60))}
                placeholder="Delta-neutral ETH"
                aria-label="Strategy name"
                className="w-full bg-transparent text-2xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/40"
              />
              <div className="flex flex-wrap gap-2">
                {ASSETS.map((a) => (
                  <button
                    key={a}
                    onClick={() => chooseAsset(a)}
                    aria-pressed={asset === a}
                    className={cn(
                      "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm transition-colors",
                      asset === a ? "border-primary/60 bg-primary/10" : "border-border bg-secondary/30 hover:bg-secondary/60",
                    )}
                  >
                    <TokenIcon symbol={a} size={18} />
                    {a}
                  </button>
                ))}
              </div>
            </div>
            <Divider />

            <div className="space-y-3 p-5 sm:p-6">
              <div className="flex items-center justify-between">
                <Step n={2}>Sources &amp; weights</Step>
                <button onClick={() => resplit(picks)} className="rounded-lg px-2.5 py-1 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground">
                  Split evenly
                </button>
              </div>
              {(["lending", "staking", "lp", "basis"] as SourceKind[]).map((kind) => {
                const i = picks.findIndex((p) => p.kind === kind);
                const p = picks[i];
                const ok = kinds.includes(kind);
                const apy = p ? sourceApy({ kind, venue: p.venue, weightBps: p.bps, params: { leverage: p.leverage } }, asset, rates) : null;
                return (
                  <div key={kind} className={cn("rounded-xl border p-3", p ? "border-primary/40 bg-primary/5" : "border-border bg-secondary/30", !ok && "opacity-50")}>
                    <label className="flex items-center gap-2.5">
                      <input type="checkbox" checked={!!p} disabled={!ok} onChange={() => toggle(kind)} className="accent-[var(--primary)]" />
                      <span className="text-sm font-medium">{KIND_LABEL[kind]}</span>
                      <span className="truncate text-xs text-muted-foreground">{ok ? HINT[kind] : `not available for ${asset}`}</span>
                      {p && <span className="tnum ml-auto text-xs text-positive">{apy === null ? "—" : pct(apy)}</span>}
                    </label>
                    {p && (
                      <div className="mt-3 space-y-2">
                        {kind === "lending" && (
                          <select value={p.venue} onChange={(e) => update(i, { venue: e.target.value })} className={inputCls}>
                            {lendingVenues(asset).map((v) => (
                              <option key={v.venue} value={v.venue}>
                                {v.name}
                              </option>
                            ))}
                          </select>
                        )}
                        {kind === "lp" && <Notice>Estimated yield ({pct(0.13, 0)} assumed APR, no venue history) and a 4-day withdraw lock.</Notice>}
                        {kind === "basis" && (
                          <Field label={`Leverage (1–${PERP[asset].maxLev}×) on Perpl ${PERP[asset].name}`}>
                            <input className={inputCls} inputMode="decimal" value={p.leverage} onChange={(e) => update(i, { leverage: e.target.value })} />
                          </Field>
                        )}
                        <div className="flex items-center gap-3">
                          <input
                            type="range"
                            min={0}
                            max={10000}
                            step={50}
                            value={p.bps}
                            onChange={(e) => {
                              const w = setWeight(picks.map((x) => x.bps), i, Number(e.target.value));
                              setPicks(picks.map((x, j) => ({ ...x, bps: w[j] })));
                            }}
                            aria-label={`${KIND_LABEL[kind]} weight`}
                            className="range w-full"
                            style={{ "--pct": `${p.bps / 100}%` } as React.CSSProperties}
                          />
                          <span className="tnum w-14 text-right text-sm">{p.bps / 100}%</span>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
            <Divider />

            <div className="grid gap-3 p-5 sm:grid-cols-2 sm:p-6">
              <div className="sm:col-span-2">
                <Step n={3}>Rules</Step>
              </div>
              <Field label="Drift band (%)" hint="rebalance past this">
                <input className={inputCls} inputMode="decimal" value={drift} onChange={(e) => setDrift(e.target.value)} />
              </Field>
              <Field label="Min time between rebalances (h)">
                <input className={inputCls} inputMode="decimal" value={minHours} onChange={(e) => setMinHours(e.target.value)} />
              </Field>
              {hasBasis && (
                <>
                  <label className="flex items-center gap-2 text-sm sm:col-span-2">
                    <input type="checkbox" checked={gateOn} onChange={(e) => setGateOn(e.target.checked)} className="accent-[var(--primary)]" />
                    Funding gate — park the basis weight in the best lending venue while funding is low
                  </label>
                  {gateOn && (
                    <>
                      <Field label="Min short APR (%)">
                        <input className={inputCls} inputMode="decimal" value={gateApr} onChange={(e) => setGateApr(e.target.value)} />
                      </Field>
                      <Field label="Lookback (days)">
                        <input className={inputCls} inputMode="numeric" value={gateDays} onChange={(e) => setGateDays(e.target.value)} />
                      </Field>
                    </>
                  )}
                </>
              )}
            </div>
            <Divider />

            <div className="space-y-4 p-5 sm:p-6">
              <div className="flex items-center justify-between gap-3">
                <Step n={4}>Backtest</Step>
                <Button variant="ghost" pending={bt.loading} disabled={!!invalid} onClick={() => void backtest()}>
                  Run backtest
                </Button>
              </div>
              {(bt.data || bt.loading || bt.error) && <BacktestView bt={bt.data} loading={bt.loading} error={bt.error} />}
            </div>
          </Panel>

          <div className="space-y-3 lg:sticky lg:top-24">
            <Panel className="space-y-4 p-5 sm:p-6">
              <Label>Preview</Label>
              <div className="flex items-center gap-3">
                <TokenIcon symbol={asset} size={36} />
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold">{name.trim() || "Untitled strategy"}</p>
                  <p className="text-xs text-muted-foreground">{asset} strategy</p>
                </div>
                <div className="ml-auto text-right">
                  <div className="tnum text-lg font-medium text-positive">{pct(est)}</div>
                  <div className="text-[11px] text-muted-foreground">live APY est.</div>
                </div>
              </div>
              {picks.length > 0 && <AllocationDonut sources={sources} />}
              {invalid && <p className="text-xs text-warning">{invalid}</p>}
              {error && <ErrorBox message={error} />}
              <Button className="w-full py-3" pending={busy === "public"} disabled={!ready || !!invalid || busy !== null} onClick={() => void publish(true)}>
                {authenticated ? "Publish strategy" : "Log in to publish"}
              </Button>
              {authenticated && (
                <Button variant="ghost" className="w-full" pending={busy === "private"} disabled={!!invalid || busy !== null} onClick={() => void publish(false)}>
                  Save private
                </Button>
              )}
              <p className="text-xs leading-relaxed text-muted-foreground">
                After publishing you can deploy it yourself; followers run the same allocation in their own wallets. Open fee 0.4% (you earn 0.2% on
                followers).
              </p>
            </Panel>
          </div>
        </div>
      </div>
    </main>
  );
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
  return (
    <h2 className="flex items-center gap-2 text-base font-medium">
      <span className="grid size-6 place-items-center rounded-full bg-primary/15 text-xs text-primary">{n}</span>
      {children}
    </h2>
  );
}
