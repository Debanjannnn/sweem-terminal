"use client";

export function AmountInput({
  label,
  value,
  onChange,
  suffix,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  suffix: string;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="flex items-center gap-2 rounded-xl border border-input bg-secondary/40 px-3 py-2.5 transition-colors focus-within:border-ring/40">
        <input
          inputMode="decimal"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="0.00"
          className="tnum w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        />
        <span className="shrink-0 text-xs text-muted-foreground">{suffix}</span>
      </span>
    </label>
  );
}
