"use client";

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { Check, Copy, ExternalLink, Globe, X } from "lucide-react";
import { isAddress } from "viem";
import { AURORA_KEY, auroraQuote, auroraStatus, chainName, chainRank, EVM, getAuroraTokens, tokenLogos, txHash, type AuroraQuote, type AuroraSwap, type AuroraToken } from "@/lib/aurora";
import { fmtUsd } from "@/lib/api";
import { txUrl } from "@/lib/config";
import { useCopy, useDebounced } from "@/lib/hooks";
import { useAsync, useSession } from "@/lib/session";
import { parseAmount, tokenBySymbol } from "@/lib/tokens";
import { cn } from "@/lib/utils";
import { Button, ErrorBox, Row, Skeleton } from "@/components/ui";
import { EASE } from "@/components/motion";
import { TokenIcon } from "@/components/TokenIcon";
import { ProtocolIcon } from "@/components/ProtocolIcon";

type Pending = { q: AuroraQuote; token: AuroraToken; wallet: string };
const SAVED = "aurora-deposit";
function savePending(d: Pending | null) {
  try {
    if (d) localStorage.setItem(SAVED, JSON.stringify(d));
    else localStorage.removeItem(SAVED);
  } catch {}
}
function loadPending(wallet: string | undefined): Pending | null {
  try {
    const d = JSON.parse(localStorage.getItem(SAVED) ?? "null") as Pending | null;
    const live = d && d.wallet === wallet && (!d.q.quote.deadline || new Date(d.q.quote.deadline).getTime() > Date.now());
    return live ? d : null;
  } catch {
    return null;
  }
}

const STEPS = [
  { key: "PENDING_DEPOSIT", label: "Waiting for deposit" },
  { key: "PROCESSING", label: "Swapping to USDC" },
  { key: "SUCCESS", label: "Arrived on Monad" },
];
const stepOf = (s: AuroraSwap["status"] | undefined) => (s === "SUCCESS" ? 2 : s === "PROCESSING" ? 1 : 0);

// "Fund from any chain" trigger; need = USDC (display units) the user is short, prefilled as the amount to receive
export function AnyChainButton({ need, onArrived, className, children }: { need?: number; onArrived?: () => void; className?: string; children?: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  if (!AURORA_KEY) return null;
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} className={cn("inline-flex items-center gap-1.5 text-xs text-primary hover:underline", className)}>
        {children ?? (
          <>
            <Globe className="size-3.5" />
            {need ? `Add ${fmtUsd(need)} from any chain` : "Deposit from any chain"}
          </>
        )}
      </button>
      <AnimatePresence>{open && <AnyChainDeposit need={need} onClose={() => setOpen(false)} onArrived={onArrived} />}</AnimatePresence>
    </>
  );
}

export function AnyChainDeposit({ need, onClose, onArrived }: { need?: number; onClose: () => void; onArrived?: () => void }) {
  const { address } = useSession();
  const tokens = useAsync("aurora-tokens", getAuroraTokens);
  const [chain, setChain] = useState("base");
  const [assetId, setAssetId] = useState<string | null>(null);
  const [amount, setAmount] = useState(need ? String(Math.ceil(need * 100) / 100) : "");
  const [refund, setRefund] = useState("");
  // an open deposit survives closing the dialog (per wallet, until its address expires or it settles)
  const [deposit, setDeposit] = useState<Pending | null>(() => loadPending(address));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const ref = useRef<HTMLDialogElement>(null);
  const present = useIsPresent();
  useEffect(() => ref.current?.showModal(), []);

  const all = tokens.data ?? [];
  const dest = all.find((t) => t.blockchain === "monad" && t.symbol === "USDC");
  const chains = [...new Set(all.map((t) => t.blockchain))].filter((c) => c !== "monad").sort((a, b) => chainRank(a) - chainRank(b) || chainName(a).localeCompare(chainName(b)));
  const onChain = all.filter((t) => t.blockchain === chain).sort((a, b) => tokenRank(a) - tokenRank(b) || a.symbol.localeCompare(b.symbol));
  const token = onChain.find((t) => t.assetId === assetId) ?? onChain[0];
  const evm = EVM.has(chain);
  const refundTo = evm ? address : refund.trim();
  const out = dest ? parseAmount(amount, dest.decimals) : null;
  const ready = !!(dest && token && address && out && out > BigInt(0) && refundTo && (!evm || isAddress(refundTo)));
  const key = ready ? `${token!.assetId}:${out}:${refundTo}` : null;

  // live dry quote (no deposit address yet), debounced
  const settledKey = useDebounced(key);
  const pq = useAsync(key && key === settledKey && !deposit ? `aurora:${key}` : null, () =>
    auroraQuote({ dry: true, exactOut: true, amount: out!.toString(), origin: token!.assetId, destination: dest!.assetId, recipient: address!, refundTo: refundTo! }),
  );

  async function start() {
    setBusy(true);
    setErr(null);
    try {
      const q = await auroraQuote({ dry: false, exactOut: true, amount: out!.toString(), origin: token!.assetId, destination: dest!.assetId, recipient: address!, refundTo: refundTo! });
      if (!q.quote.depositAddress) throw new Error("No deposit address returned");
      const d = { q, token: token!, wallet: address! };
      savePending(d);
      setDeposit(d);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    // Esc/backdrop → unmount via state so the exit animation plays (same as SearchDialog)
    <motion.dialog
      ref={ref}
      aria-label="Deposit from any chain"
      onClose={onClose}
      onCancel={(e) => (e.preventDefault(), onClose())}
      onClick={(e) => e.target === e.currentTarget && onClose()}
      initial={{ opacity: 0, scale: 0.98, y: 8 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      exit={{ opacity: 0, scale: 0.98, y: 8 }}
      transition={{ duration: 0.18, ease: EASE }}
      className={cn(
        "mx-auto mt-[8vh] w-[calc(100%-2rem)] max-w-md space-y-5 rounded-3xl border border-border bg-popover p-5 text-foreground shadow-2xl sm:p-6 backdrop:bg-black/60 backdrop:backdrop-blur-sm backdrop:transition-opacity backdrop:duration-200 starting:backdrop:opacity-0",
        !present && "pointer-events-none backdrop:opacity-0",
      )}
    >
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-lg font-medium">Deposit from any chain</h2>
            <p className="mt-1 text-xs text-muted-foreground">Arrives as USDC in your Monad wallet</p>
          </div>
          <button type="button" aria-label="Close" onClick={onClose} className="rounded-full p-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground">
            <X className="size-4" />
          </button>
        </div>

        {!address ? (
          <p className="text-sm text-muted-foreground">Log in first so funds have a wallet to land in.</p>
        ) : tokens.error ? (
          <ErrorBox message={`Aurora unavailable: ${tokens.error}`} />
        ) : !tokens.data ? (
          <Skeleton className="h-64 w-full rounded-2xl" />
        ) : deposit ? (
          <Progress
            deposit={deposit}
            onArrived={onArrived}
            onReset={() => {
              savePending(null);
              setDeposit(null);
            }}
          />
        ) : (
          <>
            <Field label="From">
              <More items={chains} keep={10} selected={chain} search={chainName} placeholder="Search chains">
                {(c) => (
                  <Chip key={c} on={c === chain} onClick={() => (setChain(c), setAssetId(null))}>
                    {chainName(c)}
                  </Chip>
                )}
              </More>
            </Field>
            <Field label="Pay with">
              <More key={chain} items={onChain} keep={5} selected={token}>
                {(t) => (
                  <Chip key={t.assetId} on={t.assetId === token?.assetId} onClick={() => setAssetId(t.assetId)}>
                    <AuroraIcon token={t} size={16} />
                    {t.symbol}
                  </Chip>
                )}
              </More>
            </Field>
            <Field label="You receive">
              <div className="flex items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3">
                <input
                  autoFocus
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  inputMode="decimal"
                  placeholder="0"
                  aria-label="USDC to receive"
                  className="tnum w-full min-w-0 bg-transparent text-3xl font-medium outline-none placeholder:text-muted-foreground/40"
                />
                <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-secondary/70 py-1.5 pl-2 pr-3 text-sm font-medium">
                  <TokenIcon symbol="USDC" size={18} /> USDC
                </span>
              </div>
            </Field>
            {!evm && (
              <Field label={`Your ${chainName(chain)} address, for refunds`}>
                <input
                  value={refund}
                  onChange={(e) => setRefund(e.target.value)}
                  placeholder="Refund address"
                  className="w-full rounded-xl border border-border bg-card px-3 py-2.5 font-mono text-xs outline-none focus:border-primary/50"
                />
              </Field>
            )}

            <dl className="tnum space-y-1.5 rounded-2xl bg-secondary/40 px-4 py-3 text-sm">
              <Row label="You send">
                {pq.data ? `${pq.data.quote.amountInFormatted} ${token?.symbol}` : ready && !pq.error ? <Skeleton className="h-4 w-24" /> : "-"}
              </Row>
              <Row label="Value">{pq.data ? fmtUsd(Number(pq.data.quote.amountInUsd)) : "-"}</Row>
              <Row label="Time">{pq.data ? `~${eta(pq.data.quote.timeEstimate)}` : "-"}</Row>
            </dl>
            {pq.error && <ErrorBox message={pq.error} />}
            {err && <ErrorBox message={err} />}
            <Button className="w-full py-3" pending={busy} disabled={!pq.data || busy} onClick={() => void start()}>
              Get deposit address
            </Button>
          </>
        )}

        <p className="flex items-center justify-center gap-1.5 text-[11px] text-muted-foreground">
          <ProtocolIcon protocol="aurora" size={14} />
          Routed by Aurora Intents on NEAR Intents
        </p>
    </motion.dialog>
  );
}

// deposit address + live status until the USDC lands
function Progress({ deposit, onArrived, onReset }: { deposit: Pending; onArrived?: () => void; onReset: () => void }) {
  const { q, token } = deposit;
  const addr = q.quote.depositAddress!;
  const [s, setS] = useState<AuroraSwap | null>(null);
  const { copied, copy } = useCopy(1500);
  const done = s?.status === "SUCCESS" || s?.status === "REFUNDED" || s?.status === "FAILED";

  useEffect(() => {
    if (done) return;
    let live = true;
    const tick = () => auroraStatus(addr, q.quote.depositMemo).then((r) => live && setS(r)).catch(() => {});
    void tick();
    const t = setInterval(tick, 5000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [addr, q.quote.depositMemo, done]);

  useEffect(() => {
    if (s?.status === "SUCCESS") onArrived?.();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s?.status]);

  const step = stepOf(s?.status);
  const destTx = s?.swapDetails?.destinationChainTxHashes?.[0];

  return (
    <div className="space-y-5">
      {!done && (
        <div className="space-y-3">
          <p className="text-sm">
            Send exactly this amount on {chainName(token.blockchain)}, from any wallet:
          </p>
          <CopyField label="Amount" value={q.quote.amountInFormatted} suffix={token.symbol} copied={copied} copy={copy} />
          <CopyField label="To address" value={addr} mono copied={copied} copy={copy} />
          {q.quote.depositMemo && (
            <p className="text-xs text-warning">
              Memo required: <span className="font-mono">{q.quote.depositMemo}</span>
            </p>
          )}
          {q.quote.deadline && <p className="text-[11px] text-muted-foreground">Address valid until {new Date(q.quote.deadline).toLocaleString()}. Unused funds are refunded.</p>}
        </div>
      )}

      <ol className="space-y-2.5">
        {STEPS.map((st, i) => (
          <li key={st.key} className="flex items-center gap-3 text-sm">
            <span
              className={cn(
                "grid size-6 shrink-0 place-items-center rounded-full text-[11px]",
                i < step || s?.status === "SUCCESS" ? "bg-positive/15 text-positive" : i === step ? "bg-primary/15 text-primary" : "bg-secondary text-muted-foreground",
              )}
            >
              {i < step || s?.status === "SUCCESS" ? <Check className="size-3.5" /> : i + 1}
            </span>
            <span className={i <= step ? "text-foreground" : "text-muted-foreground"}>{st.label}</span>
            {i === step && !done && <span className="ml-auto size-1.5 animate-pulse rounded-full bg-primary" />}
          </li>
        ))}
      </ol>

      {s?.status === "SUCCESS" && (
        <div className="space-y-1 rounded-2xl bg-positive/10 px-4 py-3 text-sm text-positive">
          <p className="font-medium">{s.swapDetails?.amountOutFormatted ?? q.quote.amountOutFormatted} USDC arrived in your wallet</p>
          {destTx && (
            <a href={txUrl(txHash(destTx))} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-xs hover:underline">
              View on Monadscan <ExternalLink className="size-3" />
            </a>
          )}
        </div>
      )}
      {(s?.status === "REFUNDED" || s?.status === "FAILED") && (
        <ErrorBox message={`${s.status === "REFUNDED" ? "Refunded" : "Failed"}${s.swapDetails?.refundReason ? `: ${s.swapDetails.refundReason}` : ""}`} />
      )}
      {s?.status === "INCOMPLETE_DEPOSIT" && <p className="text-xs text-warning">Deposit below the quoted amount. Send the rest, or it is refunded after the deadline.</p>}
      {(done || s?.status === "PENDING_DEPOSIT") && (
        <button type="button" onClick={onReset} className="w-full text-center text-xs text-muted-foreground hover:text-foreground">
          {done ? "New deposit" : "Cancel and start over"}
        </button>
      )}
    </div>
  );
}

// selectable value + copy button (buttons can't be text-selected, so the value is plain text)
function CopyField({ label, value, suffix, mono, copied, copy }: { label: string; value: string; suffix?: string; mono?: boolean; copied: string | null; copy: (v: string) => void }) {
  return (
    <div className="flex items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="text-[11px] text-muted-foreground">{label}</p>
        <p className={cn("select-all break-all", mono ? "font-mono text-xs leading-5" : "tnum text-base font-medium")}>
          {value}
          {suffix && <span className="select-none text-muted-foreground"> {suffix}</span>}
        </p>
      </div>
      <button
        type="button"
        aria-label={`Copy ${label.toLowerCase()}`}
        onClick={() => copy(value)}
        className="shrink-0 rounded-lg p-2 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
      >
        {copied === value ? <Check className="size-4 text-positive" /> : <Copy className="size-4" />}
      </button>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">{label}</p>
      {children}
    </div>
  );
}

// first `keep` chips (plus the selected one), the rest behind a "+N more" toggle; `search` adds a filter box over all items
function More<T>({
  items,
  keep,
  selected,
  search,
  placeholder,
  children,
}: {
  items: T[];
  keep: number;
  selected: T | undefined;
  search?: (x: T) => string;
  placeholder?: string;
  children: (x: T) => React.ReactNode;
}) {
  const [all, setAll] = useState(false);
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const shown = needle && search ? items.filter((x) => search(x).toLowerCase().includes(needle)) : all ? items : items.filter((x, i) => i < keep || x === selected);
  return (
    <div className="space-y-2">
      {search && (
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={placeholder}
          aria-label={placeholder}
          className="w-full rounded-full border border-border bg-transparent px-3 py-1.5 text-xs outline-none placeholder:text-muted-foreground/60 focus:border-primary/50"
        />
      )}
      <div className="flex flex-wrap gap-1.5">
        {shown.map(children)}
        {needle && shown.length === 0 && <span className="px-1 py-1 text-xs text-muted-foreground">No match</span>}
        {!needle && items.length > shown.length && (
          <button type="button" onClick={() => setAll(true)} className="rounded-full px-2 py-1 text-xs text-muted-foreground hover:text-foreground">
            +{items.length - shown.length} more
          </button>
        )}
      </div>
    </div>
  );
}

// local icon if we ship one, else each remote logo in turn (onError → next), else nothing (no letter placeholder)
function AuroraIcon({ token, size }: { token: AuroraToken; size: number }) {
  const local = token.symbol === "ETH" ? "WETH" : token.symbol;
  const [i, setI] = useState(0);
  const urls = tokenLogos(token);
  if (tokenBySymbol(local)?.icon) return <TokenIcon symbol={local} size={size} />;
  if (i >= urls.length) return null;
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img src={urls[i]} alt={token.symbol} width={size} height={size} onError={() => setI(i + 1)} className="shrink-0 rounded-full" style={{ width: size, height: size }} />
  );
}

// USDC, USDT, native coin, other dollars, the rest
const tokenRank = (t: AuroraToken) =>
  t.symbol === "USDC" ? 0 : /^USDT/.test(t.symbol) ? 1 : !t.contractAddress ? 2 : /^USD|DAI$/.test(t.symbol) ? 3 : /^W?(ETH|BTC)$|^cbBTC$/.test(t.symbol) ? 4 : 5;

function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs transition-colors",
        on ? "border-primary/50 bg-primary/10 text-foreground" : "border-border text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

const eta = (s: number) => (s < 90 ? `${Math.round(s)}s` : `${Math.round(s / 60)} min`);
