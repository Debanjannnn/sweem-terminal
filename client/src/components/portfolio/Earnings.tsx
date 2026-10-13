"use client";

import Link from "next/link";
import { useState } from "react";
import { fmtTime, fmtUsd, getEarnings } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { Button, Panel, StatusPill } from "@/components/ui";

// Creator rev-share + fees you paid. Hidden while /me/earnings is unavailable.
export function EarningsCard() {
  const { ready, authenticated } = useSession();
  const e = useAsync(ready && authenticated ? "earnings" : null, getEarnings);
  const [open, setOpen] = useState(false);
  const d = e.data;
  if (!d) return null;
  return (
    <section className="space-y-3">
      <h2 className="px-1 text-lg font-medium">Creator earnings</h2>
      <Panel className="space-y-3 p-5">
        <dl className="tnum grid grid-cols-3 gap-3 text-sm">
          {[
            ["Owed to you", d.owedUsdc],
            ["Paid to you", d.paidUsdc],
            ["Fees you paid", d.feesPaidUsdc],
          ].map(([k, v]) => (
            <div key={k}>
              <dt className="text-xs text-muted-foreground">{k}</dt>
              <dd>{fmtUsd(Number(v))}</dd>
            </div>
          ))}
        </dl>
        {d.rows.length > 0 && (
          <Button variant="ghost" onClick={() => setOpen((o) => !o)}>
            {open ? "Hide" : `Show ${d.rows.length}`} fee{d.rows.length === 1 ? "" : "s"}
          </Button>
        )}
        {open && (
          <ul className="space-y-1 text-xs">
            {d.rows.map((r) => (
              <li key={r.executionId} className="flex items-center gap-2">
                <Link href={`/earn/${r.strategyId}`} className="hover:underline">
                  {fmtTime(r.createdAt)}
                </Link>
                <StatusPill status={r.paid ? "paid" : "owed"} />
                <span className="tnum ml-auto text-muted-foreground">
                  fee {fmtUsd(Number(r.feeUsdc))} · creator {fmtUsd(Number(r.creatorOwedUsdc))}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </section>
  );
}
