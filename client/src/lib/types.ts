// Wire shapes, field names as the backend sends them. Addresses lowercase.
// Raw* / quote amounts = base-unit integer strings; everything else decimal display strings.

export type Market = {
  market: string;
  base: string;
  quote: string;
  baseDecimals: number;
  quoteDecimals: number;
  pricePrecision: string;
  sizePrecision: string;
  tickSize: string;
  minSize: string;
  takerFeePps: string;
};

export type Component = { market: string; weightBps: number };

export type Nav = {
  navPerShare: string;
  totalNav: string;
  supply: string;
  ts: string;
};

export type BasketStats = {
  mints: number;
  redeems: number;
  usdcInRaw: string;
  usdcOutRaw: string;
  sharesRaw: string;
  holders: number;
};

export type Basket = {
  address: string;
  name: string;
  symbol: string;
  components: Component[];
  createdBlock: number;
  createdTx: string;
  creator: string | null;
  nav: Nav | null;
  stats?: BasketStats;
};

export type NavComponent = {
  market: string;
  token: string;
  balance: string;
  mid: string;
  value: string;
};

// market-data GET /baskets/{addr}/nav and WS topic nav.<basket>; ts unix ms.
export type LiveNav = {
  basket: string;
  block: number;
  ts: number;
  supply: string;
  totalNav: string;
  navPerShare: string;
  stale: boolean;
  components: NavComponent[];
};

export type Level = [px: string, sz: string];

// GET /books/{market} adds stale; WS book.<market> omits it.
export type Book = {
  market: string;
  seq: number;
  ts: number;
  bids: Level[];
  asks: Level[];
  stale?: boolean;
};

export type Trade = {
  market: string;
  ts: number;
  px: string;
  sz: string;
  side: "buy" | "sell";
};

export type QuoteLeg = {
  market: string;
  in: string;
  out: string;
  avgPx: string;
  impactBps: number;
};

export type Quote = {
  expectedOut: string;
  minOut: string;
  legs: QuoteLeg[];
  stale: boolean;
};

export type Delegation = {
  id: string;
  walletId: string;
  walletAddress: string;
  keyQuorumId: string;
  active: boolean;
  createdAt: string;
  revokedAt: string | null;
};

export type Me = {
  id: string;
  walletAddress: string | null;
  createdAt: string;
  delegation: Delegation | null;
};

export type Position = {
  basket: string;
  sharesRaw: string;
  navPerShare: string | null;
  valueUsdc: string | null;
  navTs: string | null;
};

export type Transfer = {
  kind: "mint" | "redeem";
  txHash: string;
  logIndex: number;
  basket: string;
  usdcRaw: string;
  sharesRaw: string;
  block: number;
  ts: string;
};

export type ScheduleKind = "recurring" | "stop_loss";

export type ScheduleInput = {
  basket: string;
  kind: ScheduleKind;
  amountUsdc?: string;
  intervalSeconds?: number;
  stopNavPerShare?: string;
  redeemBps?: number;
  maxSlippageBps: number;
  maxPerRunUsdc: string;
};

export type Schedule = {
  id: string;
  basket: string;
  kind: ScheduleKind;
  amountUsdc: string | null;
  intervalSeconds: number | null;
  nextRunAt: string | null;
  stopNavPerShare: string | null;
  redeemBps: number | null;
  maxSlippageBps: number;
  maxPerRunUsdc: string;
  active: boolean;
  createdAt: string;
};

export type ExecutionStatus = "pending" | "submitted" | "confirmed" | "failed" | "rejected";

export type Execution = {
  id: string;
  scheduleId: string;
  basket: string;
  slot: number;
  kind: "mint" | "redeem";
  status: ExecutionStatus;
  txHash: string | null;
  error: string | null;
  amountRaw: string | null;
  minOutRaw: string | null;
  createdAt: string;
  updatedAt: string;
};
