import { cn } from "@/lib/cn";

const TONE = {
  neutral: "border-border bg-secondary/60 text-muted-foreground",
  positive: "border-positive/30 bg-positive/10 text-positive",
  warning: "border-warning/30 bg-warning/10 text-warning",
  destructive: "border-destructive/30 bg-destructive/10 text-destructive",
};

export function Pill({
  children,
  tone = "neutral",
  className,
}: {
  children: React.ReactNode;
  tone?: keyof typeof TONE;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px]",
        TONE[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
