"use client";

import { Search } from "lucide-react";
import { cn } from "@/lib/cn";

export function SearchPill({
  value,
  onChange,
  className,
}: {
  value: string;
  onChange: (v: string) => void;
  className?: string;
}) {
  return (
    <label
      className={cn(
        "flex items-center gap-2 rounded-full bg-secondary/50 px-4 py-2 transition-colors focus-within:bg-secondary/80",
        className,
      )}
    >
      <Search className="size-4 shrink-0 text-muted-foreground" />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Search markets"
        aria-label="Search markets"
        className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
      />
    </label>
  );
}
