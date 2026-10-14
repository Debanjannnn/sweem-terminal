"use client";

import Link from "next/link";
import { errText, getPositions } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { Button, ErrorBox, Panel, Skeleton, StatusPill } from "@/components/ui";
import { HedgeMarkets, PositionRisk } from "@/components/perpl/PerplRisk";

// Hedge risk: the Perpl markets basis baskets short (protocol view) above the viewer's live hedges (wallet view)
export default function RiskPage() {
  const { ready, authenticated, login } = useSession();
  const on = ready && authenticated;
  const positions = useAsync(on ? "positions" : null, getPositions, 30_000);
  const hedged = (positions.data ?? []).filter((p) => p.status !== "closed" && p.status !== "failed" && p.legs.some((l) => l.kind === "basis"));

  return (
    <main className="w-full px-4 pb-24 pt-8 md:px-6">
      <div className="mx-auto w-full space-y-6">
        <header className="space-y-1 px-1">
          <h1 className="text-2xl font-medium">Risk</h1>
          <p className="text-sm text-muted-foreground">
            Delta-neutral baskets short on Perpl. The markets that set your funding and liquidation risk, and every hedge you hold, as the guard
            sees it.
          </p>
        </header>

        <HedgeMarkets />

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-medium">Your hedges</h2>
          {!ready ? (
            <Skeleton className="h-40 w-full rounded-3xl" />
          ) : !authenticated ? (
            <Panel className="flex flex-wrap items-center justify-between gap-3 p-5">
              <p className="text-sm text-muted-foreground">Log in to see live margin, liquidation and guard levels for your hedged baskets.</p>
              <Button onClick={login}>Log in</Button>
            </Panel>
          ) : positions.error ? (
            <ErrorBox message={errText(new Error(positions.error))} />
          ) : !positions.data ? (
            <Skeleton className="h-40 w-full rounded-3xl" />
          ) : hedged.length === 0 ? (
            <Panel className="p-5 text-sm text-muted-foreground">
              No hedged baskets yet. Add a <span className="text-foreground">Basis</span> source when you{" "}
              <Link href="/baskets/new" className="text-foreground underline underline-offset-4 hover:no-underline">
                create a basket
              </Link>{" "}
              and its short shows up here.
            </Panel>
          ) : (
            <div className="grid gap-3 xl:grid-cols-2">
              {hedged.map((p) => (
                <Panel key={p.id} className="space-y-3 p-5">
                  <div className="flex items-center gap-3">
                    <Link href={`/baskets/${p.strategyId}`} className="truncate font-medium hover:underline">
                      {p.strategyName}
                    </Link>
                    <span className="ml-auto">
                      <StatusPill status={p.status} />
                    </span>
                  </div>
                  <PositionRisk positionId={p.id} />
                </Panel>
              ))}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
