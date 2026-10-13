import { TokenIcon } from "@/components/TokenIcon";
import { cn } from "@/lib/utils";

// shared list/detail bits (file name kept: create/ imports IconStack from here)
export function IconStack({ symbols, size, ring }: { symbols: string[]; size: number; ring: string }) {
  return (
    <span aria-hidden className="flex shrink-0 -space-x-2">
      {symbols.slice(0, 3).map((s, i) => (
        <TokenIcon
          key={`${s}${i}`}
          symbol={s}
          size={size}
          className={cn("rounded-full ring-2 transition-transform duration-200 ease-out hover:z-10 hover:-translate-y-1", ring)}
        />
      ))}
    </span>
  );
}

// usd: absolute $ change (4dp) instead of %
export function ChangeText({ value, usd, className }: { value: number | null | undefined; usd?: boolean; className?: string }) {
  if (value === null || value === undefined) return <span className={cn("text-muted-foreground", className)}>-</span>;
  const abs = Math.abs(value);
  // sub-cent moves (MON) keep 2 significant digits instead of printing $0.0000
  const txt = usd && abs > 0 && abs < 0.01 ? abs.toPrecision(2) : abs.toFixed(2);
  const up = value >= 0 || Number(txt) === 0;
  return (
    <span className={cn("inline-flex items-center gap-1 font-mono", up ? "text-positive" : "text-destructive", className)}>
      <span aria-hidden className={cn("text-[0.6em]", !up && "rotate-180")}>▲</span>
      {usd ? `$${txt}` : `${txt}%`}
    </span>
  );
}

export function LiquidityBadge({ liquid }: { liquid: boolean | null | undefined }) {
  if (liquid === null || liquid === undefined) return null;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium whitespace-nowrap",
        liquid ? "bg-primary/10 text-primary" : "bg-warning/10 text-warning",
      )}
    >
      <span className={cn("size-1.5 rounded-full", liquid ? "bg-primary" : "bg-warning")} />
      {liquid ? "Open for trade" : "No liquidity"}
    </span>
  );
}
