import { CircleHelp, Settings, Wallet, type LucideIcon } from "lucide-react";

export type NavItem = { href: string; label: string; icon: LucideIcon };

export const RAIL: NavItem[] = [];

export const RAIL_FOOT: NavItem[] = [
  { href: "/portfolio", label: "Portfolio", icon: Wallet },
];

// ponytail: settings/help have no destination yet, link them when those routes exist
export const RAIL_CHROME = [
  { label: "Settings", icon: Settings },
  { label: "Help", icon: CircleHelp },
];
