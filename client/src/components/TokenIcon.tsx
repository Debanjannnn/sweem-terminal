import { tokenBySymbol } from "@/lib/tokens";

export function TokenIcon({
  symbol,
  size = 24,
  className,
}: {
  symbol: string;
  size?: number;
  className?: string;
}) {
  const t = tokenBySymbol(symbol);

  if (t?.icon) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        src={t.icon}
        alt={symbol}
        width={size}
        height={size}
        className={`shrink-0 rounded-full ${className ?? ""}`}
        style={{ width: size, height: size }}
      />
    );
  }

  const initial = (symbol.replace(/^[a-z]+/, "") || symbol).charAt(0).toUpperCase();
  return (
    <span
      aria-label={symbol}
      role="img"
      title={symbol}
      className={`inline-grid shrink-0 place-items-center rounded-full font-semibold ${
        t?.color ? "text-white" : "bg-secondary"
      } ${className ?? ""}`}
      style={{
        width: size,
        height: size,
        fontSize: Math.round(size * 0.42),
        ...(t?.color ? { backgroundColor: t.color } : {}),
      }}
    >
      {initial || "?"}
    </span>
  );
}
