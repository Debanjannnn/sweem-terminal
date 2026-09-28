"use client";

import { useParams } from "next/navigation";
import { getBasket } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { useTopic } from "@/lib/ws";
import type { LiveNav } from "@/lib/types";
import { ErrorBox, Skeleton } from "@/components/ui";

export default function BasketPage() {
  const { address } = useParams<{ address: string }>();
  const basket = useAsync(`basket:${address}`, () => getBasket(address));
  const nav = useTopic<LiveNav>(`nav.${address}`);
  return (
    <div className="w-full space-y-3 px-4 py-10">
      {basket.error && <ErrorBox message={basket.error} />}
      {basket.loading && <Skeleton className="h-16 w-full" />}
      {basket.data && <h1 className="text-2xl font-semibold">{basket.data.name}</h1>}
      <p className="tnum text-muted-foreground">NAV/share {nav?.navPerShare ?? basket.data?.nav?.navPerShare ?? "—"}</p>
    </div>
  );
}
