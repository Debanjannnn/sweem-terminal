// blocks are finalized, so (block, logIndex) is stable forever
export const VAULT_ID = "SweemVault";
export const PROTOCOL_ID = "Sweem";

export const eventId = (blockNumber: bigint | number, logIndex: number) => `${blockNumber}-${logIndex}`;

export const positionId = (marketId: string, trader: string) => `${marketId}-${trader}`;

export const kuruOrderId = (market: string, orderId: bigint) => `${market.toLowerCase()}-${orderId}`;
