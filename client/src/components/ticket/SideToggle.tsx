"use client";

import { motion, useReducedMotion } from "motion/react";
import { PILL_SPRING } from "@/components/motion";
import { cn } from "@/lib/cn";

export type Side = "long" | "short";

const SIDES: { value: Side; fill: string; text: string }[] = [
  { value: "long", fill: "bg-long", text: "text-long-foreground" },
  { value: "short", fill: "bg-short", text: "text-short-foreground" },
];

// spot buys and sells; a perp goes long or short. same control, same colours.
export function SideToggle({
  value,
  onChange,
  labels = ["Long", "Short"],
  id = "side-toggle",
}: {
  value: Side;
  onChange: (v: Side) => void;
  labels?: [string, string];
  id?: string;
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
                  layoutId={id}
                  className={cn("absolute inset-0 rounded-lg", s.fill)}
                  transition={PILL_SPRING}
                />
              ))}
            <span className="relative">{labels[s.value === "long" ? 0 : 1]}</span>
          </button>
        );
      })}
    </div>
  );
}
