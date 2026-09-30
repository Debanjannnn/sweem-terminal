"use client";

import { useEffect, useState } from "react";
import { parseUnits } from "viem";

export const LOW_GAS = parseUnits("0.1", 18);

export function useNow() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return now;
}
