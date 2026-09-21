"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { motion, useReducedMotion } from "motion/react";
import { PILL_SPRING } from "@/components/motion";
import { RAIL, RAIL_CHROME, RAIL_FOOT, type NavItem } from "@/lib/nav";
import { cn } from "@/lib/cn";

export function Rail() {
  const pathname = usePathname();
  const active = (href: string) => pathname === href || pathname.startsWith(`${href}/`);

  return (
    <aside className="flex w-[var(--rail-width)] shrink-0 flex-col items-center gap-1.5 border-r border-border bg-card py-4">
      <Link
        href="/spot"
        aria-label="sweem"
        className="mb-4 grid size-10 place-items-center rounded-xl bg-primary text-sm font-semibold text-primary-foreground"
      >
        sw
      </Link>
      <nav className="flex flex-col gap-1.5">
        {RAIL.map((item) => (
          <RailLink key={item.href} item={item} active={active(item.href)} />
        ))}
      </nav>
      <div className="mt-auto flex flex-col gap-1.5">
        {RAIL_FOOT.map((item) => (
          <RailLink key={item.href} item={item} active={active(item.href)} />
        ))}
        {RAIL_CHROME.map(({ label, icon: Icon }) => (
          <button
            key={label}
            type="button"
            aria-label={label}
            title={label}
            className="grid size-10 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-secondary/60 hover:text-foreground"
          >
            <Icon className="size-5" />
          </button>
        ))}
      </div>
    </aside>
  );
}

function RailLink({ item, active }: { item: NavItem; active: boolean }) {
  const reduced = useReducedMotion();
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      title={item.label}
      aria-label={item.label}
      aria-current={active ? "page" : undefined}
      className={cn(
        "relative grid size-10 place-items-center rounded-xl transition-colors",
        active
          ? "text-primary-foreground"
          : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
      )}
    >
      {active &&
        (reduced ? (
          <span className="absolute inset-0 rounded-xl bg-primary" />
        ) : (
          <motion.span
            layoutId="rail-active"
            className="absolute inset-0 rounded-xl bg-primary"
            transition={PILL_SPRING}
          />
        ))}
      <Icon className="relative size-5" />
    </Link>
  );
}
