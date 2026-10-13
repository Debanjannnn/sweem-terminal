"use client";

import { useState } from "react";
import { createSchedule, errText, fmtUsd } from "@/lib/api";
import { useSession } from "@/lib/session";
import type { ScheduleInput, ScheduleKind, Target } from "@/lib/types";
import { Button, ErrorBox, Label } from "@/components/ui";
import { Segmented } from "@/components/motion";
import { showToast } from "@/components/TxToast";
import { DelegationCard } from "./Delegation";
import { cn } from "@/lib/utils";

const KINDS: Record<Target["type"], ScheduleKind[]> = {
  market: ["dca", "take_profit", "stop_loss"],
  basket: ["dca", "take_profit", "stop_loss", "rebalance"],
  position: ["take_profit", "stop_loss", "rebalance"],
};
export const KIND_LABEL: Record<string, string> = {
  dca: "DCA",
  take_profit: "Take profit",
  stop_loss: "Stop loss",
  rebalance: "Rebalance",
  recurring: "Recurring (legacy)",
};

const INTERVALS = [
  { value: "3600", label: "1h" },
  { value: "86400", label: "1d" },
  { value: "604800", label: "1w" },
  { value: "custom", label: "Custom" },
] as const;
type IntervalOpt = (typeof INTERVALS)[number]["value"];

const DEC = /^\d+(\.\d+)?$/;
const pos = (s: string) => DEC.test(s.trim()) && Number(s) > 0;

export const inputCls =
  "tnum w-full rounded-xl border border-border bg-card px-3 py-2.5 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 focus:border-ring";

export function Field({ label, hint, children }: { label: string; hint?: React.ReactNode; children: React.ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label>{label}</Label>
        {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </label>
  );
}

// current = live price (market, USDC/unit) or value (basket/position, USDC) for TP/SL hints
// quote: what a market target is priced/paid in (AUSD books); baskets/positions are USDC
export function ScheduleForm({ target, current, onCreated, quote = "USDC" }: { target: Target; current: number | null; onCreated: () => void; quote?: string }) {
  const { authenticated, login, delegated } = useSession();
  const kinds = KINDS[target.type];
  const [kind, setKind] = useState<ScheduleKind>(kinds[0]);
  const [amount, setAmount] = useState("25");
  const [every, setEvery] = useState<IntervalOpt>("86400");
  const [customMin, setCustomMin] = useState("30");
  const [maxPerRun, setMaxPerRun] = useState("");
  const [slippage, setSlippage] = useState("1");
  const [trigger, setTrigger] = useState("");
  const [sellPct, setSellPct] = useState(100);
  const [drift, setDrift] = useState("5");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const tp = kind === "take_profit";
  const triggerLabel = target.type === "market" ? `Trigger price (${quote})` : `Trigger value (${quote})`;
  const dist = current && pos(trigger) ? ((Number(trigger) - current) / current) * 100 : null;
  const intervalSeconds = every === "custom" ? Math.round(Number(customMin) * 60) : Number(every);
  const cap = maxPerRun.trim() || (kind === "dca" ? amount.trim() : "1000");
  const slippageBps = Math.round(Number(slippage) * 100);
  const driftBps = Math.round(Number(drift) * 100);

  function build(): ScheduleInput | string {
    if (!DEC.test(slippage.trim()) || slippageBps < 1 || slippageBps > 1000) return "Max slippage must be 0.01%–10%.";
    if (!pos(cap)) return `Max per run must be a positive ${quote} amount.`;
    const base = { kind, target, maxSlippageBps: slippageBps, maxPerRunUsdc: cap };
    if (kind === "dca") {
      if (!pos(amount)) return `Amount must be a positive ${quote} amount.`;
      if (!(intervalSeconds >= 60)) return "Interval must be at least 1 minute.";
      if (Number(cap) < Number(amount)) return "Max per run must be at least the amount.";
      return { ...base, amountUsdc: amount.trim(), intervalSeconds };
    }
    if (kind === "rebalance") {
      if (!(intervalSeconds >= 60)) return "Check cadence must be at least 1 minute.";
      if (target.type === "basket" && !(driftBps >= 1 && driftBps <= 10000)) return "Drift band must be 0.01%–100%.";
      return { ...base, intervalSeconds, ...(drift.trim() && driftBps > 0 ? { driftBps } : {}) };
    }
    if (!pos(trigger)) return "Trigger must be a positive number.";
    return { ...base, triggerPrice: trigger.trim(), sellBps: sellPct * 100 };
  }

  async function submit() {
    const input = build();
    setError(typeof input === "string" ? input : null);
    if (typeof input === "string") return;
    setPending(true);
    try {
      await createSchedule(input);
      showToast({ tone: "success", title: `${KIND_LABEL[kind]} scheduled` });
      onCreated();
    } catch (e) {
      setError(errText(e));
    } finally {
      setPending(false);
    }
  }

  if (!authenticated)
    return (
      <div className="flex items-center justify-between gap-3 text-sm">
        <span className="text-muted-foreground">Sign in to automate.</span>
        <Button onClick={login}>Log in</Button>
      </div>
    );

  const intervalPicker = (label: string) => (
    <>
      <Field label={label}>
        <div className="flex flex-wrap gap-1.5">
          {INTERVALS.map((o) => (
            <button
              key={o.value}
              type="button"
              onClick={() => setEvery(o.value)}
              className={cn(
                "rounded-full px-3 py-1.5 text-xs font-medium transition-colors",
                every === o.value ? "bg-primary text-primary-foreground" : "bg-secondary/60 text-muted-foreground hover:text-foreground",
              )}
            >
              {o.label}
            </button>
          ))}
        </div>
      </Field>
      {every === "custom" && (
        <Field label="Custom interval (minutes, min 1)">
          <input className={inputCls} inputMode="decimal" value={customMin} onChange={(e) => setCustomMin(e.target.value)} />
        </Field>
      )}
    </>
  );

  return (
    <div className="space-y-4">
      <Segmented
        id={`schedule-kind-${target.type}`}
        value={kind}
        onChange={(k) => (setKind(k), setError(null))}
        options={kinds.map((k) => ({ value: k, label: KIND_LABEL[k] }))}
      />

      {!delegated && <DelegationCard compact />}

      {kind === "dca" && (
        <>
          <Field label={`Buy per run (${quote})`}>
            <input className={inputCls} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
          </Field>
          {intervalPicker("Every")}
        </>
      )}

      {(kind === "take_profit" || kind === "stop_loss") && (
        <>
          <Field label={triggerLabel} hint={current !== null ? `now ${fmtUsd(current)}` : "no live value"}>
            <input
              className={inputCls}
              inputMode="decimal"
              placeholder={current ? (current * (tp ? 1.1 : 0.9)).toFixed(4) : "0.00"}
              value={trigger}
              onChange={(e) => setTrigger(e.target.value)}
            />
          </Field>
          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            {current !== null &&
              [5, 10, 20].map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => setTrigger((current * (1 + ((tp ? 1 : -1) * p) / 100)).toFixed(6))}
                  className="rounded-full bg-secondary/60 px-3 py-1.5 font-medium text-muted-foreground transition-colors hover:text-foreground"
                >
                  {tp ? "+" : "−"}
                  {p}%
                </button>
              ))}
            {dist !== null && (
              <span className={cn("ml-auto", (tp ? dist <= 0 : dist >= 0) ? "text-warning" : "text-muted-foreground")}>
                {(tp ? dist <= 0 : dist >= 0) ? "already past trigger, fires immediately" : `${Math.abs(dist).toFixed(2)}% ${dist > 0 ? "above" : "below"} now`}
              </span>
            )}
          </div>
          <Field label="Sell" hint={<span className="tnum text-foreground">{sellPct}%</span>}>
            <input
              type="range"
              min={1}
              max={100}
              value={sellPct}
              onChange={(e) => setSellPct(Number(e.target.value))}
              className="range w-full"
              style={{ "--pct": `${sellPct}%` } as React.CSSProperties}
            />
          </Field>
        </>
      )}

      {kind === "rebalance" && (
        <>
          <Field label="Drift band (%)" hint={target.type === "position" ? "blank = strategy rules" : "rebalance when a leg drifts this far"}>
            <input className={inputCls} inputMode="decimal" value={drift} onChange={(e) => setDrift(e.target.value)} />
          </Field>
          {intervalPicker("Check every")}
        </>
      )}

      <div className="grid grid-cols-2 gap-3">
        <Field label={`Max per run (${quote})`}>
          <input className={inputCls} inputMode="decimal" placeholder={cap} value={maxPerRun} onChange={(e) => setMaxPerRun(e.target.value)} />
        </Field>
        <Field label="Max slippage (%)">
          <input className={inputCls} inputMode="decimal" value={slippage} onChange={(e) => setSlippage(e.target.value)} />
        </Field>
      </div>

      {error && <ErrorBox message={error} />}

      <Button className="w-full" pending={pending} disabled={!delegated} onClick={() => void submit()}>
        {delegated ? `Create ${KIND_LABEL[kind].toLowerCase()}` : "Enable delegation first"}
      </Button>
    </div>
  );
}
