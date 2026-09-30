"use client";

import { ShieldCheck } from "lucide-react";
import { useSession } from "@/lib/session";
import { Button, ErrorBox, Panel, StatusPill } from "@/components/ui";
import { fmtTime } from "@/lib/api";

export const POLICY_COPY =
  "The Privy policy on Monad (chain 143) only lets sweem's key: trade on the 4 Kuru order books and margin account, approve those venues, supply/withdraw on Aave, Neverland and listed ERC-4626 vaults, stake/redeem shMON, deposit/withdraw the Kuru ActiveVault, open/close Perpl hedges (via the AUSD 3pool), and send USDC only to the sweem fee treasury. Everything else is denied; funds never leave your wallet's own positions. Revoke any time.";

// Delegation status with enable/revoke; compact = inline prompt used inside ScheduleForm.
export function DelegationCard({ compact = false }: { compact?: boolean }) {
  const { me, delegated, delegate, revoke, delegating, delegationError, signerConfigured, wallet } = useSession();
  const body = (
    <div className="space-y-3 p-5">
      <div className="flex flex-wrap items-center gap-3">
        <ShieldCheck className="size-4 text-muted-foreground" />
        <span className="font-medium">Automation</span>
        <StatusPill status={delegated ? "confirmed" : "rejected"} />
        <span className="text-xs text-muted-foreground">{delegated ? "enabled" : "off"}</span>
        {delegated ? (
          <Button variant="ghost" className="ml-auto" pending={delegating} onClick={() => void revoke()}>
            Revoke
          </Button>
        ) : (
          <Button
            className="ml-auto"
            pending={delegating}
            disabled={!wallet || !signerConfigured}
            onClick={() => void delegate()}
          >
            Enable delegation
          </Button>
        )}
      </div>
      <p className="text-xs leading-5 text-muted-foreground">
        {delegated
          ? `Enabled ${fmtTime(me!.delegation!.createdAt)}. Revoking also stops every schedule.`
          : "Only needed for automation: DCA, take-profit / stop-loss, rebalancing and Earn strategy deploy / withdraw run from your own Privy wallet via a delegated signer, even while you're offline. Manual trades don't need it; you sign those yourself."}{" "}
        {POLICY_COPY}
      </p>
      {!signerConfigured && <ErrorBox message="NEXT_PUBLIC_PRIVY_SIGNER_ID is not set" />}
      {delegationError && <ErrorBox message={delegationError} />}
    </div>
  );
  return compact ? <div className="rounded-xl border border-border bg-secondary/30">{body}</div> : <Panel>{body}</Panel>;
}
