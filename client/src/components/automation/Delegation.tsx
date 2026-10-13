"use client";

import { ChevronDown, ShieldCheck } from "lucide-react";
import { useSession } from "@/lib/session";
import { Button, ErrorBox, Panel } from "@/components/ui";
import { fmtTime } from "@/lib/api";
import { cn } from "@/lib/utils";

const POLICY = [
  "Trade on Kuru order books and margin account",
  "Supply / withdraw on Aave, Neverland and ERC-4626 vaults",
  "Stake / redeem shMON, Kuru ActiveVault deposits",
  "Open / close Perpl hedges via the AUSD 3pool",
  "Send USDC only to the sweem fee treasury",
];

// Delegation status with enable/revoke; compact = inline prompt used inside ScheduleForm.
export function DelegationCard({ compact = false }: { compact?: boolean }) {
  const { me, delegated, delegate, revoke, delegating, delegationError, signerConfigured, wallet } = useSession();
  const body = (
    <div className="space-y-4 p-5">
      <div className="flex items-start gap-3">
        <span className={cn("grid size-9 shrink-0 place-items-center rounded-full", delegated ? "bg-positive/15 text-positive" : "bg-secondary text-muted-foreground")}>
          <ShieldCheck className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-2 font-medium">
            Automation
            <span className={cn("rounded-md px-1.5 py-0.5 text-[11px]", delegated ? "bg-positive/15 text-positive" : "bg-secondary text-muted-foreground")}>
              {delegated ? "On" : "Off"}
            </span>
          </p>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            {delegated
              ? `Enabled ${fmtTime(me!.delegation!.createdAt)}. Revoking stops every schedule.`
              : "Lets DCA, TP/SL and baskets run from your wallet while you're offline."}
          </p>
        </div>
      </div>
      <details className="group text-xs text-muted-foreground">
        <summary className="flex cursor-pointer list-none items-center gap-1 hover:text-foreground [&::-webkit-details-marker]:hidden">
          What can it do?
          <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
        </summary>
        <ul className="mt-2 space-y-1 pl-4 leading-5 [&>li]:list-disc">
          {POLICY.map((l) => (
            <li key={l}>{l}</li>
          ))}
          <li>Everything else denied. Funds stay in your wallet. Revoke any time.</li>
        </ul>
      </details>
      {delegated ? (
        <Button variant="ghost" className="w-full" pending={delegating} onClick={() => void revoke()}>
          Revoke
        </Button>
      ) : (
        <Button className="w-full" pending={delegating} disabled={!wallet || !signerConfigured} onClick={() => void delegate()}>
          Enable delegation
        </Button>
      )}
      {!signerConfigured && <ErrorBox message="NEXT_PUBLIC_PRIVY_SIGNER_ID is not set" />}
      {delegationError && <ErrorBox message={delegationError} />}
    </div>
  );
  return compact ? <div className="rounded-xl border border-border bg-secondary/30">{body}</div> : <Panel>{body}</Panel>;
}
