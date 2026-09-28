"use client";

import { useState } from "react";
import { motion } from "motion/react";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Pill } from "@/components/ui/Pill";
import { Segmented } from "@/components/ui/Segmented";
import { Skeleton } from "@/components/ui/Skeleton";
import { STAGGER_STEP, rise } from "@/components/motion";
import { PositionCard, type Position, type PositionStatus } from "./PositionCard";

const FILTERS = [
  { value: "open", label: "Open" },
  { value: "pending", label: "Pending" },
  { value: "cancelled", label: "Cancelled" },
] as const;

const EMPTY: Record<PositionStatus, string> = {
  open: "No open positions.",
  pending: "No pending orders.",
  cancelled: "No cancelled orders.",
};

export function PositionsPanel({
  positions = [],
  loading = false,
  error = null,
  degraded = [],
  onClose,
  closingId,
}: {
  positions?: Position[];
  loading?: boolean;
  error?: string | null;
  degraded?: string[];
  onClose?: (p: Position) => void;
  closingId?: string;
}) {
  const [filter, setFilter] = useState<PositionStatus>("open");
  const shown = positions.filter((p) => p.status === filter);

  return (
    <div className="space-y-4 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <Segmented
          id="positions-filter"
          value={filter}
          onChange={setFilter}
          options={FILTERS}
          className="w-fit"
        />
        {degraded.length > 0 && (
          <Pill tone="warning">
            {degraded.join(" and ")} unavailable — figures from {degraded.join("/")} are
            missing, not zero
          </Pill>
        )}
      </div>

      {error ? (
        <ErrorState message={error} />
      ) : loading ? (
        <div className="space-y-2">
          <Skeleton className="h-20 rounded-2xl" />
          <Skeleton className="h-20 rounded-2xl" />
          <Skeleton className="h-20 rounded-2xl" />
        </div>
      ) : shown.length === 0 ? (
        <EmptyState message={EMPTY[filter]} />
      ) : (
        <div className="space-y-2">
          {shown.map((p, i) => (
            <motion.div
              key={p.id}
              {...rise}
              transition={{ ...rise.transition, delay: i * STAGGER_STEP }}
            >
              <PositionCard position={p} onClose={onClose} closing={closingId === p.id} />
            </motion.div>
          ))}
        </div>
      )}
    </div>
  );
}
