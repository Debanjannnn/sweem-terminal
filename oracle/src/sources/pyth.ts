import type { Address, Hex, PublicClient } from "viem";
import { type PriceSource, type Sample, assertFresh } from "./source.ts";

const abi = [
  {
    name: "getPriceUnsafe",
    type: "function",
    stateMutability: "view",
    inputs: [{ name: "id", type: "bytes32" }],
    outputs: [
      {
        type: "tuple",
        components: [
          { name: "price", type: "int64" },
          { name: "conf", type: "uint64" },
          { name: "expo", type: "int32" },
          { name: "publishTime", type: "uint256" },
        ],
      },
    ],
  },
] as const;

// expo is negative and per-feed; never assume 8 decimals
export function toWad(price: bigint, expo: number): bigint {
  const shift = 18 + expo;
  return shift >= 0 ? price * 10n ** BigInt(shift) : price / 10n ** BigInt(-shift);
}

// "Unsafe" means no staleness check on chain, so it is ours to make: pyth is a pull
// oracle and an unpushed chain serves an arbitrarily old price without erroring.
export function pythSource(
  client: Pick<PublicClient, "readContract">,
  contract: Address,
  feedId: Hex,
  staleMs: number,
): PriceSource {
  return {
    name: "pyth",
    async read(): Promise<Sample> {
      const p = await client.readContract({ address: contract, abi, functionName: "getPriceUnsafe", args: [feedId] });
      if (p.price <= 0n) throw new Error(`pyth ${feedId}: non-positive price ${p.price}`);
      return assertFresh("pyth", { price: toWad(p.price, p.expo), sourceTs: Number(p.publishTime) * 1000 }, staleMs);
    },
  };
}
