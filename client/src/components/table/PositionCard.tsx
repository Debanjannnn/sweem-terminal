import { Card } from "@/components/ui/Card";
import { Pill } from "@/components/ui/Pill";
import { formatPctSigned, formatSize, formatUsd } from "@/lib/format";
import { cn } from "@/lib/cn";
import { PnlBar } from "./PnlBar";
import { TxState, type TxTier } from "./TxState";

export type PositionStatus = "open" | "pending" | "cancelled";

export type Position = {
  id: string;
  symbol: string;
  side: "long" | "short";
  status: PositionStatus;
  leverage?: number;
  size: number;
  entryPrice?: number;
  // omitted when market-data could not price the market
  markPrice?: number;
  pnl?: number;
  pnlPct?: number;
  stopLoss?: number;
  takeProfit?: number;
  tx?: TxTier;
  note?: string;
};

const value = (n: number | undefined, fmt: (n: number) => string) =>
  n === undefined || Number.isNaN(n) ? "—" : fmt(n);

export function PositionCard({ position: p }: { position: Position }) {
  const up = (p.pnl ?? 0) >= 0;
  return (
    <Card className="space-y-3 bg-background/40 p-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span className="font-medium">{p.symbol}</span>
        <Pill tone={p.side === "long" ? "positive" : "destructive"}>
          {p.side}
          {p.leverage !== undefined && ` ${p.leverage}x`}
        </Pill>
        <Pill>{p.status}</Pill>
        {p.tx && <TxState tier={p.tx} />}
        <span
          className={cn(
            "tnum ml-auto text-sm",
            p.pnl === undefined
              ? "text-muted-foreground"
              : up
                ? "text-long"
                : "text-short",
          )}
        >
          {value(p.pnl, formatUsd)} · {value(p.pnlPct, (n) => formatPctSigned(n))}
        </span>
      </div>

      <div className="flex flex-wrap gap-x-8 gap-y-1 text-xs text-muted-foreground">
        <span className="tnum">Size {formatSize(p.size)}</span>
        <span className="tnum">Entry {value(p.entryPrice, formatUsd)}</span>
        <span className="tnum">Mark {value(p.markPrice, formatUsd)}</span>
      </div>

      {p.note && <p className="text-xs text-muted-foreground">{p.note}</p>}

      {p.stopLoss !== undefined && p.takeProfit !== undefined && p.markPrice !== undefined && (
        <PnlBar
          stopLoss={p.stopLoss}
          takeProfit={p.takeProfit}
          markPrice={p.markPrice}
          up={up}
        />
      )}
    </Card>
  );
}
