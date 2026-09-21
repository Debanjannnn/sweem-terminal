import {
  ArrowLeftRight,
  CalendarClock,
  CircleHelp,
  Grid2x2,
  Settings,
  Timer,
  TrendingUp,
  Wallet,
  type LucideIcon,
} from "lucide-react";

export type NavItem = { href: string; label: string; icon: LucideIcon };

export const RAIL: NavItem[] = [
  { href: "/spot", label: "Spot", icon: ArrowLeftRight },
  { href: "/perp", label: "Perp", icon: TrendingUp },
  { href: "/twap", label: "TWAP", icon: Timer },
  { href: "/dca", label: "DCA", icon: CalendarClock },
  { href: "/grid", label: "Grid", icon: Grid2x2 },
];

export const RAIL_FOOT: NavItem[] = [
  { href: "/portfolio", label: "Portfolio", icon: Wallet },
];

// ponytail: settings/help have no destination yet, link them when those routes exist
export const RAIL_CHROME = [
  { label: "Settings", icon: Settings },
  { label: "Help", icon: CircleHelp },
];

export const PERP_NAV = [
  { href: "/perp", label: "Perps" },
  { href: "/perp/power", label: "Power" },
  { href: "/perp/velocity", label: "Velocity" },
];

export const isPerp = (pathname: string) => pathname.startsWith("/perp");
