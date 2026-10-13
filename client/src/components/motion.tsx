"use client";

import { useEffect, useRef, useState } from "react";
import { MotionConfig, motion } from "motion/react";
import { cn } from "@/lib/utils";

export const EASE = [0.16, 1, 0.3, 1] as const;
const SPRING = { type: "spring", stiffness: 500, damping: 40 } as const;

// root: honours prefers-reduced-motion (drops transform/layout anims)
export function MotionRoot({ children }: { children: React.ReactNode }) {
  return <MotionConfig reducedMotion="user">{children}</MotionConfig>;
}

export function Reveal({
  children,
  delay = 0,
  className,
  y = 6,
}: {
  children: React.ReactNode;
  delay?: number;
  className?: string;
  y?: number;
}) {
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, delay, ease: EASE }}
    >
      {children}
    </motion.div>
  );
}

// track / item / pill overrides per size; xs = bare chips (slippage), lg = side-panel tabs
const SIZE = {
  xs: ["flex w-auto gap-1 bg-transparent p-0", "px-2 py-1 text-xs", ""],
  sm: ["flex w-auto", "py-1", ""],
  md: ["", "", ""],
  lg: ["gap-1 rounded-full border border-border bg-secondary/60 p-1", "truncate rounded-full py-2 text-[15px]", "rounded-full bg-background"],
} as const;

export function Segmented<T extends string>({
  value,
  onChange,
  options,
  id,
  className,
  size = "md",
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { value: T; label: string }[];
  id: string;
  className?: string;
  size?: keyof typeof SIZE;
}) {
  const [track, item, pill] = SIZE[size];
  return (
    <div
      role="tablist"
      className={cn(
        "grid w-full grid-flow-col rounded-lg bg-foreground/[0.05] p-0.5",
        track,
        className,
      )}
    >
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="tab"
            aria-selected={on}
            onClick={() => onChange(o.value)}
            className={cn(
              "relative rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
              item,
              on ? "text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {on && <TabPill id={`segmented-${id}`} className={cn("rounded-md bg-card shadow-sm", pill)} />}
            <span className="relative">{o.label}</span>
          </button>
        );
      })}
    </div>
  );
}

// sliding active-tab background; render inside a `relative` tab, label needs `relative` too
export function TabPill({ id, className }: { id: string; className?: string }) {
  return <motion.span aria-hidden layoutId={id} transition={SPRING} className={cn("absolute inset-0", className)} />;
}

// flashes green/red when a live number moves; colour only so width never shifts
export function Flash({ value, children, className }: { value: number | null | undefined; children: React.ReactNode; className?: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const prev = useRef(value);
  useEffect(() => {
    const p = prev.current;
    prev.current = value;
    if (p == null || value == null || p === value || !ref.current) return;
    ref.current.animate([{ color: `var(--${value > p ? "positive" : "destructive"})`, offset: 0 }], { duration: 700, easing: "ease-out" });
  }, [value]);
  return (
    <span ref={ref} className={className}>
      {children}
    </span>
  );
}

// animates its height to follow content (tab switches, quote rows appearing)
export function AutoHeight({ children, className }: { children: React.ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [h, setH] = useState<number | "auto">("auto");
  useEffect(() => {
    const el = ref.current!;
    const ro = new ResizeObserver(() => setH(el.offsetHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return (
    <motion.div className={cn("overflow-hidden", className)} initial={false} animate={{ height: h }} transition={{ duration: 0.25, ease: EASE }}>
      <div ref={ref}>{children}</div>
    </motion.div>
  );
}
