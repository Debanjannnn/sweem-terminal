"use client";

import { ShieldCheck } from "lucide-react";
import { useSession } from "@/lib/session";
import { Button, ErrorBox, Panel, StatusPill } from "@/components/ui";
import { fmtTime } from "@/lib/api";

export const POLICY_COPY =
  "sweem's automation key can only call mint, redeem and approve on sweem baskets from your wallet. It can't transfer funds anywhere else, and you can revoke it at any time.";

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
            Enable automation
          </Button>
        )}
      </div>
      <p className="text-xs leading-5 text-muted-foreground">
        {delegated
          ? `Enabled ${fmtTime(me!.delegation!.createdAt)}. Revoking also stops every schedule.`
          : "Recurring buys and stop-losses run while you're offline, signed from your Privy wallet."}{" "}
        {POLICY_COPY}
      </p>
      {!signerConfigured && <ErrorBox message="NEXT_PUBLIC_PRIVY_SIGNER_ID is not set" />}
      {delegationError && <ErrorBox message={delegationError} />}
    </div>
  );
  return compact ? <div className="rounded-xl border border-border bg-secondary/30">{body}</div> : <Panel>{body}</Panel>;
}
