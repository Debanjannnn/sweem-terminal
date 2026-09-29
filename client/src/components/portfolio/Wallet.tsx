"use client";

import { useEffect, useState } from "react";
import { parseUnits, type Address } from "viem";
import { readBalance, readNextClaimAt, useTx } from "@/lib/tx";
import { useAsync } from "@/lib/session";
import { TOKENS, type Token } from "@/lib/tokens";
import { Button, Panel, Skeleton } from "@/components/ui";

export const LOW_GAS = parseUnits("0.1", 18);
const WALLET_TOKENS = Object.values(TOKENS);

export function useBalances(address: Address | undefined) {
  return useAsync(address ? `balances:${address}` : null, () =>
    Promise.all(WALLET_TOKENS.map(async (t) => [t, await readBalance(t.address, address!)] as [Token, bigint])),
  );
}

export function useNow() {
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
