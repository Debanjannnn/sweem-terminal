"use client";

import { useEffect } from "react";
import { Check, ExternalLink } from "lucide-react";
import { errText, shortHash } from "@/lib/api";
import { txUrl } from "@/lib/config";
import { settled, useExecution } from "@/lib/hooks";
import type { Execution, Step } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Spinner, StatusPill } from "./ui";

const LABEL: Record<string, string> = {
  transfer: "Open fee → treasury",
  approve: "Approve",
  marketBuy: "Market buy",
  marketSell: "Market sell",
  marginDeposit: "Deposit to trading balance",
  marginWithdraw: "Withdraw trading balance",
  limit: "Place limit order",
  cancel: "Cancel order",
};

export const stepLabel = (s: Step) =>
  [LABEL[s.action] ?? s.action.replace(/([A-Z])/g, " $1").toLowerCase(), s.market ?? s.venue ?? s.token ?? s.asset]
    .filter(Boolean)
    .join(" · ");

// Live step-by-step progress of an executor run (polls until settled).
export function ExecutionProgress({ id, onSettled }: { id: string; onSettled?: (e: Execution) => void }) {
  const ex = useExecution(id);
  const e = ex?.data;
  const done = settled(e);
  useEffect(() => {
    if (e && done) onSettled?.(e);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [done]);
  if (!e) return ex?.error ? <p className="text-xs text-destructive">{errText(new Error(ex.error))}</p> : <Spinner label="Submitting…" />;
  const pct = e.stepsTotal ? (e.stepDone / e.stepsTotal) * 100 : 0;
  return (
    <div className="space-y-3 rounded-2xl border border-border bg-card p-4 text-sm">
      <div className="flex items-center gap-2">
        <span className="font-medium capitalize">{e.kind.replace(/_/g, " ")}</span>
        <StatusPill status={e.status} />
        <span className="tnum ml-auto text-xs text-muted-foreground">
          {e.stepDone}/{e.stepsTotal} steps
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
        <div
          className={cn("h-full rounded-full transition-[width] duration-500", e.status === "failed" ? "bg-destructive" : "bg-primary")}
          style={{ width: `${pct}%` }}
        />
      </div>
      <ol className="space-y-1.5 text-xs">
        {e.steps.map((s, i) => (
          <li key={i} className={cn("flex items-center gap-2", i < e.stepDone ? "text-foreground" : "text-muted-foreground")}>
            {i < e.stepDone ? (
              <Check className="size-3.5 text-positive" />
            ) : i === e.stepDone && !done ? (
              <span className="size-3.5 animate-spin rounded-full border-2 border-border border-t-primary" />
            ) : (
              <span className="size-3.5 rounded-full border border-border" />
            )}
            {stepLabel(s)}
            {e.txHashes[i] && (
              <a href={txUrl(e.txHashes[i])} target="_blank" rel="noreferrer" className="ml-auto inline-flex items-center gap-1 hover:text-foreground">
                {shortHash(e.txHashes[i])}
                <ExternalLink className="size-3" />
              </a>
            )}
          </li>
        ))}
      </ol>
      {e.error && <p className="text-xs break-words text-destructive">{errText(new Error(e.error))}</p>}
    </div>
  );
}
