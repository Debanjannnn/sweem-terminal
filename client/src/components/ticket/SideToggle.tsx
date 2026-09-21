"use client";

import { motion, useReducedMotion } from "motion/react";
import { PILL_SPRING } from "@/components/motion";
import { cn } from "@/lib/cn";

export type Side = "long" | "short";

const SIDES: { value: Side; label: string; fill: string; text: string }[] = [
  { value: "long", label: "Long", fill: "bg-long", text: "text-long-foreground" },
  { value: "short", label: "Short", fill: "bg-short", text: "text-short-foreground" },
];

export function SideToggle({
  value,
  onChange,
}: {
  value: Side;
  onChange: (v: Side) => void;
}) {
  const reduced = useReducedMotion();
  return (
    <div
      role="tablist"
      aria-label="Position side"
      className="grid grid-cols-2 rounded-xl border border-border bg-card p-1"
    >
      {SIDES.map((s) => {
        const on = s.value === value;
        return (
          <button
            key={s.value}
            role="tab"
            aria-selected={on}
            onClick={() => onChange(s.value)}
            className={cn(
              "relative rounded-lg px-4 py-2.5 text-sm font-semibold transition-colors",
              on ? s.text : "text-muted-foreground hover:text-foreground",
            )}
          >
            {on &&
              (reduced ? (
                <span className={cn("absolute inset-0 rounded-lg", s.fill)} />
              ) : (
                <motion.span
                  layoutId="side-toggle"
                  className={cn("absolute inset-0 rounded-lg", s.fill)}
                  transition={PILL_SPRING}
                />
              ))}
            <span className="relative">{s.label}</span>
          </button>
        );
      })}
    </div>
  );
}
