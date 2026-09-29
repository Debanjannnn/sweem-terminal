"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { Check, Copy, Wallet as WalletIcon } from "lucide-react";
import { zeroAddress } from "viem";
import { errMsg, fmtTime, shortHash } from "@/lib/api";
import { addressUrl, USDC } from "@/lib/config";
import { useAsync, useSession } from "@/lib/session";
import { fmtUnits, tokenOf } from "@/lib/tokens";
import { readBalance, readNextClaimAt, useTx } from "@/lib/tx";
import { cn } from "@/lib/utils";
import { BasketSearch } from "./BasketSearch";
import { Spinner, useDismissable } from "./ui";

const NAV = [
  { label: "Explore", href: "/" },
  { label: "Portfolio", href: "/portfolio" },
  { label: "Create", href: "/create" },
] as const;

export function Navbar() {
  const pathname = usePathname();
  const { ready, authenticated, login } = useSession();

  return (
    <header className="sticky top-0 z-40 w-full border-b border-border bg-background/85 backdrop-blur">
      <div className="mx-auto grid w-full max-w-[1400px] grid-cols-[1fr_auto] items-center gap-4 px-6 py-3 md:grid-cols-[1fr_minmax(0,360px)_1fr]">
        <div className="flex items-center gap-8">
          <Link href="/" className="flex items-center gap-2 text-lg font-semibold tracking-tight">
            <Image src="/sweem-mark.png" alt="" width={26} height={26} priority unoptimized className="size-[26px]" />
            sweem
          </Link>
          <nav className="hidden items-center gap-6 sm:flex">
            {NAV.map((item) => {
              const active = item.href === "/" ? pathname === "/" || pathname.startsWith("/baskets") : pathname.startsWith(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    "text-sm font-medium transition-colors",
                    active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>

        <div className="hidden justify-center md:flex">
          <BasketSearch />
        </div>

        <div className="flex items-center justify-end gap-2">
          {!ready ? (
            <Spinner />
          ) : authenticated ? (
            <WalletMenu />
          ) : (
            <button
              onClick={login}
              className="rounded-xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              Sign up / Log in
            </button>
          )}
        </div>
      </div>
    </header>
  );
}

function WalletMenu() {
  const { address, logout, exportWallet } = useSession();
  const { open, setOpen, ref } = useDismissable();
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function copy() {
    if (!address) return;
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {}
  }

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-label="Wallet"
        className="tnum inline-flex items-center gap-2 rounded-xl bg-secondary px-3.5 py-2 text-sm font-medium transition-colors hover:bg-accent"
      >
        <WalletIcon className="size-4 text-muted-foreground" />
        {address ? shortHash(address) : "Connected"}
      </button>
      {open && (
        <div className="absolute right-0 top-full z-50 mt-2 w-80 max-w-[calc(100vw-2rem)] space-y-3 rounded-xl border border-border bg-popover p-4 text-sm shadow-xl">
          {!address ? (
            <Spinner label="Creating your wallet…" />
          ) : (
            <>
              <div className="flex items-center justify-between gap-3">
                <a
                  href={addressUrl(address)}
                  target="_blank"
                  rel="noreferrer"
                  title={address}
                  className="tnum underline-offset-4 hover:underline"
                >
                  {shortHash(address)}
                </a>
                <button
                  onClick={() => void copy()}
                  aria-label="Copy wallet address"
                  className="rounded-md p-1.5 text-muted-foreground transition-colors hover:text-foreground"
                >
                  {copied ? <Check className="size-4 text-positive" /> : <Copy className="size-4" />}
                </button>
              </div>
              <Wallet address={address} />
              {err && <p className="text-xs break-words text-destructive">{err}</p>}
              <div className="flex gap-2 pt-1">
                <MenuButton
                  onClick={() => {
                    setErr(null);
                    exportWallet().catch((e: unknown) => setErr(errMsg(e)));
                  }}
                >
                  Export wallet
                </MenuButton>
                <MenuButton onClick={() => void logout()}>Log out</MenuButton>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function Wallet({ address }: { address: `0x${string}` }) {
  const { claimFaucet } = useTx();
  const [claiming, setClaiming] = useState(false);
  const bal = useAsync(`wallet:${address}`, async () => {
    const [usdc, mon, next] = await Promise.all([
      readBalance(USDC, address),
      readBalance(zeroAddress, address),
      readNextClaimAt(address),
    ]);
    return { usdc, mon, next, claimable: next.getTime() <= Date.now() };
  });
  const { usdc, mon, next, claimable } = bal.data ?? {};

  return (
    <div className="space-y-2">
      <Row label="USDC" value={usdc === undefined ? "…" : fmtUnits(usdc, tokenOf(USDC).decimals, 2)} />
      <Row label="MON" value={mon === undefined ? "…" : fmtUnits(mon, 18)} />
      {bal.error && <p className="text-xs text-warning">{bal.error}</p>}
      <div className="flex items-center justify-between gap-3 pt-1">
        <span className="text-xs text-muted-foreground">
          {next === undefined ? "Faucet" : claimable ? "Faucet ready" : `Faucet at ${fmtTime(next.getTime())}`}
        </span>
        <MenuButton
          disabled={!claimable || claiming}
          onClick={() => {
            setClaiming(true);
            claimFaucet()
              .then(bal.reload, () => {})
              .finally(() => setClaiming(false));
          }}
        >
          {claiming ? "Claiming…" : "Claim test tokens"}
        </MenuButton>
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="tnum">{value}</span>
    </div>
  );
}

function MenuButton({
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...rest}
      className="rounded-lg border border-border bg-secondary/50 px-3 py-1.5 text-xs transition-colors hover:bg-secondary disabled:cursor-not-allowed disabled:text-muted-foreground"
    >
      {children}
    </button>
  );
}
