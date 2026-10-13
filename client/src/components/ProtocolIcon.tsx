import { cn } from "@/lib/utils";

// logos from DefiLlama's protocol registry, keyed by protocol (deployments erc4626 `protocol`, venue ids)
const MARK: Record<string, string> = {
  aave: "aave-v3.png",
  neverland: "neverland.png",
  euler: "euler-v2.png",
  morpho: "morpho-blue.png",
  shmonad: "shmonad.png",
  kuru: "kuru-clob.png",
  perpl: "perpl.png",
};

export function ProtocolIcon({ protocol, size = 20, className }: { protocol: string; size?: number; className?: string }) {
  const file = MARK[protocol];
  if (!file) return null;
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={`/protocols/${file}`}
      alt={protocol}
      width={size}
      height={size}
      style={{ width: size, height: size }}
      className={cn("shrink-0 rounded-full object-contain", className)}
    />
  );
}
