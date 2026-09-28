"use client";

import Link from "next/link";
import { ArrowRight, BookOpen, Bot, Layers, Repeat, ShieldAlert, Zap } from "lucide-react";
import { getBaskets } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { ErrorBox } from "@/components/ui";
import { Reveal } from "@/components/motion";
import { Faq } from "@/components/Faq";
import { TokenIcon } from "@/components/TokenIcon";
import { FeaturedCarousel } from "@/components/invest/FeaturedCarousel";
import { useBasketExtras, useMarkets } from "@/components/invest/extras";

const STEPS = [
  { icon: Layers, title: "Pick a basket", body: "Browse baskets people built from Kuru markets, or compose your own with fixed weights." },
  { icon: Zap, title: "Buy with USDC", body: "One transaction splits your USDC across every leg on Kuru order books. You get basket shares back." },
  { icon: Repeat, title: "Automate it", body: "Set recurring buys or a stop-loss on NAV. A scoped Privy signer runs them while you're away." },
];

const PILLARS = [
  { icon: Zap, title: "Monad", body: "Sub-second blocks make a multi-leg fill feel like a single swap." },
  { icon: BookOpen, title: "Kuru order books", body: "Every leg crosses a real on-chain book, with a min-out guarding slippage." },
  { icon: Bot, title: "Privy automation", body: "Delegate a signer that can only mint or redeem baskets. Revoke any time." },
];

export function Landing() {
  const baskets = useAsync("baskets", () => getBaskets());
  const markets = useMarkets();
  const extras = useBasketExtras(baskets.data);

  return (
    <div className="w-full space-y-16 px-4 pt-10 pb-24 sm:space-y-20">
      <Reveal>
        <section className="relative overflow-hidden rounded-2xl border border-border bg-gradient-to-b from-card to-background px-6 py-14 sm:px-12 sm:py-20">
          <div
            aria-hidden
            className="pointer-events-none absolute -top-32 -right-24 size-96 rounded-full bg-primary/10 blur-3xl"
          />
          <span className="inline-flex items-center gap-2 rounded-full border border-border bg-card px-3 py-1 text-xs text-muted-foreground">
            <span className="size-1.5 rounded-full bg-primary" />
            Live on Monad testnet
          </span>
          <h1 className="mt-6 max-w-2xl text-4xl font-bold tracking-tight sm:text-6xl">
            make your money <span className="text-primary">sweem</span>
          </h1>
          <p className="mt-5 max-w-xl text-base text-muted-foreground sm:text-lg">
            Tradable token baskets on Monad. Buy a whole portfolio in one transaction, filled on Kuru order books, and
            let Privy automation handle recurring buys and stop-losses.
          </p>
          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link
              href="/invest"
              className="inline-flex items-center gap-2 rounded-2xl bg-primary px-6 py-3 text-sm font-medium text-primary-foreground transition-[background-color,border-radius] duration-200 hover:rounded-3xl hover:bg-primary/90"
            >
              Explore baskets <ArrowRight className="size-4" />
            </Link>
            <Link
              href="/create"
              className="inline-flex items-center rounded-2xl border border-border bg-secondary/40 px-6 py-3 text-sm font-medium transition-[background-color,border-radius] duration-200 hover:rounded-3xl hover:bg-secondary"
            >
              Create a basket
            </Link>
          </div>
          <div className="mt-10 flex items-center gap-3 text-xs text-muted-foreground">
            <span className="flex -space-x-2">
              {["WETH", "cbBTC", "XAUT", "MON", "USDC"].map((s) => (
                <TokenIcon key={s} symbol={s} size={28} className="ring-2 ring-card" />
              ))}
            </span>
            WETH, cbBTC, gold and MON, priced in USDC
          </div>
        </section>
      </Reveal>

      <section className="space-y-6">
        <h2 className="text-2xl font-bold tracking-tight">How it works</h2>
        <div className="grid gap-3 md:grid-cols-3">
          {STEPS.map(({ icon: Icon, title, body }, i) => (
            <Reveal key={title} delay={0.05 * i} className="rounded-2xl border border-border bg-card p-6">
              <div className="flex items-center justify-between">
                <span className="grid size-10 place-items-center rounded-xl bg-primary text-primary-foreground">
                  <Icon className="size-5" />
                </span>
                <span className="tnum text-sm text-muted-foreground">0{i + 1}</span>
              </div>
              <h3 className="mt-6 font-semibold">{title}</h3>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{body}</p>
            </Reveal>
          ))}
        </div>
      </section>

      <section className="space-y-4">
        {baskets.error ? (
          <ErrorBox message={baskets.error} />
        ) : (
          <FeaturedCarousel
            title="Live baskets"
            baskets={baskets.data ?? []}
            markets={markets.data}
            extras={extras}
            loading={baskets.loading}
          />
        )}
        {baskets.data?.length === 0 && (
          <p className="text-sm text-muted-foreground">No baskets yet. Be the first to create one.</p>
        )}
      </section>

      <section className="grid gap-3 md:grid-cols-3">
        {PILLARS.map(({ icon: Icon, title, body }) => (
          <div key={title} className="rounded-2xl border border-border bg-card/60 p-6">
            <Icon className="size-5 text-primary" />
            <h3 className="mt-4 font-semibold">{title}</h3>
            <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{body}</p>
          </div>
        ))}
      </section>

      <div className="mx-auto max-w-2xl">
        <Faq />
      </div>

      <section className="flex flex-col items-start justify-between gap-6 rounded-2xl border border-border bg-card p-8 sm:flex-row sm:items-center sm:p-10">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">Ready to sweem?</h2>
          <p className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
            <ShieldAlert className="size-4 shrink-0" />
            Testnet only. Grab USDC from the faucet in the wallet menu.
          </p>
        </div>
        <Link
          href="/invest"
          className="inline-flex shrink-0 items-center gap-2 rounded-2xl bg-primary px-6 py-3 text-sm font-medium text-primary-foreground transition-[background-color,border-radius] duration-200 hover:rounded-3xl hover:bg-primary/90"
        >
          Start investing <ArrowRight className="size-4" />
        </Link>
      </section>
    </div>
  );
}
