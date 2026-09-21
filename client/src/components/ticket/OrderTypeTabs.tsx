"use client";

import { Segmented } from "@/components/ui/Segmented";

export type OrderType = "market" | "limit" | "stop";

const TYPES = [
  { value: "market", label: "Market" },
  { value: "limit", label: "Limit" },
  { value: "stop", label: "Stop" },
] as const;

export const OrderTypeTabs = ({
  value,
  onChange,
}: {
  value: OrderType;
  onChange: (v: OrderType) => void;
}) => (
  <Segmented
    id="order-type"
    value={value}
    onChange={onChange}
    options={TYPES}
    tone="accent"
    className="grid-cols-3"
  />
);
