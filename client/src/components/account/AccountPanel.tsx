"use client";

import { useState } from "react";
import { AmountInput } from "@/components/ticket/AmountInput";
import { SummaryRows } from "@/components/ticket/SummaryRows";
import { TxState } from "@/components/table/TxState";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { ErrorState } from "@/components/ui/ErrorState";
import { Pill } from "@/components/ui/Pill";
import { Segmented } from "@/components/ui/Segmented";
import { Skeleton } from "@/components/ui/Skeleton";
import { errorMessage, fromWad, type Account, type Market } from "@/lib/api";
import { formatSize } from "@/lib/format";
import { useAccount, useAuthorize, useDeposit, useRevokeAuthorization } from "@/lib/query";
import { useSession } from "@/lib/session";
import { depositAmount, isZero } from "@/lib/units";
import { SectionTitle } from "@/components/ui/SectionTitle";

const EXPIRY = [
  { value: "24", label: "24h" },
  { value: "168", label: "7d" },
  { value: "720", label: "30d" },
] as const;

type Stage = "unfunded" | "funded" | "delegated" | "ready";

// with no backend signer the owner's own wallet trades, so funded is ready
const stage = (a: Account): Stage =>
  !a.registered ? "unfunded" : !a.signer ? "ready" : a.signer_authorized ? "delegated" : "funded";

const STAGE = {
  unfunded: {
    tone: "warning",
    label: "not funded",
    line: "Kuru holds every trader's funds in its AccountCore contract, so nothing can be ordered against a wallet balance. Depositing is what creates the account.",
  },
  funded: {
    tone: "neutral",
    label: "funded",
    line: "This account holds funds inside Kuru, so orders can be placed. Automated strategies need a delegated signer as well.",
  },
  delegated: {
    tone: "positive",
    label: "funded · delegated",
    line: "This account holds funds inside Kuru and our signer may trade it.",
  },
  ready: {
    tone: "positive",
    label: "funded",
    line: "This account holds funds inside Kuru. Your own wallet signs its orders, so there is no separate signer to authorize.",
  },
} as const;

export function AccountPanel({ market }: { market?: Market | null }) {
  const { authenticated, login } = useSession();
  const account = useAccount();
  const deposit = useDeposit();
  const authorize = useAuthorize();
  const revoke = useRevokeAuthorization();

  const [token, setToken] = useState<"base" | "quote">("quote");
  const [amount, setAmount] = useState("");
  const [hours, setHours] = useState<(typeof EXPIRY)[number]["value"]>("168");

  const symbol = market ? (token === "base" ? market.base : market.quote) : "";
  const decimals = market ? (token === "base" ? market.baseDecimals : market.quoteDecimals) : 0;
  const atoms = market && amount ? depositAmount(market, token, amount) : "";
  const fundable = !!market && atoms !== "" && !isZero(atoms);
  const expiry = Math.floor(Date.now() / 1000) + Number(hours) * 3_600;

  const send = async () => {
    if (!market || !fundable) return;
    try {
      await deposit.mutateAsync({ market_id: market.id, token, amount: atoms });
      setAmount("");
    } catch {
      // the message renders under the button; a toast would hide the 412 sentence
    }
  };

  if (!authenticated) {
    return (
      <Card className="space-y-3 p-4">
        <Header />
        <p className="text-xs text-muted-foreground">
          Sign in to see whether this wallet has a funded Kuru trading account.
        </p>
        <Button variant="ghost" className="w-full" onClick={login}>
          Sign in
        </Button>
      </Card>
    );
  }

  if (account.isPending) {
    return (
      <Card className="space-y-3 p-4">
        <Header />
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-16 rounded-xl" />
      </Card>
    );
  }

  if (account.isError || !account.data) {
    return (
      <Card className="space-y-3 p-4">
        <Header />
        <ErrorState
          message={
            account.error
              ? errorMessage(account.error)
              : "The wallet service answered without an account, so its state is unknown."
          }
        />
      </Card>
    );
  }

  const a = account.data;
  const s = STAGE[stage(a)];

  return (
    <Card className="space-y-4 p-4">
      <Header>
        <Pill tone={s.tone}>{s.label}</Pill>
      </Header>

      <p className="text-xs text-muted-foreground">{s.line}</p>

      <SummaryRows
        rows={[
          { label: "Account id", value: a.registered ? String(a.user_id) : null },
          { label: "Authorization epoch", value: a.auth_nonce },
        ]}
      />

      <div className="h-px bg-border" />

      <div className="space-y-3">
        <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Deposit</div>
        <Segmented
          id="deposit-token"
          value={token}
          onChange={setToken}
          options={[
            { value: "quote", label: market?.quote ?? "quote" },
            { value: "base", label: market?.base ?? "base" },
          ]}
          className="grid-cols-2"
        />
        <AmountInput
          label="Amount"
          value={amount}
          onChange={setAmount}
          suffix={symbol || "amount"}
        />
        {fundable && (
          <p className="text-xs text-muted-foreground">
            This moves {amount} {symbol} out of your wallet into Kuru&apos;s AccountCore
            contract at {a.account_core}. It stays yours and you can withdraw it later.
          </p>
        )}
        <Button
          className="w-full"
          onClick={send}
          pending={deposit.isPending}
          disabled={!fundable}
        >
          {a.registered ? "Deposit" : "Fund trading account"}
        </Button>
        {deposit.data && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <TxState tier="seen" />
              <span className="text-xs text-muted-foreground">
                {deposit.data.state === "submitted"
                  ? "The deposit was sent but no receipt arrived, so the balance below is not known yet."
                  : "Mined at Monad's latest view, which is proposed and can still change."}
              </span>
            </div>
            <SummaryRows
              rows={[
                {
                  label: "Free",
                  value: deposit.data.free
                    ? `${formatSize(fromWad(deposit.data.free, decimals))} ${symbol}`
                    : null,
                },
                {
                  label: "Reserved by resting orders",
                  value: deposit.data.reserved
                    ? `${formatSize(fromWad(deposit.data.reserved, decimals))} ${symbol}`
                    : null,
                },
              ]}
            />
          </div>
        )}
        {deposit.error && <ErrorState message={errorMessage(deposit.error)} />}
      </div>

      {a.signer && (
        <>
          <div className="h-px bg-border" />

          <div className="space-y-3">
            <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
              Delegated trading
            </div>
            <p className="text-xs text-muted-foreground">
              Our backend signer gets TRADE only: it can place and cancel orders for this
              account, and it cannot withdraw from it. Revoking advances the authorization
              epoch, which invalidates every pending automated order.
            </p>
            {a.signer_authorized ? (
              <>
                <SummaryRows
                  rows={[
                    {
                      label: "Expires",
                      value: a.signer_expiry
                        ? new Date(a.signer_expiry * 1000).toLocaleString()
                        : null,
                    },
                  ]}
                />
                {!a.signer_expiry && (
                  <p className="text-xs text-muted-foreground">
                    The contract emits no event for a signer expiry, so this account does not
                    report when the grant ends. Revoke to end it now.
                  </p>
                )}
                <Button
                  variant="ghost"
                  className="w-full"
                  onClick={() => revoke.mutate()}
                  pending={revoke.isPending}
                >
                  Revoke trading authorization
                </Button>
              </>
            ) : (
              <>
                <Segmented
                  id="authorize-expiry"
                  value={hours}
                  onChange={setHours}
                  options={EXPIRY}
                  className="grid-cols-3"
                />
                <p className="text-xs text-muted-foreground">
                  Expires {new Date(expiry * 1000).toLocaleString()}.
                </p>
                <Button
                  className="w-full"
                  onClick={() => authorize.mutate(expiry)}
                  pending={authorize.isPending}
                  disabled={!a.registered}
                >
                  Authorize trading
                </Button>
              </>
            )}
            {(authorize.error || revoke.error) && (
              <ErrorState message={errorMessage(authorize.error ?? revoke.error)} />
            )}
          </div>
        </>
      )}
    </Card>
  );
}

const Header = ({ children }: { children?: React.ReactNode }) => (
  <div className="flex items-center justify-between gap-3">
    <SectionTitle>Kuru account</SectionTitle>
    {children}
  </div>
);
