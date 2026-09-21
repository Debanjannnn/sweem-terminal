import { Pill } from "@/components/ui/Pill";
import { cn } from "@/lib/cn";

export type TxTier = "seen" | "safe" | "final" | "failed";

const TIERS = {
  seen: { tone: "warning", label: "Seen", dot: "bg-warning animate-pulse" },
  safe: { tone: "positive", label: "Safe", dot: "border border-positive" },
  final: { tone: "positive", label: "Final", dot: "bg-positive" },
  failed: { tone: "destructive", label: "Failed", dot: "bg-destructive" },
} as const;

export function TxState({ tier }: { tier: TxTier }) {
  const t = TIERS[tier];
  return (
    <Pill tone={t.tone}>
      <span className={cn("size-1.5 rounded-full", t.dot)} />
      {t.label}
    </Pill>
  );
}
