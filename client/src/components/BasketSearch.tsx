"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { Search } from "lucide-react";
import { getBaskets, getStrategies } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { LISTED, pairsOf } from "@/lib/tokens";
import { cn } from "@/lib/utils";
import { IconStack } from "@/components/invest/BasketCard";
import { parts } from "@/components/earn/sources";
import { EASE } from "@/components/motion";
import { AnimatePresence, motion, useIsPresent } from "motion/react";

const OPEN = "sweem:open-search";
const openSearch = () => document.dispatchEvent(new Event(OPEN));

function Kbd({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-grid h-6 min-w-6 place-items-center rounded-md bg-secondary px-1.5 font-sans text-xs text-muted-foreground",
        className,
      )}
    >
      {children}
    </kbd>
  );
}

// Big home search; flags <html data-hero> while on screen so the navbar trigger hides.
export function HeroSearch() {
  const ref = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const root = document.documentElement;
    const io = new IntersectionObserver(([e]) => root.toggleAttribute("data-hero", e.isIntersecting), {
      rootMargin: "-64px 0px 0px 0px",
    });
    io.observe(ref.current!);
    return () => {
      io.disconnect();
      root.removeAttribute("data-hero");
    };
  }, []);

  return (
    <button
      ref={ref}
      type="button"
      onClick={openSearch}
      className="group mx-auto flex w-full max-w-[580px] cursor-pointer items-center gap-3 rounded-full border-2 border-white/10 bg-card py-3.5 pr-5 pl-5 text-left text-muted-foreground ring-white/5 transition-[box-shadow,border-color] hover:border-white/15 hover:ring-4 md:py-4"
    >
      <Search className="size-5 shrink-0 transition-colors group-hover:text-foreground" />
      <span className="flex-1 text-[15px] md:text-[17px]">Search tokens, baskets and strategies</span>
      <span className="hidden gap-1 sm:flex">
        <Kbd>⌘</Kbd>
        <Kbd>K</Kbd>
      </span>
    </button>
  );
}

// Navbar trigger (hidden while HeroSearch is visible) + ⌘K + the dialog itself.
export function BasketSearch() {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const show = () => setOpen(true);
    const key = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen(true);
      }
    };
    document.addEventListener(OPEN, show);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener(OPEN, show);
      document.removeEventListener("keydown", key);
    };
  }, []);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="Search"
        className="group flex size-9 cursor-pointer items-center justify-center gap-2 rounded-full border border-white/10 bg-card text-sm text-muted-foreground transition-[opacity,translate,border-color] duration-200 hover:border-white/15 hover:text-foreground md:w-[320px] md:justify-between md:px-3 [html[data-hero]_&]:pointer-events-none [html[data-hero]_&]:translate-y-1 [html[data-hero]_&]:opacity-0"
      >
        <span className="flex items-center gap-2">
          <Search className="size-4" />
          <span className="hidden md:inline">Search…</span>
        </span>
        <span className="hidden gap-1 md:flex">
          <Kbd className="h-5 min-w-5 text-[11px]">⌘</Kbd>
          <Kbd className="h-5 min-w-5 text-[11px]">K</Kbd>
        </span>
      </button>
      <AnimatePresence>{open && <SearchDialog onClose={() => setOpen(false)} />}</AnimatePresence>
    </>
  );
}

function SearchDialog({ onClose }: { onClose: () => void }) {
  const router = useRouter();
  const ref = useRef<HTMLDialogElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const baskets = useAsync("baskets", () => getBaskets());
  const strategies = useAsync("strategies", () => getStrategies());
  const present = useIsPresent();

  useEffect(() => ref.current?.showModal(), []);

  const s = q.trim().toLowerCase();
  const all = [
    ...LISTED.map((m) => ({ key: m.symbol, href: `/trade/${m.symbol}`, title: m.token.name, tag: `$${m.token.symbol}`, sub: `Token · Kuru / ${pairsOf(m).map((p) => p.quote.symbol).join(", ")}`, icons: [m.token.symbol] })),
    ...(baskets.data ?? []).map((b) => ({
      key: b.id,
      href: `/baskets/${b.id}`,
      title: b.name,
      tag: "Basket",
      sub: b.legs.map((l) => `${l.market} ${l.weightBps / 100}%`).join(" · "),
      icons: b.legs.map((l) => l.market),
    })),
    ...(strategies.data ?? []).map((st) => ({
      key: st.id,
      href: `/earn/${st.id}`,
      title: st.name,
      tag: "Earn",
      sub: parts(st).map((sl) => `${sl.asset} · ${sl.sources.map((x) => x.kind).join(", ")}`).join(" | "),
      icons: parts(st).map((sl) => sl.asset),
    })),
  ];
  const items = all.filter((it) => !s || [it.title, it.tag, it.sub].some((v) => v.toLowerCase().includes(s)));
  const idx = Math.min(active, items.length - 1);

  const go = (href: string) => {
    onClose();
    router.push(href);
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && items[idx]) return go(items[idx].href);
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const next = (idx + (e.key === "ArrowDown" ? 1 : -1) + items.length) % Math.max(items.length, 1);
    setActive(next);
    list.current?.querySelectorAll("[role=option]")[next]?.scrollIntoView({ block: "nearest" });
  };

  return (
    // Esc/backdrop → unmount via state (not dialog.close) so the exit animation plays while still modal
    <motion.dialog
      ref={ref}
      onClose={onClose}
      onCancel={(e) => (e.preventDefault(), onClose())}
      onClick={(e) => e.target === e.currentTarget && onClose()}
      initial={{ opacity: 0, scale: 0.98 }}
      animate={{ opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.98 }}
      transition={{ duration: 0.18, ease: EASE }}
      className={cn(
        "mx-auto mt-[12vh] w-[calc(100%-2rem)] max-w-xl overflow-hidden rounded-2xl border border-border bg-popover p-0 text-foreground shadow-2xl backdrop:bg-black/60 backdrop:backdrop-blur-sm backdrop:transition-opacity backdrop:duration-200 starting:backdrop:opacity-0",
        !present && "pointer-events-none backdrop:opacity-0",
      )}
    >
      <label className="flex items-center gap-3 border-b border-border px-4">
        <Search className="size-4 shrink-0 text-muted-foreground" />
        <input
          autoFocus
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKey}
          placeholder="Search tokens, baskets or strategies…"
          role="combobox"
          aria-expanded
          aria-controls="basket-search-list"
          className="h-13 flex-1 bg-transparent text-[15px] outline-none placeholder:text-muted-foreground"
        />
      </label>

      <div ref={list} id="basket-search-list" role="listbox" className="h-[400px] overflow-y-auto p-2">
        <p className="flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-muted-foreground">
          <Search className="size-3" />
          {s ? "Search results" : "Explore"}
        </p>
        {items.map((it, i) => (
          <button
            key={it.key}
            type="button"
            role="option"
            aria-selected={i === idx}
            onMouseMove={() => setActive(i)}
            onClick={() => go(it.href)}
            className="flex w-full cursor-pointer items-center gap-3 rounded-2xl px-3 py-2 text-left transition-colors duration-150 aria-selected:bg-secondary"
          >
            <IconStack symbols={it.icons} size={28} ring="ring-popover" />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="flex items-center gap-2">
                <span className="truncate font-medium">{it.title}</span>
                <span className="text-sm text-muted-foreground">{it.tag}</span>
              </span>
              <span className="truncate text-xs text-muted-foreground">{it.sub}</span>
            </span>
          </button>
        ))}
        {items.length === 0 && (
          <p className="py-10 text-center text-sm text-muted-foreground">No results found.</p>
        )}
        {(baskets.error || strategies.error) && <p className="px-3 py-2 text-xs text-muted-foreground">Some results unavailable (api offline).</p>}
      </div>

      <div className="hidden items-center justify-center gap-5 border-t border-border px-3 py-3 text-xs md:flex">
        {[
          [["↑", "↓"], "List"],
          [["Enter"], "Select"],
          [["Esc"], "Close"],
        ].map(([keys, label]) => (
          <span key={label as string} className="flex items-center gap-2">
            {(keys as string[]).map((k) => (
              <Kbd key={k} className="h-5 text-[11px]">
                {k}
              </Kbd>
            ))}
            {label}
          </span>
        ))}
      </div>
    </motion.dialog>
  );
}
