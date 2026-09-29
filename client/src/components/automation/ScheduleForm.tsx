"use client";

import { useState } from "react";
import { createSchedule, errMsg, fmtUsd, getLiveNav } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import type { ScheduleInput, ScheduleKind } from "@/lib/types";
import { Button, ErrorBox, Label } from "@/components/ui";
import { Segmented } from "@/components/motion";
import { showToast } from "@/components/TxToast";
import { DelegationCard } from "./Delegation";
import { cn } from "@/lib/utils";

const API_ERRORS: Record<string, string> = {
  invalid_basket: "Invalid basket address.",
  invalid_max_slippage_bps: "Max slippage must be between 0.01% and 10%.",
  invalid_max_per_run_usdc: "Max per run must be a positive USDC amount.",
  invalid_amount_usdc: "Amount must be a positive USDC amount.",
  invalid_interval_seconds: "Interval must be at least 1 minute.",
  max_per_run_below_amount: "Max per run must be at least the amount.",
  invalid_stop_nav_per_share: "Trigger NAV must be a positive number.",
  invalid_redeem_bps: "Sell percentage must be between 1% and 100%.",
  invalid_kind: "Unknown schedule type.",
  delegation_inactive: "Enable automation first.",
  basket_not_found: "This basket isn't indexed yet.",
  unauthorized: "Session expired, sign in again.",
  bad_request: "The request was malformed.",
};

export const apiErrorText = (e: unknown) => {
  const m = errMsg(e);
  return API_ERRORS[m] ?? m;
};

const INTERVALS = [
  { value: "60", label: "1m test" },
  { value: "3600", label: "1h" },
  { value: "86400", label: "1d" },
  { value: "604800", label: "1w" },
  { value: "custom", label: "Custom" },
] as const;
type IntervalOpt = (typeof INTERVALS)[number]["value"];

const DEC = /^\d+(\.\d+)?$/;
const pos = (s: string) => DEC.test(s.trim()) && Number(s) > 0;
// ponytail: api requires maxPerRunUsdc on stop-loss but only enforces it on mints, so send a no-op cap.
const STOP_LOSS_CAP = "1000000000";

export const inputCls =
  "tnum w-full rounded-xl border border-border bg-card px-3 py-2.5 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 focus:border-ring";

function Field({ label, hint, children }: { label: string; hint?: React.ReactNode; children: React.ReactNode }) {
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

export function ScheduleForm({ basket, onCreated }: { basket: string; onCreated: () => void }) {
  const { authenticated, login, delegated } = useSession();
  const [kind, setKind] = useState<ScheduleKind>("recurring");
  const [amount, setAmount] = useState("10");
  const [every, setEvery] = useState<IntervalOpt>("86400");
  const [customMin, setCustomMin] = useState("30");
  const [maxPerRun, setMaxPerRun] = useState("");
  const [slippage, setSlippage] = useState("1");
  const [stopNav, setStopNav] = useState("");
  const [sellPct, setSellPct] = useState(100);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const nav = useAsync(kind === "stop_loss" ? `nav:${basket}` : null, () => getLiveNav(basket));
  const current = nav.data ? Number(nav.data.navPerShare) : null;
  const below = current && pos(stopNav) ? ((current - Number(stopNav)) / current) * 100 : null;

  const intervalSeconds = every === "custom" ? Math.round(Number(customMin) * 60) : Number(every);
  const cap = maxPerRun.trim() || amount.trim();
  const slippageBps = Math.round(Number(slippage) * 100);

  function validate(): string | null {
    if (!DEC.test(slippage.trim()) || slippageBps < 1 || slippageBps > 1000) return API_ERRORS.invalid_max_slippage_bps;
    if (kind === "recurring") {
      if (!pos(amount)) return API_ERRORS.invalid_amount_usdc;
      if (!Number.isFinite(intervalSeconds) || intervalSeconds < 60) return API_ERRORS.invalid_interval_seconds;
      if (!pos(cap)) return API_ERRORS.invalid_max_per_run_usdc;
      if (Number(cap) < Number(amount)) return API_ERRORS.max_per_run_below_amount;
    } else {
      if (!pos(stopNav)) return API_ERRORS.invalid_stop_nav_per_share;
      if (sellPct < 1 || sellPct > 100) return API_ERRORS.invalid_redeem_bps;
    }
    return null;
  }

  async function submit() {
    const v = validate();
    setError(v);
    if (v) return;
    const input: ScheduleInput =
      kind === "recurring"
        ? { basket, kind, amountUsdc: amount.trim(), intervalSeconds, maxSlippageBps: slippageBps, maxPerRunUsdc: cap }
        : {
            basket,
            kind,
            stopNavPerShare: stopNav.trim(),
            redeemBps: sellPct * 100,
            maxSlippageBps: slippageBps,
            maxPerRunUsdc: STOP_LOSS_CAP,
          };
    setPending(true);
    try {
      await createSchedule(input);
      showToast({ tone: "success", title: kind === "recurring" ? "Recurring buy scheduled" : "Stop-loss set" });
      onCreated();
    } catch (e) {
      setError(apiErrorText(e));
    } finally {
      setPending(false);
    }
  }

  if (!authenticated)
    return (
      <div className="flex items-center justify-between gap-3 text-sm">
        <span className="text-muted-foreground">Sign in to automate this basket.</span>
        <Button onClick={login}>Connect</Button>
      </div>
    );

  return (
    <div className="space-y-4">
      <Segmented
        id={`schedule-kind-${basket}`}
        value={kind}
        onChange={(k) => (setKind(k), setError(null))}
        options={[
          { value: "recurring", label: "Recurring buy" },
          { value: "stop_loss", label: "Stop-loss" },
        ]}
      />

      {!delegated && <DelegationCard compact />}

      {kind === "recurring" ? (
        <>
          <Field label="Amount per buy (USDC)">
            <input className={inputCls} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
          </Field>
          <Field label="Every">
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
          <Field label="Max per run (USDC)" hint="safety cap, ≥ amount">
            <input
              className={inputCls}
              inputMode="decimal"
              placeholder={amount || "0"}
              value={maxPerRun}
              onChange={(e) => setMaxPerRun(e.target.value)}
            />
          </Field>
        </>
      ) : (
        <>
          <Field
            label="Trigger NAV / share (USDC)"
            hint={nav.loading ? "loading NAV…" : current !== null ? `current ${fmtUsd(current)}` : "NAV unavailable"}
          >
            <input
              className={inputCls}
              inputMode="decimal"
              placeholder={current ? (current * 0.9).toFixed(4) : "0.00"}
              value={stopNav}
              onChange={(e) => setStopNav(e.target.value)}
            />
          </Field>
          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            {current !== null &&
              [5, 10, 20].map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => setStopNav((current * (1 - p / 100)).toFixed(6))}
                  className="rounded-full bg-secondary/60 px-3 py-1.5 font-medium text-muted-foreground transition-colors hover:text-foreground"
                >
                  −{p}%
                </button>
              ))}
            {below !== null && (
              <span className={cn("ml-auto", below < 0 ? "text-warning" : "text-muted-foreground")}>
                {below < 0 ? `${(-below).toFixed(2)}% above current, fires immediately` : `${below.toFixed(2)}% below current`}
              </span>
            )}
          </div>
          <Field label="Sell" hint={<span className="tnum text-foreground">{sellPct}% of shares</span>}>
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

      <Field label="Max slippage (%)" hint="0.01 – 10">
        <input className={inputCls} inputMode="decimal" value={slippage} onChange={(e) => setSlippage(e.target.value)} />
      </Field>

      {error && <ErrorBox message={error} />}

      <Button className="w-full" pending={pending} disabled={!delegated} onClick={() => void submit()}>
        {kind === "recurring" ? "Schedule recurring buy" : "Set stop-loss"}
      </Button>
    </div>
  );
}
