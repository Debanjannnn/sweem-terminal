"use client";

import { motion, useReducedMotion } from "motion/react";
import { cn } from "@/lib/utils";

const EASE = [0.16, 1, 0.3, 1] as const;

export function Reveal({
  children,
  delay = 0,
  className,
  y = 10,
}: {
  children: React.ReactNode;
  delay?: number;
  className?: string;
  y?: number;
}) {
  const reduced = useReducedMotion();
  if (reduced) return <div className={className}>{children}</div>;
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, delay, ease: EASE }}
    >
      {children}
    </motion.div>
  );
}

export function Segmented<T extends string>({
  value,
  onChange,
  options,
  id,
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { value: T; label: string }[];
  id: string;
  className?: string;
}) {
  const reduced = useReducedMotion();
  return (
    <div
      role="tablist"
      className={cn(
        "grid w-full grid-flow-col rounded-lg bg-foreground/[0.05] p-0.5",
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
              "relative rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
              on ? "text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {on &&
              (reduced ? (
                <span className="absolute inset-0 rounded-md bg-card shadow-sm" />
              ) : (
                <motion.span
                  layoutId={`segmented-${id}`}
                  className="absolute inset-0 rounded-md bg-card shadow-sm"
                  transition={{ type: "spring", stiffness: 400, damping: 34 }}
                />
              ))}
            <span className="relative">{o.label}</span>
          </button>
        );
      })}
    </div>
  );
}
