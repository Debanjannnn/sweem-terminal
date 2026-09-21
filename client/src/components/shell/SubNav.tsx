"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { motion, useReducedMotion } from "motion/react";
import { PILL_SPRING } from "@/components/motion";
import { PERP_NAV } from "@/lib/nav";
import { cn } from "@/lib/cn";

export function SubNav() {
  const pathname = usePathname();
  const reduced = useReducedMotion();

  return (
    <nav className="flex shrink-0 items-center gap-0.5 border-b border-border px-6 py-2">
      {PERP_NAV.map((item) => {
        const active = pathname === item.href;
        return (
          <Link
            key={item.href}
            href={item.href}
            className={cn(
              "relative rounded-full px-5 py-2 text-sm font-medium transition-colors",
              active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {active &&
              (reduced ? (
                <span className="absolute inset-0 rounded-full bg-secondary" />
              ) : (
                <motion.span
                  layoutId="subnav-pill"
                  className="absolute inset-0 rounded-full bg-secondary"
                  transition={PILL_SPRING}
                />
              ))}
            <span className="relative">{item.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
