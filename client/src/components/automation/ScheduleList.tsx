"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { ExternalLink, Trash2 } from "lucide-react";
import { deleteSchedule, fmtBps, fmtTime, fmtUsd, getBaskets, getExecutions, getSchedules, shortHash } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { SHARE_DECIMALS, txUrl, USDC_DECIMALS } from "@/lib/config";
import { fmtUnits } from "@/lib/tokens";
import type { Execution, Schedule } from "@/lib/types";
import { ErrorBox, Panel, Skeleton, StatusPill } from "@/components/ui";
import { toastError } from "@/components/TxToast";
import { apiErrorText } from "./ScheduleForm";

const every = (s: number) =>
  s % 604800 === 0 ? `${s / 604800}w` : s % 86400 === 0 ? `${s / 86400}d` : s % 3600 === 0 ? `${s / 3600}h` : s % 60 === 0 ? `${s / 60}m` : `${s}s`;

const describe = (s: Schedule) =>
  s.kind === "recurring"
    ? `Buy ${fmtUsd(Number(s.amountUsdc))} every ${every(s.intervalSeconds ?? 0)}`
    : `Sell ${fmtBps(s.redeemBps ?? 0).replace(".00", "")} when NAV ≤ ${fmtUsd(Number(s.stopNavPerShare))}`;

const execAmount = (e: Execution) =>
  e.amountRaw === null
    ? "—"
    : e.kind === "mint"
      ? `${fmtUnits(e.amountRaw, USDC_DECIMALS, 2)} USDC`
      : `${fmtUnits(e.amountRaw, SHARE_DECIMALS, 4)} shares`;

// Refreshes every 15s so newly created schedules and execution statuses show up without a remount.
export function ScheduleList({ basket }: { basket?: string }) {
  const { authenticated } = useSession();
  const schedules = useAsync(authenticated ? "schedules" : null, getSchedules);
  const executions = useAsync(authenticated ? "executions" : null, getExecutions);
  const baskets = useAsync(authenticated && !basket ? "baskets" : null, () => getBaskets());
  const [deleting, setDeleting] = useState<string | null>(null);
  const { reload: r1 } = schedules;
  const { reload: r2 } = executions;

  useEffect(() => {
    const t = setInterval(() => (r1(), r2()), 15_000);
    return () => clearInterval(t);
  }, [r1, r2]);

  if (!authenticated) return null;

  const b = basket?.toLowerCase();
  const mine = (schedules.data ?? []).filter((s) => !b || s.basket === b).sort((x, y) => Number(y.active) - Number(x.active));
  const runs = (executions.data ?? [])
    .filter((e) => !b || e.basket === b)
    .sort((x, y) => y.createdAt.localeCompare(x.createdAt))
    .slice(0, 20);
  const nameOf = (addr: string) => baskets.data?.find((x) => x.address === addr)?.symbol ?? shortHash(addr);

  async function remove(id: string) {
    setDeleting(id);
    try {
      await deleteSchedule(id);
      r1();
    } catch (e) {
      toastError(new Error(apiErrorText(e)));
    } finally {
      setDeleting(null);
    }
  }

  const loading = (schedules.loading && !schedules.data) || (executions.loading && !executions.data);

  return (
    <div className="space-y-4">
      {schedules.error && <ErrorBox message={apiErrorText(new Error(schedules.error))} />}
      <Panel>
        <div className="px-5 pt-4 text-sm font-medium">Schedules</div>
        {loading ? (
          <div className="space-y-3 p-5">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
          </div>
        ) : mine.length === 0 ? (
          <p className="p-5 text-sm text-muted-foreground">No schedules yet.</p>
        ) : (
          <ul className="divide-y divide-border">
            {mine.map((s) => (
              <li key={s.id} className={s.active ? "p-5" : "p-5 opacity-60"}>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span className="rounded-md bg-secondary/60 px-2 py-0.5 text-[11px] text-muted-foreground">
                    {s.kind === "recurring" ? "recurring" : "stop-loss"}
                  </span>
                  <span className="tnum text-sm">{describe(s)}</span>
                  {!b && (
                    <Link href={`/baskets/${s.basket}`} className="text-xs text-muted-foreground underline-offset-4 hover:underline">
                      {nameOf(s.basket)}
                    </Link>
                  )}
                  <span className="ml-auto">
                    <StatusPill status={s.active ? "confirmed" : "rejected"} />
                  </span>
                  {s.active && (
                    <button
                      type="button"
                      aria-label="Delete schedule"
                      disabled={deleting === s.id}
                      onClick={() => void remove(s.id)}
                      className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-destructive disabled:opacity-50"
                    >
                      <Trash2 className="size-4" />
                    </button>
                  )}
                </div>
                <div className="tnum mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>{s.active ? "active" : "inactive"}</span>
                  {s.kind === "recurring" && <span>max/run {fmtUsd(Number(s.maxPerRunUsdc))}</span>}
                  <span>slippage {fmtBps(s.maxSlippageBps)}</span>
                  {s.active && s.nextRunAt && <span>next run {fmtTime(s.nextRunAt)}</span>}
                  <span className="ml-auto">created {fmtTime(s.createdAt)}</span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel>
        <div className="px-5 pt-4 text-sm font-medium">Recent executions</div>
        {executions.error && (
          <div className="p-5">
            <ErrorBox message={executions.error} />
          </div>
        )}
        {loading ? (
          <div className="p-5">
            <Skeleton className="h-4 w-1/2" />
          </div>
        ) : runs.length === 0 ? (
          <p className="p-5 text-sm text-muted-foreground">No executions yet.</p>
        ) : (
          <ul className="divide-y divide-border">
            {runs.map((e) => (
              <li key={e.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 py-3 text-sm">
                <StatusPill status={e.status} />
                <span className="capitalize">{e.kind === "mint" ? "buy" : "sell"}</span>
                <span className="tnum">{execAmount(e)}</span>
                {!b && <span className="text-xs text-muted-foreground">{nameOf(e.basket)}</span>}
                {e.error && (
                  <span className="text-xs text-destructive" title={e.error}>
                    {apiErrorText(new Error(e.error))}
                  </span>
                )}
                <span className="ml-auto text-xs text-muted-foreground">{fmtTime(e.createdAt)}</span>
                {e.txHash && (
                  <a
                    href={txUrl(e.txHash)}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    {shortHash(e.txHash)}
                    <ExternalLink className="size-3" />
                  </a>
                )}
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </div>
  );
}
