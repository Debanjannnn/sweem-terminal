"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { ExecutionStatus } from "@/lib/types";

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
        "inline-flex items-center justify-center gap-2 rounded-2xl px-4 py-2 text-sm font-medium transition-[background-color,border-radius,color] duration-200 hover:rounded-3xl disabled:cursor-not-allowed",
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

const STATUS_TONE: Record<ExecutionStatus | "reverted", keyof typeof TONE> = {
  confirmed: "good",
  submitted: "warn",
  pending: "warn",
  failed: "bad",
  reverted: "bad",
  rejected: "muted",
};

export function StatusPill({ status }: { status: string }) {
  return (
    <span className={cn("rounded-md border px-2 py-0.5 text-[11px]", TONE[STATUS_TONE[status as ExecutionStatus] ?? "muted"])}>
      {status}
    </span>
  );
}
