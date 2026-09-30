"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { Check, Copy, Menu, Wallet as WalletIcon } from "lucide-react";
import { errMsg, shortHash } from "@/lib/api";
import { addressUrl } from "@/lib/config";
import { useHoldings } from "@/lib/hooks";
import { useSession } from "@/lib/session";
import { cn } from "@/lib/utils";
import { BasketSearch } from "./BasketSearch";
import { Spinner, useDismissable } from "./ui";

const NAV = [
  { label: "Trade", href: "/" },
  { label: "Earn", href: "/earn" },
  { label: "Portfolio", href: "/portfolio" },
] as const;

export function Navbar() {
  const pathname = usePathname();
  const { ready, authenticated, login } = useSession();
  const { open: menuOpen, setOpen: setMenuOpen, ref: menuRef } = useDismissable();
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 4);
    on();
    window.addEventListener("scroll", on, { passive: true });
    return () => window.removeEventListener("scroll", on);
  }, []);
  const isActive = (href: string) =>
    href === "/" ? pathname === "/" || pathname.startsWith("/baskets") || pathname.startsWith("/trade") : pathname.startsWith(href);
  const links = (cls: string) =>
    NAV.map((item) => (
      <Link
        key={item.href}
        href={item.href}
        onClick={() => setMenuOpen(false)}
        className={cn(
          cls,
          "font-medium transition-colors duration-150",
          isActive(item.href) ? "text-foreground" : "text-muted-foreground hover:text-foreground",
        )}
      >
        {item.label}
      </Link>
    ));

  return (
    // transparent at the top of every page; solid + hairline once scrolled
    <header
      className={cn(
        "sticky top-0 z-40 w-full border-b transition-colors duration-200",
        scrolled ? "border-border bg-background/90 backdrop-blur-md" : "border-transparent bg-transparent",
      )}
    >
      <div className="grid h-16 w-full grid-cols-[1fr_auto_1fr] items-center gap-4 px-4 md:px-6">
        <Link href="/" className="flex items-center gap-2 justify-self-start text-2xl font-semibold tracking-tight">
          <Image src="/sweem-mark.png" alt="" width={26} height={26} priority unoptimized className="size-[26px]" />
          sweem
        </Link>

        <BasketSearch />

        <div className="flex items-center justify-end gap-2 sm:gap-6">
          <nav aria-label="Main navigation" className="hidden items-center gap-6 md:flex">
            {links("inline-flex h-10 items-center px-2 text-[15px]")}
          </nav>
          <div className="relative md:hidden" ref={menuRef}>
            <button
              onClick={() => setMenuOpen((o) => !o)}
              aria-expanded={menuOpen}
              aria-label="Menu"
              className="grid size-9 place-items-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
            >
              <Menu className="size-5" />
            </button>
            {menuOpen && (
              <nav className="absolute right-0 top-full z-50 mt-2 flex w-44 flex-col rounded-xl border border-border bg-popover p-1 shadow-xl">
                {links("rounded-lg px-3 py-2 text-sm hover:bg-secondary")}
              </nav>
            )}
          </div>
          {!ready ? (
            <Spinner />
          ) : authenticated ? (
            <WalletMenu />
          ) : (
            <button
              onClick={login}
              className="inline-flex h-9 items-center rounded-full bg-primary px-3.5 text-sm font-semibold whitespace-nowrap text-primary-foreground transition-[background-color,scale] duration-150 hover:bg-primary/90 active:scale-[0.96]"
            >
              <span className="sm:hidden">Log in</span>
              <span className="hidden sm:inline">Sign up / Log in</span>
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
        className="tnum inline-flex h-9 items-center gap-2 rounded-full bg-secondary px-3.5 text-sm font-medium transition-colors hover:bg-accent"
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
              <Wallet />
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

function Wallet() {
  const h = useHoldings();
  const amt = (sym: string) => {
    const t = h.data?.tokens.find((x) => x.symbol === sym);
    return t ? Number(t.amount).toLocaleString("en-US", { maximumFractionDigits: 4 }) : h.data ? "0" : "…";
  };
  return (
    <div className="space-y-2">
      <Row label="Network" value="Monad mainnet" />
      <Row label="USDC" value={amt("USDC")} />
      <Row label="MON (gas)" value={amt("MON")} />
      {h.error && <p className="text-xs text-warning">Balances unavailable: {h.error}</p>}
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
