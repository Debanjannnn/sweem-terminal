"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import { cn } from "@/lib/cn";

export function useDismissable() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return { open, setOpen, ref };
}

export function Dropdown<T extends string>({
  value,
  onChange,
  options,
  ariaLabel,
  placeholder = "none",
  className,
}: {
  value: T | null;
  onChange: (v: T) => void;
  options: readonly { value: T; label: string; hint?: string }[];
  ariaLabel: string;
  placeholder?: string;
  className?: string;
}) {
  const { open, setOpen, ref } = useDismissable();
  const current = options.find((o) => o.value === value);

  return (
    <div className={cn("relative shrink-0", className)} ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        disabled={options.length === 0}
        aria-label={ariaLabel}
        aria-expanded={open}
        className="inline-flex max-w-56 items-center gap-2 rounded-full bg-secondary/70 py-2 pl-4 pr-3 text-sm font-medium transition-colors hover:bg-secondary disabled:cursor-not-allowed disabled:text-muted-foreground"
      >
        <span className="truncate">{current?.label ?? placeholder}</span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
      </button>
      {open && (
        <ul className="absolute right-0 z-30 mt-2 max-h-72 w-60 overflow-y-auto rounded-xl border border-border bg-popover p-1 shadow-xl">
          {options.map((o) => (
            <li key={o.value}>
              <button
                type="button"
                onClick={() => {
                  onChange(o.value);
                  setOpen(false);
                }}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-secondary/70"
              >
                <span className="truncate">{o.label}</span>
                {o.hint && (
                  <span className="ml-auto tnum text-xs text-muted-foreground">
                    {o.hint}
                  </span>
                )}
                {o.value === value && (
                  <Check className={cn("size-4", o.hint ? "" : "ml-auto")} />
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
