export const formatUsd = (n: number) =>
  n.toLocaleString("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: n !== 0 && Math.abs(n) < 1 ? 4 : 2,
  });

export const formatUsdCompact = (n: number) =>
  n.toLocaleString("en-US", {
    style: "currency",
    currency: "USD",
    notation: "compact",
    maximumFractionDigits: 1,
  });

export const formatPct = (n: number, digits = 2) => `${n.toFixed(digits)}%`;

// -0.001 must not print as "-0.00%": round before deciding the sign.
export const formatPctSigned = (n: number, digits = 2) => {
  const r = Number(n.toFixed(digits));
  return `${r > 0 ? "+" : ""}${r.toFixed(digits)}%`;
};

export const formatSize = (n: number, digits = 4) =>
  n.toLocaleString("en-US", { maximumFractionDigits: digits });

export const truncateAddress = (a: string) =>
  a.length > 14 ? `${a.slice(0, 8)}…${a.slice(-6)}` : a;
