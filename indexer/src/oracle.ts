import type { IndexFeed } from "envio";

// id is the IndexOracle address — one contract per feed
export const emptyFeed = (address: string): IndexFeed => ({
  id: address,
  latestValue: 0n,
  latestRoundId: 0n,
  latestTimestamp: 0n,
  halted: false,
  publicationCount: 0,
  breakerTripCount: 0,
});
