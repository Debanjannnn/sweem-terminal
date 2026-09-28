"use client";

import { getPositions } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { ErrorBox, Skeleton } from "@/components/ui";

export default function PortfolioPage() {
  const { authenticated, login } = useSession();
  const positions = useAsync(authenticated ? "positions" : null, getPositions);
  if (!authenticated)
    return (
      <div className="px-4 py-10">
        <button onClick={login} className="rounded-full bg-primary px-6 py-2.5 text-sm font-medium text-primary-foreground">
          Connect
        </button>
      </div>
    );
  return (
    <div className="w-full space-y-3 px-4 py-10">
      {positions.error && <ErrorBox message={positions.error} />}
      {positions.loading && <Skeleton className="h-16 w-full" />}
      {positions.data?.map((p) => (
        <p key={p.basket} className="tnum">
          {p.basket} · {p.valueUsdc ?? "—"} USDC
        </p>
      ))}
    </div>
  );
}
