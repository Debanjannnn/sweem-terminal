"use client";

import { cn } from "@/lib/cn";

export function Button({
  children,
  variant = "primary",
  pending = false,
  className,
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
      disabled={rest.disabled || pending}
      aria-busy={pending || undefined}
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-2xl px-4 py-2 text-sm font-medium transition-[background-color,border-radius,color] duration-200 hover:rounded-3xl disabled:cursor-not-allowed",
        styles,
        className,
      )}
    >
      {pending && (
        <span className="size-3.5 shrink-0 animate-spin rounded-full border-2 border-current/30 border-t-current" />
      )}
      {children}
    </button>
  );
}
