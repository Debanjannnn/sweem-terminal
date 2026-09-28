"use client";

import { useState } from "react";
import { ChevronDown, UserRound } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { useDismissable } from "@/components/ui/Dropdown";
import { ErrorState } from "@/components/ui/ErrorState";
import { Pill } from "@/components/ui/Pill";
import { ApiError, errorMessage } from "@/lib/api";
import { truncateAddress } from "@/lib/format";
import { useDelegation, useMarkets, usePutDelegation, useRevokeDelegation } from "@/lib/query";
import { useSession } from "@/lib/session";

const EXPIRY = [
  { label: "24 hours", hours: 24 },
  { label: "7 days", hours: 24 * 7 },
  { label: "30 days", hours: 24 * 30 },
];

export function WalletMenu() {
  const session = useSession();
  const { open, setOpen, ref } = useDismissable();
  const delegation = useDelegation();
  const put = usePutDelegation();
  const revokeRow = useRevokeDelegation();
  const markets = useMarkets();

  const [perSlice, setPerSlice] = useState("1000000");
  const [total, setTotal] = useState("10000000");
  const [hours, setHours] = useState(EXPIRY[1].hours);
  const [error, setError] = useState<string | null>(null);

  const notDelegated =
    delegation.error instanceof ApiError && delegation.error.status === 404;
  const live = delegation.data?.live === true && !notDelegated;

  const grant = async () => {
    setError(null);
    try {
      await session.delegate();
      await put.mutateAsync({
        signer_id: session.signerId,
        policy_id: session.policyId,
        // perpl caps count margin in AUSD atoms, kuru counts what a trade spends
        venues: ["kuru", "perpl"],
        markets: (markets.data ?? [])
          .filter((m) => m.venue === "kuru" || m.venue === "perpl")
          .map((m) => m.id),
        max_notional_per_slice: perSlice,
        max_notional_total: total,
        expires_at: new Date(Date.now() + hours * 3_600_000).toISOString(),
      });
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const revoke = async () => {
    setError(null);
    try {
      await session.revoke();
      await revokeRow.mutateAsync();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  if (!session.ready) {
    return (
      <span className="h-9 w-36 animate-pulse rounded-full bg-secondary/60" aria-hidden />
    );
  }

  if (!session.authenticated) {
    return (
      <Button variant="ghost" onClick={session.login} className="rounded-full">
        Sign in
      </Button>
    );
  }

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex items-center gap-2 rounded-full bg-secondary/50 py-1.5 pl-1.5 pr-3 text-sm transition-colors hover:bg-secondary"
      >
        <span className="grid size-7 place-items-center rounded-full bg-secondary text-muted-foreground">
          <UserRound className="size-4" />
        </span>
        <span className="tnum">
          {session.address ? truncateAddress(session.address) : "No wallet"}
        </span>
        <Pill tone={live ? "positive" : "neutral"}>{live ? "delegated" : "manual"}</Pill>
        <ChevronDown className="size-4 text-muted-foreground" />
      </button>

      {open && (
        <div className="absolute right-0 z-30 mt-2 w-80 space-y-3 rounded-xl border border-border bg-popover p-4 shadow-xl">
          <div className="space-y-1">
            <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
              Embedded wallet
            </div>
            <div className="tnum text-sm break-all">{session.address ?? "—"}</div>
          </div>

          <div className="h-px bg-border" />

          {live ? (
            <>
              <p className="text-xs text-muted-foreground">
                A signer executes within your limits until{" "}
                {new Date(delegation.data!.expires_at).toLocaleString()}. Revoking removes
                every signer on this wallet — Privy has no selective removal.
              </p>
              <Button
                variant="ghost"
                className="w-full"
                pending={session.delegating || revokeRow.isPending}
                onClick={revoke}
              >
                Revoke delegation
              </Button>
            </>
          ) : (
            <>
              <p className="text-xs text-muted-foreground">
                Automation rules run while you are logged out, so they need a delegated
                signer. Your keys stay with Privy; the limits below cap what it may spend.
              </p>
              {!session.signerConfigured && (
                <ErrorState message="NEXT_PUBLIC_PRIVY_SIGNER_ID is not set, so this build cannot name a signer to delegate to." />
              )}
              <label className="block space-y-1 text-xs text-muted-foreground">
                Per-order cap (base units)
                <input
                  value={perSlice}
                  onChange={(e) => setPerSlice(e.target.value)}
                  inputMode="numeric"
                  className="tnum w-full rounded-xl border border-input bg-secondary/40 px-3 py-2 text-sm text-foreground outline-none"
                />
              </label>
              <label className="block space-y-1 text-xs text-muted-foreground">
                Total cap (base units)
                <input
                  value={total}
                  onChange={(e) => setTotal(e.target.value)}
                  inputMode="numeric"
                  className="tnum w-full rounded-xl border border-input bg-secondary/40 px-3 py-2 text-sm text-foreground outline-none"
                />
              </label>
              <label className="block space-y-1 text-xs text-muted-foreground">
                Expires in
                <select
                  value={hours}
                  onChange={(e) => setHours(Number(e.target.value))}
                  className="w-full rounded-xl border border-input bg-secondary/40 px-3 py-2 text-sm text-foreground outline-none"
                >
                  {EXPIRY.map((e) => (
                    <option key={e.hours} value={e.hours}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </label>
              <Button
                className="w-full"
                pending={session.delegating || put.isPending}
                disabled={!session.signerConfigured || !session.address}
                onClick={grant}
              >
                Delegate signer
              </Button>
            </>
          )}

          {(error ?? session.error) && <ErrorState message={error ?? session.error!} />}

          <div className="h-px bg-border" />
          <Button variant="ghost" className="w-full" onClick={session.logout}>
            Sign out
          </Button>
        </div>
      )}
    </div>
  );
}
