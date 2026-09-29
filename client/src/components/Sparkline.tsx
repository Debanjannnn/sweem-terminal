import { useId } from "react";
import { cn } from "@/lib/utils";

// green when last >= first, red otherwise; flat/empty series → muted line
export function Sparkline({ data, area, className }: { data: number[]; area?: boolean; className?: string }) {
  const id = useId();
  const min = Math.min(...data);
  const max = Math.max(...data);
  if (data.length < 2 || max === min)
    return (
      <div aria-hidden className={cn("flex items-center", className)}>
        <div className="h-px w-full bg-muted-foreground/30" />
      </div>
    );

  const pts = data.map((v, i) => [(i / (data.length - 1)) * 100, 6 + (1 - (v - min) / (max - min)) * 88]);
  const line = pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(2)},${y.toFixed(2)}`).join("");
  const [ex, ey] = pts[pts.length - 1];
  const up = data[data.length - 1] >= data[0];

  return (
    <div aria-hidden className={cn("relative", up ? "text-positive" : "text-destructive", className)}>
      <svg viewBox="0 0 100 100" preserveAspectRatio="none" className="size-full overflow-visible">
        {area && (
          <>
            <defs>
              <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="currentColor" stopOpacity={0.3} />
                <stop offset="1" stopColor="currentColor" stopOpacity={0} />
              </linearGradient>
            </defs>
            <path d={`${line}L100,100L0,100Z`} fill={`url(#${id})`} />
          </>
        )}
        <path
          d={line}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          strokeLinejoin="round"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      {/* html dot so it stays round under preserveAspectRatio=none */}
      <span
        className="absolute size-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-current"
        style={{ left: `${ex}%`, top: `${ey}%` }}
      />
    </div>
  );
}
