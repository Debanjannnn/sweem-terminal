"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Check, Plus, X } from "lucide-react";
import { createBasket, errText, fmtBps, getBook } from "@/lib/api";
import { useAsync, useSession } from "@/lib/session";
import { MARKETS } from "@/lib/tokens";
import { Button, Divider, ErrorBox, Label, Panel, Tooltip } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";
import { IconStack } from "@/components/invest/BasketCard";
import { cn } from "@/lib/utils";
import { evenSplit, pctToBps, setWeight, weightsError } from "./weights";

export const COLORS = ["var(--primary)", "var(--chart-1)", "var(--chart-2)", "var(--warning)", "var(--chart-3)", "var(--chart-4)"];

type Pick = { market: string; bps: number };

// Basket = off-chain definition (api); buying it places one Kuru market order per leg from the user's wallet.
export function CreateBasket() {
  const router = useRouter();
  const { ready, authenticated, login } = useSession();
  // tradable = both book sides present; unknown (fetch failed) stays selectable
  const books = useAsync("create:books", async () => {
    const res = await Promise.allSettled(MARKETS.map((m) => getBook(m.orderbook)));
    return new Map(MARKETS.map((m, i) => {
      const r = res[i];
      return [m.symbol, r.status === "rejected" || (r.value.bids.length > 0 && r.value.asks.length > 0)];
    }));
  });

  const [picks, setPicks] = useState<Pick[]>([]);
  const [typing, setTyping] = useState<{ i: number; text: string } | null>(null);
  const [name, setName] = useState("");
  const [isPublic, setPublic] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sym = (m: string) => m;
  const resplit = (ms: string[]) => {
    const w = evenSplit(ms.length);
    setPicks(ms.map((market, i) => ({ market, bps: w[i] })));
  };
  const toggle = (m: string) =>
    resplit(picks.some((p) => p.market === m) ? picks.filter((p) => p.market !== m).map((p) => p.market) : [...picks.map((p) => p.market), m]);
  const setBps = (i: number, bps: number) => {
    const w = setWeight(picks.map((p) => p.bps), i, bps);
    setPicks(picks.map((p, j) => ({ ...p, bps: w[j] })));
  };

  const wErr = weightsError(picks.map((p) => p.bps));
  const sum = picks.reduce((s, p) => s + p.bps, 0);
  const nameErr = name.trim() === "" ? "Name your basket" : null;
  const invalid = wErr ?? nameErr;

  async function submit() {
    if (!authenticated) return login();
    if (invalid) return;
    setBusy(true);
    setError(null);
    try {
      const b = await createBasket({ name: name.trim(), legs: picks.map((p) => ({ market: p.market, weightBps: p.bps })), public: isPublic });
      router.push(`/baskets/${b.id}`);
    } catch (e) {
      setError(errText(e));
      setBusy(false);
    }
  }

  return (
    <main className="w-full px-4 pt-8 pb-24 md:px-6">
      <div className="mx-auto w-full space-y-6">
        <div>
          <h1 className="text-xl font-medium sm:text-2xl">Create a basket</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Pick Kuru markets and weights. Buying fills each leg on its Kuru book into your own wallet — no share token, no pooled funds.
          </p>
        </div>

        <div className="grid gap-6 lg:grid-cols-[1fr_340px] lg:items-start">
          <Panel>
            <div className="px-5 pt-5 sm:px-6 sm:pt-6">
              <Step n={1}>Name it</Step>
            </div>
            <div className="grid gap-5 p-5 sm:grid-cols-[1fr_auto] sm:p-6">
              <div>
                <Label>Name</Label>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value.slice(0, 40))}
                  placeholder="Blue Chips"
                  aria-label="Basket name"
                  disabled={busy}
                  className="mt-3 w-full bg-transparent text-2xl font-medium tracking-tight outline-none placeholder:text-muted-foreground/40"
                />
              </div>
              <label className="flex items-center gap-2 self-end pb-1 text-sm">
                <input type="checkbox" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} disabled={busy} className="accent-[var(--primary)]" />
                Public (listed on Explore)
              </label>
            </div>

            <Divider />

            <div className="p-5 sm:p-6">
              <Step n={2}>Pick markets</Step>
              <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
                {MARKETS.map((m) => {
                  const on = picks.some((p) => p.market === m.symbol);
                  const dead = books.data?.get(m.symbol) === false;
                  const tile = (
                    <button
                      type="button"
                      onClick={() => toggle(m.symbol)}
                      disabled={busy || (dead && !on)}
                      aria-pressed={on}
                      className={cn(
                        "flex w-full items-center gap-2.5 rounded-xl border p-3 text-left text-sm transition-colors disabled:cursor-not-allowed",
                        on ? "border-primary/60 bg-primary/10" : "border-border bg-secondary/30 hover:bg-secondary/60",
                        dead && "opacity-50",
                      )}
                    >
                      <TokenIcon symbol={m.symbol} size={24} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">{m.symbol}</span>
                        <span className="block text-xs text-muted-foreground">{dead ? "no liquidity" : m.token.name}</span>
                      </span>
                      {on ? <Check className="size-4 text-primary" /> : !dead && <Plus className="size-4 text-muted-foreground" />}
                    </button>
                  );
                  return dead ? (
                    <Tooltip key={m.symbol} label="No bids or asks on this Kuru book right now" className="w-full">
                      {tile}
                    </Tooltip>
                  ) : (
                    <div key={m.symbol}>{tile}</div>
                  );
                })}
              </div>
            </div>

            {picks.length > 0 && (
              <>
                <Divider />
                <div className="space-y-3 p-5 sm:p-6">
                  <div className="flex items-center justify-between">
                    <Step n={3}>Set weights</Step>
                    <div className="flex items-center gap-3 text-xs">
                      <span className={cn("tnum", sum === 10000 ? "text-muted-foreground" : "text-warning")}>
                        {fmtBps(sum)} / 100%
                      </span>
                      <button
                        type="button"
                        onClick={() => resplit(picks.map((p) => p.market))}
                        disabled={busy}
                        className="rounded-lg px-2.5 py-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                      >
                        Split evenly
                      </button>
                    </div>
                  </div>
                  {picks.map((p, i) => (
                    <div key={p.market} className="rounded-xl border border-border bg-secondary/30 p-3">
                      <div className="flex items-center gap-2.5">
                        <TokenIcon symbol={sym(p.market)} size={22} />
                        <span className="text-sm font-medium">{sym(p.market)}</span>
                        <span className="ml-auto flex items-center gap-1 text-sm">
                          <input
                            inputMode="decimal"
                            value={typing?.i === i ? typing.text : String(p.bps / 100)}
                            onChange={(e) => {
                              setTyping({ i, text: e.target.value });
                              const bps = pctToBps(e.target.value);
                              if (bps !== null) setBps(i, bps);
                            }}
                            onBlur={() => setTyping(null)}
                            disabled={busy}
                            aria-label={`${sym(p.market)} weight in percent`}
                            className="tnum w-16 rounded-md border border-transparent bg-transparent px-1 py-0.5 text-right outline-none focus:border-border focus:bg-background"
                          />
                          <span className="text-muted-foreground">%</span>
                        </span>
                        <button
                          type="button"
                          onClick={() => toggle(p.market)}
                          disabled={busy}
                          aria-label={`Remove ${sym(p.market)}`}
                          className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                        >
                          <X className="size-4" />
                        </button>
                      </div>
                      <input
                        type="range"
                        min={0}
                        max={10000}
                        step={50}
                        value={p.bps}
                        onChange={(e) => setBps(i, Number(e.target.value))}
                        disabled={busy}
                        aria-label={`${sym(p.market)} weight`}
                        className="range mt-2 w-full"
                        style={{ "--pct": `${p.bps / 100}%` } as React.CSSProperties}
                      />
                    </div>
                  ))}
                </div>
              </>
            )}

            <div className="space-y-3 p-5 pt-0 sm:p-6 sm:pt-0">
              {picks.length > 0 && invalid && <p className="text-xs text-warning">{invalid}</p>}
              <Button
                className="w-full py-3"
                pending={busy}
                disabled={!ready || (authenticated && !!invalid)}
                onClick={() => void submit()}
              >
                {busy ? "Creating basket…" : authenticated ? "Create basket" : "Connect to create"}
              </Button>
              {error && <ErrorBox message={error} />}
            </div>
          </Panel>

          <div className="space-y-3 lg:sticky lg:top-6">
            <Panel className="p-5 sm:p-6">
              <Label>Preview</Label>
              <div className="mt-4 flex items-start gap-3">
                {picks.length > 0 ? (
                  <IconStack symbols={picks.map((p) => sym(p.market))} size={36} ring="ring-card" />
                ) : (
                  <span className="grid size-9 place-items-center rounded-full bg-secondary text-sm font-semibold">
                    {name.trim().charAt(0).toUpperCase() || "?"}
                  </span>
                )}
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-semibold">{name.trim() || "Untitled basket"}</p>
                  <p className="truncate text-xs text-muted-foreground">{isPublic ? "Public" : "Private"} basket</p>
                </div>
              </div>
              <div className="mt-6 flex h-2 overflow-hidden rounded-full bg-secondary">
                {picks.map((p, i) => (
                  <div
                    key={p.market}
                    className="h-full transition-[width] duration-300"
                    style={{ width: `${p.bps / 100}%`, background: COLORS[i % COLORS.length] }}
                  />
                ))}
              </div>
              <ul className="mt-4 space-y-2">
                {picks.length === 0 && <li className="text-sm text-muted-foreground">Pick markets to see the mix.</li>}
                {picks.map((p, i) => (
                  <li key={p.market} className="flex items-center gap-2 text-sm">
                    <span className="size-2 rounded-full" style={{ background: COLORS[i % COLORS.length] }} />
                    {sym(p.market)}
                    <span className="tnum ml-auto text-muted-foreground">{fmtBps(p.bps)}</span>
                  </li>
                ))}
              </ul>
            </Panel>
            <p className="px-1 text-xs leading-relaxed text-muted-foreground">
              Anyone can buy a public basket; each buyer&apos;s legs fill on Kuru into their own wallet. Rebalance
              and TP/SL automations run per holder.
            </p>
          </div>
        </div>
      </div>
    </main>
  );
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
  return (
    <h2 className="flex items-center gap-2 text-base font-medium">
      <span className="grid size-6 place-items-center rounded-full bg-primary/15 text-xs text-primary">{n}</span>
      {children}
    </h2>
  );
}
