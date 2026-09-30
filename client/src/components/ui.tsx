"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { TabPill } from "./motion";

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

export function Spinner({ label }: { label?: string }) {
  return (
    <span className="inline-flex items-center gap-2 text-sm text-muted-foreground">
      <span className="size-3 animate-spin rounded-full border-2 border-border border-t-foreground/70" />
      {label}
    </span>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={cn("animate-pulse rounded-md bg-secondary/60", className)}
    />
  );
}

export function ErrorBox({ message }: { message: string }) {
  return (
    <p className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-xs break-words text-destructive">
      {message}
    </p>
  );
}

export function Button({
  children,
  variant = "primary",
  className = "",
  pending = false,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "ghost";
  pending?: boolean;
}) {
  const styles = {
    primary:
      "bg-primary text-primary-foreground hover:bg-primary/90 disabled:bg-secondary disabled:text-muted-foreground",
    ghost:
      "border border-border bg-secondary/40 text-foreground hover:bg-secondary disabled:text-muted-foreground",
  }[variant];
  return (
    <button
      {...rest}
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-2xl px-4 py-2 text-sm font-medium transition-[background-color,border-radius,color,scale] duration-200 hover:rounded-3xl enabled:active:scale-[0.98] disabled:cursor-not-allowed",
        styles,
        className,
      )}
      disabled={rest.disabled || pending}
      aria-busy={pending || undefined}
    >
      {pending && (
        <span className="size-3.5 shrink-0 animate-spin rounded-full border-2 border-current/30 border-t-current" />
      )}
      {children}
    </button>
  );
}

export function Panel({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "overflow-hidden rounded-3xl border border-border bg-card",
        className,
      )}
    >
      {children}
    </div>
  );
}

export const Divider = () => <div className="h-px bg-border" />;

export function Label({ children }: { children: React.ReactNode }) {
  return <span className="text-sm text-muted-foreground">{children}</span>;
}

export function Tooltip({
  label,
  children,
  className,
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span className={cn("group relative inline-flex", className)} title={label}>
      {children}
      <span
        role="tooltip"
        className="pointer-events-none absolute bottom-full left-1/2 z-40 mb-2 hidden -translate-x-1/2 rounded-md border border-border bg-popover px-2 py-1 text-[11px] whitespace-nowrap text-foreground shadow-xl group-hover:block group-focus-within:block"
      >
        {label}
      </span>
    </span>
  );
}

const TONE = {
  good: "border-positive/30 bg-positive/10 text-positive",
  warn: "border-warning/30 bg-warning/10 text-warning",
  bad: "border-destructive/30 bg-destructive/10 text-destructive",
  muted: "border-border bg-secondary/60 text-muted-foreground",
};

const STATUS_TONE: Record<string, keyof typeof TONE> = {
  confirmed: "good",
  submitted: "warn",
  pending: "warn",
  failed: "bad",
  reverted: "bad",
  open: "good",
  opening: "warn",
  closing: "warn",
  closed: "muted",
  cancelled: "muted",
  rejected: "muted",
};

export function StatusPill({ status }: { status: string }) {
  return (
    <span className={cn("rounded-md border px-2 py-0.5 text-[11px]", TONE[STATUS_TONE[status] ?? "muted"])}>
      {status}
    </span>
  );
}

export function Card({ title, children, action, className }: { title: string; children: React.ReactNode; action?: React.ReactNode; className?: string }) {
  return (
    <section className={cn("rounded-3xl border border-border bg-card p-6", className)}>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h2 className="text-2xl font-medium">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}

export function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-12 items-center justify-between gap-4 border-t border-border py-2 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right">{children}</dd>
    </div>
  );
}

export function Stat({ label, value, note }: { label: string; value: React.ReactNode | null; note?: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="tnum mt-1 truncate text-xl font-medium">{value ?? <Skeleton className="h-7 w-20" />}</dd>
      {note && <dd className="tnum mt-0.5 truncate text-xs text-muted-foreground">{note}</dd>}
    </div>
  );
}

export function Notice({ tone = "warn", children }: { tone?: keyof typeof TONE; children: React.ReactNode }) {
  return <p className={cn("rounded-xl border px-3 py-2 text-xs leading-5", TONE[tone])}>{children}</p>;
}

// Tabs styled like the side-panel switcher
export function PanelTabs<T extends string>({ id, value, onChange, options }: { id: string; value: T; onChange: (v: T) => void; options: { value: T; label: string }[] }) {
  return (
    <div role="tablist" className="grid grid-flow-col gap-1 rounded-full border border-border bg-secondary/60 p-1">
      {options.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "relative truncate rounded-full px-3 py-2 text-[15px] font-medium transition-colors",
            value === o.value ? "text-foreground" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {value === o.value && <TabPill id={`panel-${id}`} className="rounded-full bg-background" />}
          <span className="relative">{o.label}</span>
        </button>
      ))}
    </div>
  );
}
