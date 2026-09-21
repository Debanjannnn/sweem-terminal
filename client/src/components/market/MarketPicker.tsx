"use client";

import { Dropdown } from "@/components/ui/Dropdown";
import { formatPctSigned } from "@/lib/format";

export type MarketOption = { symbol: string; changePct?: number };

export function MarketPicker({
  value,
  markets = [],
  onChange,
}: {
  value: string | null;
  markets?: MarketOption[];
  onChange?: (symbol: string) => void;
}) {
  return (
    <Dropdown
      value={value}
      onChange={(v) => onChange?.(v)}
      options={markets.map((m) => ({
        value: m.symbol,
        label: m.symbol,
        hint: m.changePct === undefined ? undefined : formatPctSigned(m.changePct),
      }))}
      ariaLabel="Market"
      placeholder={value ?? "Select market"}
    />
  );
}
