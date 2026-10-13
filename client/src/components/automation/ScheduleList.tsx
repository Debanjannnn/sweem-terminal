"use client";

import Link from "next/link";
import { useState } from "react";
import { ExternalLink, Trash2 } from "lucide-react";
import { deleteSchedule, errText, fmtBps, fmtTime, fmtUsd, getExecutions, getSchedules, shortHash } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { txUrl } from "@/lib/config";
import type { Schedule, Target } from "@/lib/types";
import { ErrorBox, Panel, Skeleton, StatusPill } from "@/components/ui";
import { toastError } from "@/components/TxToast";
import { stepLabel } from "@/components/Execution";
import { KIND_LABEL } from "./ScheduleForm";

const every = (s: number) =>
  s % 604800 === 0 ? `${s / 604800}w` : s % 86400 === 0 ? `${s / 86400}d` : s % 3600 === 0 ? `${s / 3600}h` : s % 60 === 0 ? `${s / 60}m` : `${s}s`;

const describe = (s: Schedule) =>
  s.kind === "dca"
    ? `Buy ${fmtUsd(Number(s.amountUsdc))} every ${every(s.intervalSeconds ?? 0)}`
    : s.kind === "rebalance"
      ? `Check every ${every(s.intervalSeconds ?? 0)}${s.driftBps ? `, drift > ${fmtBps(s.driftBps)}` : ""}`
      : s.kind === "take_profit" || s.kind === "stop_loss"
        ? `Sell ${(s.sellBps ?? 0) / 100}% when ${s.kind === "take_profit" ? "≥" : "≤"} ${fmtUsd(Number(s.triggerPrice))}`
        : "Legacy schedule";

const targetHref = (t: Target | null) =>
  !t ? null : t.type === "market" ? `/trade/${t.id}` : t.type === "strategy" ? `/baskets/${t.id}` : "/portfolio";

// Schedules (optionally for one target); refreshes every 15s.
export function ScheduleList({ target, names }: { target?: Target; names?: Record<string, string> }) {
  const { authenticated } = useSession();
  const schedules = useAsync(authenticated ? "schedules" : null, getSchedules, 15_000);
  const [deleting, setDeleting] = useState<string | null>(null);
  const { reload } = schedules;
  if (!authenticated) return null;

  const mine = (schedules.data ?? [])
    .filter((s) => !target || (s.target?.type === target.type && s.target.id === target.id))
    .sort((x, y) => Number(y.active) - Number(x.active));

  async function remove(id: string) {
    setDeleting(id);
    try {
      await deleteSchedule(id);
      reload();
    } catch (e) {
      toastError(new Error(errText(e)));
    } finally {
      setDeleting(null);
    }
  }

  return (
    <Panel>
      <div className="px-5 pt-4 text-sm font-medium">Automations</div>
      {schedules.error && (
        <div className="p-5">
          <ErrorBox message={errText(new Error(schedules.error))} />
        </div>
      )}
      {schedules.loading && !schedules.data ? (
        <div className="space-y-3 p-5">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      ) : mine.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No automations yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {mine.map((s) => {
            const href = !target ? targetHref(s.target) : null;
            return (
              <li key={s.id} className={s.active ? "p-5" : "p-5 opacity-60"}>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span className="rounded-md bg-secondary/60 px-2 py-0.5 text-[11px] text-muted-foreground">{KIND_LABEL[s.kind] ?? s.kind}</span>
                  <span className="tnum text-sm">{describe(s)}</span>
                  {href && s.target && (
                    <Link href={href} className="text-xs text-muted-foreground underline-offset-4 hover:underline">
                      {names?.[s.target.id] ?? (s.target.type === "market" ? s.target.id : `${s.target.type === "strategy" ? "basket" : s.target.type} ${s.target.id.slice(0, 8)}`)}
                    </Link>
                  )}
                  <span className="ml-auto">
                    <StatusPill status={s.active ? "confirmed" : "rejected"} />
                  </span>
                  {s.active && (
                    <button
                      type="button"
                      aria-label="Delete automation"
                      disabled={deleting === s.id}
                      onClick={() => void remove(s.id)}
                      className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-destructive disabled:opacity-50"
                    >
                      <Trash2 className="size-4" />
                    </button>
                  )}
                </div>
                <div className="tnum mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>max/run {fmtUsd(Number(s.maxPerRunUsdc))}</span>
                  <span>slippage {fmtBps(s.maxSlippageBps)}</span>
                  {s.active && s.nextRunAt && <span>next {fmtTime(s.nextRunAt)}</span>}
                  <span className="ml-auto">created {fmtTime(s.createdAt)}</span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}

// Every api + scheduler execution for the user, newest first.
export function ExecutionLog() {
  const { authenticated } = useSession();
  const ex = useAsync(authenticated ? "executions" : null, getExecutions, 15_000);
  const runs = [...(ex.data ?? [])].sort((x, y) => y.createdAt.localeCompare(x.createdAt)).slice(0, 30);
  return (
    <Panel>
      <div className="px-5 pt-4 text-sm font-medium">Executions</div>
      {ex.error && (
        <div className="p-5">
          <ErrorBox message={errText(new Error(ex.error))} />
        </div>
      )}
      {ex.loading && !ex.data ? (
        <div className="p-5">
          <Skeleton className="h-4 w-1/2" />
        </div>
      ) : runs.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No executions yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {runs.map((e) => {
            const hash = e.txHash ?? e.txHashes.at(-1);
            return (
              <li key={e.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 py-3 text-sm">
                <StatusPill status={e.status} />
                <span className="capitalize">{e.kind.replace(/_/g, " ")}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {e.steps.filter((s) => s.action !== "approve").map(stepLabel).slice(0, 3).join(", ")}
                </span>
                <span className="tnum text-xs text-muted-foreground">
                  {e.stepDone}/{e.stepsTotal}
                </span>
                {e.scheduleId && <span className="rounded-md bg-secondary/60 px-1.5 py-0.5 text-[10px] text-muted-foreground">auto</span>}
                {e.error && (
                  <span className="text-xs text-destructive" title={e.error}>
                    {errText(new Error(e.error))}
                  </span>
                )}
                <span className="ml-auto text-xs text-muted-foreground">{fmtTime(e.createdAt)}</span>
                {hash && (
                  <a href={txUrl(hash)} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
                    {shortHash(hash)}
                    <ExternalLink className="size-3" />
                  </a>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}
