import type { Address, PublicClient } from "viem";
import { type PriceSource, type Sample, assertFresh } from "./source.ts";

const abi = [
  { name: "decimals", type: "function", stateMutability: "view", inputs: [], outputs: [{ type: "uint8" }] },
  {
    name: "latestRoundData",
    type: "function",
    stateMutability: "view",
    inputs: [],
    outputs: [
      { name: "roundId", type: "uint80" },
      { name: "answer", type: "int256" },
      { name: "startedAt", type: "uint256" },
      { name: "updatedAt", type: "uint256" },
      { name: "answeredInRound", type: "uint80" },
    ],
  },
] as const;

export function chainlinkSource(
  client: Pick<PublicClient, "readContract">,
  feed: Address,
  staleMs: number,
): PriceSource {
  let decimals: number | null = null;
  return {
    name: "chainlink",
    async read(): Promise<Sample> {
      decimals ??= await client.readContract({ address: feed, abi, functionName: "decimals" });
      const [roundId, answer, , updatedAt] = await client.readContract({
        address: feed,
        abi,
        functionName: "latestRoundData",
      });
      if (answer <= 0n) throw new Error(`chainlink ${feed}: non-positive answer ${answer}`);
      const price =
        decimals <= 18 ? answer * 10n ** BigInt(18 - decimals) : answer / 10n ** BigInt(decimals - 18);
      return assertFresh("chainlink", { price, sourceTs: Number(updatedAt) * 1000, roundId }, staleMs);
    },
  };
}
