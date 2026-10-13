"use client";

import { useState } from "react";
import { formatUnits, parseAbi } from "viem";
import { depositMargin, errText, MARGIN_TOKENS, withdrawMargin, type MarginToken } from "@/lib/api";
import { DEPLOYMENTS, publicClient } from "@/lib/config";
import { useHoldings } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, parseAmount, tokenBySymbol } from "@/lib/tokens";
import { Button, Notice, Panel } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { ExecutionProgress } from "@/components/Execution";
import { DelegationCard } from "@/components/automation/Delegation";
import { balanceOf } from "@/components/trade/MarketPanel";
import { Segmented } from "@/components/motion";
import { cn } from "@/lib/utils";

const ABI = parseAbi(["function getBalance(address user, address token) view returns (uint256)"]);
const MA = DEPLOYMENTS.kuru.marginAccount as `0x${string}`;
// Kuru keeps native MON under address(0), same as deployments.json
const addrOf = (t: MarginToken) => DEPLOYMENTS.tokens[t].address as `0x${string}`;
const dec = (t: MarginToken) => tokenBySymbol(t)!.decimals;
// MON deposit leaves this much in the wallet for gas
const GAS_RESERVE = BigInt(1e17);

const readMargin = (user: `0x${string}`) =>
  publicClient.multicall({
    allowFailure: false,
    contracts: MARGIN_TOKENS.map((t) => ({ address: MA, abi: ABI, functionName: "getBalance", args: [user, addrOf(t)] }) as const),
  });

// Kuru margin account (funds limit orders); deposit/withdraw run by the delegated signer.
export function TradingBalance() {
  const { address, delegated } = useSession();
  const holdings = useHoldings();
  const bal = useAsync(address ? `margin:${address}` : null, () => readMargin(address!));
  const [side, setSide] = useState<"deposit" | "withdraw">("deposit");
  const [token, setToken] = useState<MarginToken>("USDC");
  const [amount, setAmount] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [execId, setExecId] = useState<string | null>(null);

  const raw = parseAmount(amount, dec(token));
  const wallet = balanceOf(holdings.data, token);
  const max =
    side === "withdraw"
      ? (bal.data?.[MARGIN_TOKENS.indexOf(token)] ?? null)
      : token === "MON" && wallet !== null
        ? (wallet > GAS_RESERVE ? wallet - GAS_RESERVE : BigInt(0))
        : wallet;
  const reason = !delegated
    ? "Enable delegation first"
    : amount.trim() === ""
      ? "Enter an amount"
      : raw === null || raw === BigInt(0)
        ? "Invalid amount"
        : max !== null && raw > max
          ? `Insufficient ${token}`
          : null;

  async function submit() {
    setBusy(true);
    setErr(null);
    setExecId(null);
    try {
      setExecId((await (side === "deposit" ? depositMargin : withdrawMargin)(token, amount.trim())).executionId);
      setAmount("");
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel className="space-y-4 p-5">
      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-5">
        {MARGIN_TOKENS.map((t, i) => (
          <li key={t}>
            <button
              type="button"
              onClick={() => setToken(t)}
              className={cn("flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left transition-colors", t === token ? "bg-secondary" : "hover:bg-secondary/50")}
            >
              <TokenIcon symbol={t} size={22} />
              <span className="min-w-0">
                <span className="block text-xs text-muted-foreground">{t}</span>
                <span className="tnum block truncate text-sm">
                  {bal.data ? fmtUnits(bal.data[i], dec(t)) : bal.error ? "-" : "…"}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      {bal.error && <Notice tone="bad">Trading balance unavailable: {errText(new Error(bal.error))}</Notice>}
      <div className="flex items-center gap-3">
        <Segmented
          id="margin-side"
          size="sm"
          value={side}
          onChange={setSide}
          options={[
            { value: "deposit", label: "Deposit" },
            { value: "withdraw", label: "Withdraw" },
          ]}
        />
        {max !== null && (
          <button type="button" onClick={() => setAmount(formatUnits(max, dec(token)))} className="tnum ml-auto text-xs text-muted-foreground hover:text-foreground">
            {side === "deposit" ? "Wallet" : "Available"} {fmtUnits(max, dec(token))} {token}
          </button>
        )}
      </div>
      <div className="flex gap-2">
        <input
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          inputMode="decimal"
          aria-label={`Amount in ${token}`}
          placeholder={`0 ${token}`}
          className="tnum w-full min-w-0 rounded-xl bg-secondary/50 px-3 py-2 text-sm outline-none placeholder:text-muted-foreground/50"
        />
        <Button pending={busy} disabled={busy || reason !== null} onClick={() => void submit()}>
          {busy ? "Submitting…" : (reason ?? `${side === "deposit" ? "Deposit" : "Withdraw"} ${token}`)}
        </Button>
      </div>
      {!delegated && <DelegationCard compact />}
      {err && <Notice tone="bad">{err}</Notice>}
      {execId && (
        <ExecutionProgress
          id={execId}
          onSettled={() => {
            bal.reload();
            holdings.reload();
          }}
        />
      )}
      <p className="text-[11px] leading-4 text-muted-foreground">Kuru margin account balance: funds your limit orders. Moves between your wallet and Kuru only.</p>
    </Panel>
  );
}
