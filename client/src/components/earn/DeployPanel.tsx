"use client";

import { useState } from "react";
import { formatUnits } from "viem";
import { deployStrategy, errText, fmtUsd } from "@/lib/api";
import { FEE_BPS } from "@/lib/config";
import { useHoldings, useTickers } from "@/lib/hooks";
import { useSession } from "@/lib/session";
import { fmtUnits, parseAmount, USDC } from "@/lib/tokens";
import type { Strategy } from "@/lib/types";
import { Button, Notice } from "@/components/ui";
import { ExecutionProgress } from "@/components/Execution";
import { DelegationCard } from "@/components/automation/Delegation";
import { actionBtn, balanceOf, pill, Row, Slippage } from "@/components/trade/MarketPanel";
import { minDeposit, parts } from "./sources";

// Follow/deploy: USDC in, 0.4% open fee to treasury, executor routes the rest into every source from the user's wallet.
export function DeployPanel({ strategy }: { strategy: Strategy }) {
  const { ready, authenticated, login, delegated, me } = useSession();
  const holdings = useHoldings();
  const { tickers } = useTickers();
  const [amount, setAmount] = useState("");
  const [slippage, setSlippage] = useState(100);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);

  const raw = parseAmount(amount, USDC.decimals);
  const usdc = balanceOf(holdings.data, "USDC");
  const n = raw !== null ? Number(formatUnits(raw, USDC.decimals)) : 0;
  const fee = (n * FEE_BPS) / 10000;
  const own = me?.id === strategy.creator;
  const srcs = parts(strategy).flatMap((sl) => sl.sources);
  const lp = srcs.some((s) => s.kind === "lp");
  // ticker-mid estimate of the Kuru min-order floor; server rejects below_min_size authoritatively
  const min = minDeposit(parts(strategy), Object.fromEntries(Object.entries(tickers).map(([k, t]) => [k, t.price]))).total;

  const reason = !ready
    ? "Loading…"
    : !delegated
      ? "Enable delegation to deploy"
      : amount.trim() === ""
        ? "Enter an amount"
        : raw === null || raw === BigInt(0)
          ? "Invalid amount"
          : usdc !== null && raw > usdc
            ? "Insufficient USDC"
            : min && n < min
              ? `Min deposit ${fmtUsd(min)}`
              : null;

  async function submit() {
    setBusy(true);
    setErr(null);
    try {
      const r = await deployStrategy(strategy.id, amount.trim(), slippage);
      setExecId(r.executionId);
      setAmount("");
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4 p-5">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium">{own ? "Deploy" : "Follow"} with USDC</span>
        <Slippage value={slippage} onChange={setSlippage} />
      </div>
      <div className="rounded-2xl bg-card p-4">
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          Amount
          {usdc !== null && (
            <button type="button" onClick={() => setAmount(formatUnits(usdc, 6))} className="tnum text-xs hover:text-foreground">
              Max {fmtUnits(usdc, 6, 2)}
            </button>
          )}
        </div>
        <div className="mt-2 flex items-center gap-3">
          <input
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            aria-label="Amount in USDC"
            placeholder="0"
            className="tnum w-full min-w-0 bg-transparent text-3xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/50"
          />
          {pill("USDC")}
        </div>
      </div>
      <dl className="space-y-1.5 px-1 text-xs">
        <Row label={`Open fee (${FEE_BPS / 100}%)`}>{fmtUsd(fee)}</Row>
        <Row label="Deployed">{fmtUsd(Math.max(0, n - fee))}</Row>
        <Row label="Withdraw fee">none</Row>
        {min !== 0 && <Row label="Min deposit">{min === null ? "-" : fmtUsd(min)}</Row>}
      </dl>
      {lp && <Notice>Includes Kuru ActiveVault liquidity: 4-day lock, withdraw may fail inside the lock. Yield is estimated.</Notice>}
      {authenticated && !delegated && <DelegationCard compact />}
      {err && <Notice tone="bad">{err}</Notice>}
      {!authenticated && ready ? (
        <Button className={actionBtn} onClick={login}>
          Log in to {own ? "deploy" : "follow"}
        </Button>
      ) : (
        <Button className={actionBtn} pending={busy} disabled={reason !== null} onClick={() => void submit()}>
          {busy ? "Submitting…" : (reason ?? `${own ? "Deploy" : "Follow"} strategy`)}
        </Button>
      )}
      {execId && <ExecutionProgress id={execId} onSettled={holdings.reload} />}
      <p className="text-[11px] leading-4 text-muted-foreground">
        Monad mainnet, real funds. Everything stays in your own wallet&apos;s venue positions; sweem&apos;s signer runs the steps (fee → swaps → deposits
        {srcs.some((s) => s.kind === "basis") ? " → Perpl hedge" : ""}). {own ? "" : "The creator earns half the open fee."}
      </p>
    </div>
  );
}
