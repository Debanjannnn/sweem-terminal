// blocks are finalized, so (block, logIndex) is stable forever
export const eventId = (blockNumber: bigint | number, logIndex: number) => `${blockNumber}-${logIndex}`;

// a slot is the only stable handle kuru gives a resting order: orderId changes
// on replace, and slots are reused across orders.
export const kuruSlotId = (market: string, userId: bigint, slotIdx: number) =>
  `${market.toLowerCase()}-${userId}-${slotIdx}`;

// the venue's own stable fill identity; recordIdx is the byte offset ordinal
export const kuruFillId = (market: string, tradeId: bigint, recordIdx: number) =>
  `${market.toLowerCase()}-${tradeId}-${recordIdx}`;

export const kuruBalanceId = (accountId: bigint, token: string) =>
  `${accountId}-${token.toLowerCase()}`;
