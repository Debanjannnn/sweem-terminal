"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { AmountInput } from "@/components/ticket/AmountInput";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Dropdown } from "@/components/ui/Dropdown";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Pill } from "@/components/ui/Pill";
import { Skeleton } from "@/components/ui/Skeleton";
import { ApiError, errorMessage, toUnits, type Schedule, type SliceState } from "@/lib/api";
import {
  useCancelSchedule,
  useCreateSchedule,
  useDelegation,
  useMarkets,
  useSchedule,
  useSchedules,
} from "@/lib/query";
import { useSession } from "@/lib/session";
import { SectionTitle } from "@/components/ui/SectionTitle";
import { SideToggle } from "@/components/ticket/SideToggle";


const SLICE_TONE: Record<SliceState, "neutral" | "positive" | "warning" | "destructive"> = {
  pending: "neutral",
  dispatched: "warning",
  filled: "positive",
  failed: "destructive",
  skipped: "warning",
};

const COPY = {
  twap: { title: "TWAP", empty: "No TWAP schedules yet." },
  dca: { title: "DCA", empty: "No DCA plans yet." },
  grid: { title: "Grid", empty: "No grid strategies yet." },
} as const;

export function SchedulePanel({ kind }: { kind: "twap" | "dca" | "grid" }) {
  const { authenticated, login } = useSession();
  const markets = useMarkets();
  const schedules = useSchedules();
  const create = useCreateSchedule();
  const cancel = useCancelSchedule();
  const delegation = useDelegation();

  const [marketId, setMarketId] = useState<string | null>(null);
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [total, setTotal] = useState("");
  const [slices, setSlices] = useState("4");
  const [minutes, setMinutes] = useState("60");
  const [levels, setLevels] = useState("");

  // only kuru is routable, so only kuru markets can carry a schedule
  const tradable = (markets.data ?? []).filter((m) => m.venue === "kuru");
  useEffect(() => {
    if (tradable.length && !tradable.some((m) => m.id === marketId)) {
      setMarketId(tradable[0].id);
    }
  }, [tradable, marketId]);

  const market = tradable.find((m) => m.id === marketId) ?? null;
  const mine = (schedules.data?.schedules ?? []).filter((s) => s.kind === kind);
  const notDelegated =
    delegation.error instanceof ApiError && delegation.error.status === 404;

  const submit = async () => {
    if (!market || !total) return;
    const decimals = side === "buy" ? market.quoteDecimals : market.baseDecimals;
    try {
      await create.mutateAsync({
        kind,
        market_id: market.id,
        venue: market.venue,
        side,
        total: toUnits(total, decimals),
        slice_count: kind === "grid" ? undefined : Number(slices),
        window_seconds: kind === "twap" ? Number(minutes) * 60 : undefined,
        cadence_seconds: kind === "dca" ? Number(minutes) * 60 : undefined,
        levels:
          kind === "grid"
            ? levels
                .split(",")
                .map((l) => l.trim())
                .filter(Boolean)
            : undefined,
        idempotency_key: crypto.randomUUID(),
      });
      toast.success(`${COPY[kind].title} created`);
      setTotal("");
    } catch {
      // the backend's sentence renders under the button
    }
  };

  const amountLabel = market
    ? `Total (${side === "buy" ? market.quote : market.base})`
    : "Total";

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
      {authenticated && (notDelegated || delegation.data?.live === false) && (
        <Card className="p-4">
          <ErrorState message="A scheduled strategy runs while you are logged out, so it needs a delegated signer. Grant one from the wallet menu before creating it." />
        </Card>
      )}

      <Card className="space-y-4 p-4">
        <SectionTitle as="h1">New {COPY[kind].title}</SectionTitle>

        {markets.isError ? (
          <ErrorState message={errorMessage(markets.error)} />
        ) : markets.isPending ? (
          <Skeleton className="h-10 rounded-xl" />
        ) : tradable.length === 0 ? (
          <EmptyState message="No routable market is listed, so nothing can be scheduled." />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <span className="text-xs text-muted-foreground">Market</span>
              <Dropdown
                value={marketId}
                onChange={setMarketId}
                options={tradable.map((m) => ({ value: m.id, label: m.symbol }))}
                ariaLabel="Market"
                className="block"
              />
            </div>

            <div className="space-y-1.5">
              <span className="text-xs text-muted-foreground">Side</span>
              <SideToggle
                id={`${kind}-side`}
                value={side === "buy" ? "long" : "short"}
                onChange={(v) => setSide(v === "long" ? "buy" : "sell")}
                labels={["Buy", "Sell"]}
              />
            </div>

            <AmountInput
              label={amountLabel}
              value={total}
              onChange={setTotal}
              suffix={market ? (side === "buy" ? market.quote : market.base) : ""}
            />

            {kind === "grid" ? (
              <AmountInput
                label="Levels (comma separated prices)"
                value={levels}
                onChange={setLevels}
                suffix={market?.quote ?? ""}
              />
            ) : (
              <>
                <AmountInput
                  label="Slices"
                  value={slices}
                  onChange={setSlices}
                  suffix="orders"
                />
                <AmountInput
                  label={kind === "twap" ? "Window" : "Every"}
                  value={minutes}
                  onChange={setMinutes}
                  suffix="minutes"
                />
              </>
            )}
          </div>
        )}

        {authenticated ? (
          <Button
            onClick={submit}
            pending={create.isPending}
            disabled={!market || !total || (kind === "grid" ? !levels : !slices)}
          >
            Create {COPY[kind].title}
          </Button>
        ) : (
          <Button onClick={login}>Sign in to schedule</Button>
        )}

        {/* a 412 names exactly what is missing; render its sentence, not a generic error */}
        {create.error && <ErrorState message={errorMessage(create.error)} />}
      </Card>

      <Card className="space-y-3 p-4">
        <SectionTitle>{COPY[kind].title} schedules</SectionTitle>
        {!authenticated ? (
          <EmptyState message="Sign in to see your schedules." />
        ) : schedules.isError ? (
          <ErrorState message={errorMessage(schedules.error)} />
        ) : schedules.isPending ? (
          <div className="space-y-2">
            <Skeleton className="h-16 rounded-2xl" />
            <Skeleton className="h-16 rounded-2xl" />
          </div>
        ) : mine.length === 0 ? (
          <EmptyState message={COPY[kind].empty} />
        ) : (
          <div className="space-y-2">
            {mine.map((s) => (
              <ScheduleRow
                key={s.id}
                schedule={s}
                onCancel={() => cancel.mutate(s.id)}
                cancelling={cancel.isPending}
              />
            ))}
          </div>
        )}
        {cancel.error && <ErrorState message={errorMessage(cancel.error)} />}
      </Card>
    </div>
  );
}

function ScheduleRow({
  schedule,
  onCancel,
  cancelling,
}: {
  schedule: Schedule;
  onCancel: () => void;
  cancelling: boolean;
}) {
  const [open, setOpen] = useState(false);
  const detail = useSchedule(open ? schedule.id : null);
  const slices = detail.data?.slices ?? [];

  return (
    <Card className="space-y-3 bg-background/40 p-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="font-medium hover:underline"
        >
          {schedule.market_id}
        </button>
        <Pill tone={schedule.side === "buy" ? "long" : "short"}>
          {schedule.side}
        </Pill>
        <Pill tone={schedule.state === "active" ? "positive" : "neutral"}>
          {schedule.state}
        </Pill>
        <span className="tnum text-xs text-muted-foreground">
          {schedule.slice_count} slices
        </span>
        {schedule.state === "active" && (
          <Button
            variant="ghost"
            className="ml-auto px-3 py-1 text-xs"
            pending={cancelling}
            onClick={onCancel}
          >
            Cancel
          </Button>
        )}
      </div>

      {open &&
        (detail.isError ? (
          <ErrorState message={errorMessage(detail.error)} />
        ) : detail.isPending ? (
          <Skeleton className="h-8" />
        ) : slices.length === 0 ? (
          <EmptyState message="This schedule has no slices." />
        ) : (
          <ul className="space-y-1">
            {slices.map((sl) => (
              <li
                key={`${sl.slice_index}:${sl.generation}`}
                className="flex flex-wrap items-center gap-2 text-xs"
              >
                <span className="tnum w-8 text-muted-foreground">#{sl.slice_index}</span>
                <Pill tone={SLICE_TONE[sl.state]}>{sl.state}</Pill>
                <span className="tnum text-muted-foreground">
                  {new Date(sl.due_at).toLocaleString()}
                </span>
                {sl.skip_reason && (
                  <span className="text-muted-foreground">{sl.skip_reason}</span>
                )}
              </li>
            ))}
          </ul>
        ))}
    </Card>
  );
}
