"use client";

import { useEffect, useState } from "react";
import { ExternalLink } from "lucide-react";
import { parseUnits, type Address } from "viem";
import { readBalance, readNextClaimAt, useTx } from "@/lib/tx";
import { useAsync } from "@/lib/session";
import { TOKENS, fmtUnits, type Token } from "@/lib/tokens";
import { Button, Panel, Skeleton } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";

const LOW_GAS = parseUnits("0.1", 18);
const WALLET_TOKENS = Object.values(TOKENS);

export function useBalances(address: Address | undefined) {
  return useAsync(address ? `balances:${address}` : null, () =>
    Promise.all(WALLET_TOKENS.map(async (t) => [t, await readBalance(t.address, address!)] as [Token, bigint])),
  );
}

export function Balances({ balances }: { balances: ReturnType<typeof useBalances> }) {
  const mon = balances.data?.find(([t]) => t.symbol === "MON")?.[1];
  return (
    <Panel>
      <div className="px-5 pt-4 text-sm font-medium">Wallet</div>
      <ul className="grid grid-cols-2 gap-4 p-5 sm:grid-cols-5">
        {WALLET_TOKENS.map((t) => {
          const bal = balances.data?.find(([x]) => x.address === t.address)?.[1];
          return (
            <li key={t.address} className="flex items-center gap-2">
              <TokenIcon symbol={t.symbol} size={22} />
              <div className="min-w-0">
                <div className="text-xs text-muted-foreground">{t.symbol}</div>
                {bal === undefined ? (
                  <Skeleton className="mt-1 h-4 w-14" />
                ) : (
                  <div className="tnum truncate text-sm">{fmtUnits(bal, t.decimals, 4)}</div>
                )}
              </div>
            </li>
          );
        })}
      </ul>
      {mon !== undefined && mon < LOW_GAS && (
        <div className="mx-5 mb-5 flex flex-wrap items-center gap-3 rounded-xl border border-warning/30 bg-warning/10 p-3 text-xs text-warning">
          <span>Low MON for gas. Every buy, sell and claim needs a little MON.</span>
          <a
            href="https://faucet.monad.xyz"
            target="_blank"
            rel="noreferrer"
            className="ml-auto inline-flex items-center gap-1 underline underline-offset-4 hover:no-underline"
          >
            Get MON <ExternalLink className="size-3" />
          </a>
        </div>
      )}
    </Panel>
  );
}

function useNow() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return now;
}

const hms = (ms: number) => {
  const s = Math.ceil(ms / 1000);
  return [Math.floor(s / 3600), Math.floor((s % 3600) / 60), s % 60].map((n) => String(n).padStart(2, "0")).join(":");
};

export function FaucetCard({ address, onClaimed }: { address: Address; onClaimed: () => void }) {
  const { claimFaucet } = useTx();
  const next = useAsync(`faucet:${address}`, () => readNextClaimAt(address));
  const now = useNow();
  const [pending, setPending] = useState(false);
  const wait = next.data ? next.data.getTime() - now : 0;

  async function claim() {
    setPending(true);
    try {
      await claimFaucet();
      next.reload();
      onClaimed();
    } catch {
      // toast already shown by useTx
    } finally {
      setPending(false);
    }
  }

  return (
    <Panel>
      <div className="flex flex-wrap items-center gap-3 p-5">
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">Test funds</div>
          <p className="mt-1 text-xs text-muted-foreground">10,000 USDC, 1 WETH, 0.1 cbBTC and 1 XAUT every 12 hours.</p>
        </div>
        {next.loading && !next.data ? (
          <Skeleton className="h-9 w-28 rounded-2xl" />
        ) : wait > 0 ? (
          <span className="tnum text-sm text-muted-foreground">next claim in {hms(wait)}</span>
        ) : (
          <Button pending={pending} onClick={() => void claim()}>
            Claim
          </Button>
        )}
      </div>
    </Panel>
  );
}
