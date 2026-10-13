"use client";

import { useCallback, useState } from "react";
import { useSendTransaction } from "@privy-io/react-auth";
import { Check, ExternalLink, X } from "lucide-react";
import { ApiError, errText, prepareTrade, recordTrade, shortHash } from "@/lib/api";
import { chain, publicClient, txUrl } from "@/lib/config";
import { useSession } from "@/lib/session";
import type { Execution, Prepared, PrepareInput, TxRequest } from "@/lib/types";
import { cn } from "@/lib/utils";
import { showToast } from "./TxToast";

type TxPhase = "queued" | "signing" | "mining" | "done" | "reverted" | "cancelled";
export type Run = {
  status: "preparing" | "sending" | "recording" | "done" | "failed";
  txs: TxRequest[];
  phases: TxPhase[];
  hashes: string[];
  execution: Execution | null;
  error: string | null;
};

// an approve carries the index of the step it enables, so the next tx shares its step
const isApprove = (txs: TxRequest[], i: number) => txs[i + 1]?.step === txs[i].step;
const rejected = (e: unknown) => /reject|denied|cancel|exited|closed/i.test(String((e as Error)?.message ?? e));
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// api re-checks every hash on chain; tx_pending = its RPC hasn't seen the block yet
async function record(prepareId: string, hashes: string[]): Promise<Execution> {
  for (let i = 0; ; i++) {
    try {
      return await recordTrade(prepareId, hashes);
    } catch (e) {
      if (!(e instanceof ApiError && (e.message === "tx_pending" || e.status === 0 || e.status === 502)) || i >= 20) throw e;
      await sleep(1500);
    }
  }
}

// Client-signed trade: prepare → user signs each tx in order from the Privy embedded wallet → wait receipts → record.
export function useClientTrade(label: string) {
  const { sendTransaction } = useSendTransaction();
  const { address } = useSession();
  const [run, setRun] = useState<Run | null>(null);

  const start = useCallback(
    async (input: PrepareInput): Promise<Execution | null> => {
      if (!address) return null;
      const set = (patch: Partial<Run>) => setRun((r) => ({ ...(r as Run), ...patch }));
      const phases: TxPhase[] = [];
      const hashes: string[] = [];
      const phase = (i: number, p: TxPhase) => ((phases[i] = p), set({ phases: [...phases] }));
      const toastId = showToast({ tone: "loading", title: `${label}: getting a fresh quote…` });
      setRun({ status: "preparing", txs: [], phases: [], hashes: [], execution: null, error: null });

      const cur: { p?: Prepared } = {};
      let error: unknown = null;
      const prep = async () => {
        const p = (cur.p = await prepareTrade({ ...input, wallet: address }));
        phases.splice(0, phases.length, ...p.txs.map((): TxPhase => "queued"));
        hashes.length = 0;
        set({ status: "sending", txs: p.txs, phases: [...phases], hashes: [] });
      };
      try {
        await prep();
        let requotes = 0;
        for (let i = 0; i < cur.p!.txs.length; i++) {
          const tx: TxRequest = cur.p!.txs[i];
          if (Date.now() > Date.parse(cur.p!.expiresAt) - 5000) {
            // only approvals sent so far: re-quote is safe (allowance persists, new plan skips them)
            if (hashes.every((_, j) => isApprove(cur.p!.txs, j)) && requotes++ < 2) {
              showToast({ tone: "loading", title: `${label}: quote expired, re-quoting…` }, toastId);
              await prep();
              i = -1;
              continue;
            }
            throw new ApiError(410, "quote_expired");
          }
          phase(i, "signing");
          showToast({ tone: "loading", title: `Confirm ${i + 1} of ${cur.p!.txs.length} in your wallet`, description: tx.description }, toastId);
          let hash: `0x${string}`;
          try {
            ({ hash } = await sendTransaction(
              { to: tx.to, data: tx.data, value: tx.value, chainId: tx.chainId || chain.id },
              { address, uiOptions: { description: tx.description } },
            ));
          } catch (e) {
            phase(i, "cancelled");
            throw rejected(e) ? new Error("Cancelled in wallet") : e;
          }
          hashes.push(hash);
          set({ hashes: [...hashes] });
          phase(i, "mining");
          showToast({ tone: "loading", title: `Waiting for tx ${i + 1} of ${cur.p!.txs.length}`, description: tx.description, hash }, toastId);
          const rc = await publicClient.waitForTransactionReceipt({ hash, timeout: 120_000 });
          if (rc.status !== "success") {
            phase(i, "reverted");
            throw new Error(`Reverted: ${tx.description}`);
          }
          phase(i, "done");
        }
      } catch (e) {
        error = e;
      }

      let execution: Execution | null = null;
      if (cur.p && hashes.length) {
        set({ status: "recording" });
        try {
          execution = await record(cur.p.prepareId, hashes);
        } catch (e) {
          error ??= e;
        }
      }
      const ok = !error && execution?.status === "confirmed";
      const last = hashes.at(-1);
      const msg = error ? errText(error) : ok ? null : (execution?.error ?? "Stopped before every step ran");
      set({ status: ok ? "done" : "failed", execution, error: msg });
      showToast(
        ok
          ? { tone: "success", title: `${label} confirmed`, hash: last }
          : {
              tone: hashes.length ? "warning" : "error",
              title: msg!,
              description: hashes.length ? `${hashes.length} of ${cur.p?.txs.length ?? 0} txs sent` : undefined,
              hash: last,
            },
        toastId,
      );
      return execution;
    },
    [address, label, sendTransaction],
  );

  const busy = !!run && run.status !== "done" && run.status !== "failed";
  return { run, start, busy, reset: () => setRun(null) };
}

const DOT: Record<TxPhase, React.ReactNode> = {
  queued: <span className="size-3.5 rounded-full border border-border" />,
  signing: <span className="size-3.5 animate-pulse rounded-full bg-primary/70" />,
  mining: <span className="size-3.5 animate-spin rounded-full border-2 border-border border-t-primary" />,
  done: <Check className="size-3.5 text-positive" />,
  reverted: <X className="size-3.5 text-destructive" />,
  cancelled: <X className="size-3.5 text-muted-foreground" />,
};
const NOTE: Partial<Record<TxPhase, string>> = { signing: "confirm in wallet", mining: "confirming", reverted: "reverted", cancelled: "cancelled" };

// Step list for a client-signed run: one row per tx with its state + Monadscan link.
export function ClientRunProgress({ run }: { run: Run }) {
  const n = run.txs.length;
  const done = run.phases.filter((p) => p === "done").length;
  return (
    <div className="space-y-3 rounded-2xl border border-border bg-card p-4 text-sm" aria-live="polite">
      <div className="flex items-center gap-2">
        <span className="font-medium">
          {run.status === "preparing"
            ? "Preparing…"
            : run.status === "recording"
              ? "Recording…"
              : run.status === "done"
                ? "Done"
                : run.status === "failed"
                  ? "Stopped"
                  : "Sign in your wallet"}
        </span>
        <span className="tnum ml-auto text-xs text-muted-foreground">
          {done}/{n} txs
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
        <div
          className={cn("h-full rounded-full transition-[width] duration-500", run.status === "failed" ? "bg-destructive" : "bg-primary")}
          style={{ width: `${n ? (done / n) * 100 : 0}%` }}
        />
      </div>
      {n > 0 && (
        <ol className="space-y-1.5 text-xs">
          {run.txs.map((tx, i) => (
            <li key={i} className={cn("flex items-center gap-2", run.phases[i] === "queued" ? "text-muted-foreground" : "text-foreground")}>
              {DOT[run.phases[i] ?? "queued"]}
              <span className="min-w-0 truncate">{tx.description}</span>
              {NOTE[run.phases[i]] && <span className="shrink-0 text-muted-foreground">· {NOTE[run.phases[i]]}</span>}
              {run.hashes[i] && (
                <a
                  href={txUrl(run.hashes[i])}
                  target="_blank"
                  rel="noreferrer"
                  className="tnum ml-auto inline-flex shrink-0 items-center gap-1 rounded hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
                >
                  {shortHash(run.hashes[i])}
                  <ExternalLink className="size-3" />
                </a>
              )}
            </li>
          ))}
        </ol>
      )}
      {run.error && <p className="text-xs break-words text-destructive">{run.error}</p>}
    </div>
  );
}
