"use client";

import { motion, useReducedMotion } from "motion/react";
import { PILL_SPRING } from "@/components/motion";
import { cn } from "@/lib/cn";

export function Segmented<T extends string>({
  id,
  value,
  onChange,
  options,
  tone = "neutral",
  className,
}: {
  id: string;
  value: T;
  onChange: (v: T) => void;
  options: readonly { value: T; label: string }[];
  tone?: "neutral" | "accent";
  className?: string;
}) {
  const reduced = useReducedMotion();
  const fill = tone === "accent" ? "bg-primary" : "bg-secondary";
  return (
    <div
      role="tablist"
      className={cn(
        "grid grid-flow-col rounded-xl border border-border bg-card p-1",
        className,
      )}
    >
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            role="tab"
            aria-selected={on}
            onClick={() => onChange(o.value)}
            className={cn(
              "relative rounded-lg px-4 py-2 text-sm font-medium transition-colors",
              on
                ? tone === "accent"
                  ? "text-primary-foreground"
                  : "text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {on &&
              (reduced ? (
                <span className={cn("absolute inset-0 rounded-lg", fill)} />
              ) : (
                <motion.span
                  layoutId={`segmented-${id}`}
                  className={cn("absolute inset-0 rounded-lg", fill)}
                  transition={PILL_SPRING}
                />
              ))}
            <span className="relative">{o.label}</span>
          </button>
        );
      })}
    </div>
  );
}
