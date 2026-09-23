import { indexer } from "envio";
import { kuruBalanceId } from "../ids.js";

// AccountCore holds every balance; orderbooks never hold tokens.

indexer.onEvent(
  { contract: "KuruAccountCore", event: "AccountRegistered" },
  async ({ event, context }) => {
    context.KuruAccount.set({
      id: `${event.params.accountId}`,
      address: event.params.account,
      rootId: event.params.rootId,
      createdAtBlock: BigInt(event.block.number),
    });
  },
);

// the balance truth. an authoritative post-state snapshot of (free, reserved),
// so it overwrites: the last one per (accountId, token) in log order is final.
// Deposit / Withdrawal / InternalAccountTransfer describe the same movements as
// history and are not bound at all — applying both double-counts.
indexer.onEvent(
  { contract: "KuruAccountCore", event: "SpotReserveUpdated" },
  async ({ event, context }) => {
    context.KuruBalance.set({
      id: kuruBalanceId(event.params.accountId, event.params.token),
      account_id: `${event.params.accountId}`,
      token: event.params.token,
      free: event.params.free,
      reserved: event.params.reserved,
      updatedAtBlock: BigInt(event.block.number),
      updatedAtTimestamp: BigInt(event.block.timestamp),
    });
  },
);
