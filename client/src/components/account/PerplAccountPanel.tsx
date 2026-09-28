"use client";

import { useState } from "react";
import { AmountInput } from "@/components/ticket/AmountInput";
import { SummaryRows } from "@/components/ticket/SummaryRows";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { ErrorState } from "@/components/ui/ErrorState";
import { Pill } from "@/components/ui/Pill";
import { Segmented } from "@/components/ui/Segmented";
import { Skeleton } from "@/components/ui/Skeleton";
import { errorMessage, fromWad, toUnits } from "@/lib/api";
import { formatSize } from "@/lib/format";
import { usePerplAccount, usePerplDeposit, usePerplWithdraw } from "@/lib/query";
import { useSession } from "@/lib/session";
import { isZero } from "@/lib/units";
import { SectionTitle } from "@/components/ui/SectionTitle";

const MODES = [
  { value: "deposit", label: "Deposit" },
  { value: "withdraw", label: "Withdraw" },
] as const;

// Perpl margins every position from collateral held on its Exchange. The wallet
// is the account: the first deposit opens it, and there is no signer to grant.
export function PerplAccountPanel() {
  const { authenticated, login } = useSession();
  const account = usePerplAccount();
  const deposit = usePerplDeposit();
  const withdraw = usePerplWithdraw();
  const [mode, setMode] = useState<"deposit" | "withdraw">("deposit");
  const [amount, setAmount] = useState("");

  if (!authenticated) {
    return (
      <Card className="space-y-3 p-4">
        <Header />
        <p className="text-xs text-muted-foreground">
          Sign in to see your Perpl collateral and positions.
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
        <Skeleton className="h-24 rounded-xl" />
      </Card>
    );
  }
  if (account.isError) {
    return (
      <Card className="space-y-3 p-4">
        <Header />
        <ErrorState message={errorMessage(account.error)} />
      </Card>
    );
  }

  const a = account.data;
  const dec = a.collateral.decimals;
  const sym = a.collateral.symbol;
  const show = (raw?: string) => (raw ? `${formatSize(fromWad(raw, dec), 2)} ${sym}` : null);
  const free = a.registered ? (BigInt(a.balance ?? "0") - BigInt(a.locked ?? "0")).toString() : "0";
  const atoms = amount ? toUnits(amount, dec) : "";
  const min = mode === "withdraw" ? a.min_withdraw : a.registered ? a.min_deposit : a.min_open;
  const problem = !atoms
    ? null
    : isZero(atoms)
      ? `That rounds to zero at ${sym}'s ${dec} decimals.`
      : BigInt(atoms) < BigInt(min)
        ? `${mode === "withdraw" ? "Withdrawals" : a.registered ? "Deposits" : "Opening an account"} need at least ${show(min)}.`
        : mode === "withdraw" && BigInt(atoms) > BigInt(free)
          ? `Only ${show(free)} is free; the rest backs open positions or orders.`
          : null;
  const act = mode === "deposit" ? deposit : withdraw;

  const submit = async () => {
    if (!atoms || problem) return;
    try {
      await act.mutateAsync(atoms);
      setAmount("");
    } catch {
      // rendered below
    }
  };

  return (
    <Card className="space-y-4 p-4">
      <Header>
        <Pill tone={a.registered ? (a.frozen ? "destructive" : "positive") : "warning"}>
          {a.registered ? (a.frozen ? "frozen" : "open") : "not opened"}
        </Pill>
      </Header>
      <p className="text-xs text-muted-foreground">
        {a.registered
          ? "Perpl margins your positions from this collateral. Your own wallet signs every order, so there is nothing to authorize."
          : `Perpl holds perp collateral on its exchange. Your first deposit of at least ${show(a.min_open)} opens the account. Every order also pays MON gas.`}
      </p>

      {a.registered && (
        <SummaryRows
          rows={[
            { label: "Balance", value: show(a.balance) },
            { label: "Locked", value: show(a.locked) },
            { label: "Free", value: show(free) },
          ]}
        />
      )}

      <Segmented
        id="perpl-funds"
        value={mode}
        onChange={setMode}
        options={a.registered ? MODES : MODES.slice(0, 1)}
        className="grid-cols-2"
      />
      <AmountInput label={mode === "deposit" ? "Deposit" : "Withdraw"} value={amount} onChange={setAmount} suffix={sym} />
      <Button className="w-full" onClick={submit} pending={act.isPending} disabled={!atoms || !!problem}>
        {mode === "deposit" ? (a.registered ? "Deposit" : "Open account") : "Withdraw"}
      </Button>
      {problem && <ErrorState message={problem} />}
      {act.error && <ErrorState message={errorMessage(act.error)} />}
      {act.data?.state === "submitted" && (
        <p className="text-xs text-muted-foreground">
          Sent; the receipt has not arrived yet, so the balance may lag.
        </p>
      )}
    </Card>
  );
}

const Header = ({ children }: { children?: React.ReactNode }) => (
  <div className="flex items-center justify-between gap-3">
    <SectionTitle>Perpl account</SectionTitle>
    {children}
  </div>
);
