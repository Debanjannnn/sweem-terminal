"use client";

import { MotionConfig, motion } from "motion/react";
import { usePathname } from "next/navigation";

export const EASE = [0.16, 1, 0.3, 1] as const;

export const PILL_SPRING = { type: "spring", stiffness: 400, damping: 34 } as const;

export const rise = {
  initial: { opacity: 0, y: 4 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.18, ease: EASE },
} as const;

export const STAGGER_STEP = 0.03;

export function MotionProvider({ children }: { children: React.ReactNode }) {
  return <MotionConfig reducedMotion="user">{children}</MotionConfig>;
}

export function PageTransition({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  return (
    <motion.div key={pathname} {...rise} className="flex min-h-0 flex-1 flex-col">
      {children}
    </motion.div>
  );
}
