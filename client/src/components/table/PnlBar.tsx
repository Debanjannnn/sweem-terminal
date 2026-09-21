import { formatUsd } from "@/lib/format";
import { cn } from "@/lib/cn";

export function PnlBar({
  stopLoss,
  takeProfit,
  markPrice,
  up,
}: {
  stopLoss: number;
  takeProfit: number;
  markPrice: number;
  up: boolean;
}) {
  const lo = Math.min(stopLoss, takeProfit);
  const hi = Math.max(stopLoss, takeProfit);
  const pct = hi === lo ? 50 : ((markPrice - lo) / (hi - lo)) * 100;
  const left = Math.min(100, Math.max(0, pct));

  return (
    <div className="space-y-1.5">
      <div className="relative h-1 rounded-full bg-secondary">
        <span
          style={{ left: `${left}%` }}
          className={cn(
            "absolute -top-1 size-3 -translate-x-1/2 rounded-full",
            up ? "bg-long" : "bg-short",
          )}
        />
      </div>
      <div className="flex justify-between text-[11px] text-muted-foreground">
        <span className="tnum">SL {formatUsd(stopLoss)}</span>
        <span className="tnum">TP {formatUsd(takeProfit)}</span>
      </div>
    </div>
  );
}
