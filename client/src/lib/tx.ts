"use client";

import { useSendTransaction } from "@privy-io/react-auth";
import { useCallback } from "react";
import {
  createPublicClient,
  encodeFunctionData,
  erc20Abi,
  http,
  parseEventLogs,
  zeroAddress,
  type Abi,
  type Address,
  type Hex,
  type TransactionReceipt,
} from "viem";
import { basketAbi, chain, factoryAbi, FACTORY, FAUCET, faucetAbi, RPC_URL, USDC } from "./config";
import { useSession } from "./session";
import type { Component } from "./types";
import { showToast, toastError } from "@/components/TxToast";

export const publicClient = createPublicClient({ chain, transport: http(RPC_URL) });

const deadline = () => BigInt(Math.floor(Date.now() / 1000) + 600);

// native MON when token is the zero address
export const readBalance = (token: string, owner: Address): Promise<bigint> =>
  token === zeroAddress
    ? publicClient.getBalance({ address: owner })
    : publicClient.readContract({ address: token as Address, abi: erc20Abi, functionName: "balanceOf", args: [owner] });

export const readAllowance = (token: Address, owner: Address, spender: Address) =>
  publicClient.readContract({ address: token, abi: erc20Abi, functionName: "allowance", args: [owner, spender] });

// unix seconds → Date; in the past = claimable now
export const readNextClaimAt = async (owner: Address) =>
  new Date(Number(await publicClient.readContract({ address: FAUCET, abi: faucetAbi, functionName: "nextClaimAt", args: [owner] })) * 1000);

// User-signed txs from the Privy embedded wallet, with loading → result toasts. Throws on failure/revert.
export function useTx() {
  const { sendTransaction } = useSendTransaction();
  const { address } = useSession();

  const send = useCallback(
    async (label: string, to: Address, abi: Abi, functionName: string, args: readonly unknown[]): Promise<TransactionReceipt> => {
      if (!address) throw new Error("no wallet");
      const id = showToast({ tone: "loading", title: label });
      try {
        const data = encodeFunctionData({ abi, functionName, args });
        const { hash } = await sendTransaction(
          { to, data, chainId: chain.id },
          { address, uiOptions: { showWalletUIs: false, description: label } },
        );
        showToast({ tone: "loading", title: label, hash }, id);
        const receipt = await publicClient.waitForTransactionReceipt({ hash });
        if (receipt.status !== "success") throw Object.assign(new Error(`${label} reverted`), { hash });
        showToast({ tone: "success", title: `${label} confirmed`, hash }, id);
        return receipt;
      } catch (e) {
        const hash = (e as { hash?: Hex }).hash;
        if (hash) showToast({ tone: "error", title: `${label} reverted`, hash }, id);
        else toastError(e, id);
        throw e;
      }
    },
    [sendTransaction, address],
  );

  const approveIfNeeded = useCallback(
    async (token: Address, spender: Address, amount: bigint) => {
      if (!address || (await readAllowance(token, address, spender)) >= amount) return null;
      return send("Approve", token, erc20Abi, "approve", [spender, amount]);
    },
    [send, address],
  );

  // usdcIn raw 6dp; approves USDC first when short
  const mint = useCallback(
    async (basket: Address, usdcIn: bigint, minShares: bigint) => {
      await approveIfNeeded(USDC, basket, usdcIn);
      return send("Buy", basket, basketAbi, "mint", [usdcIn, minShares, deadline()]);
    },
    [send, approveIfNeeded],
  );

  const redeem = useCallback(
    (basket: Address, shares: bigint, minUsdcOut: bigint) =>
      send("Sell", basket, basketAbi, "redeem", [shares, minUsdcOut, deadline()]),
    [send],
  );

  // returns the new basket address from the BasketCreated log
  const createBasket = useCallback(
    async (name: string, symbol: string, components: Component[]): Promise<Address> => {
      const receipt = await send("Create basket", FACTORY, factoryAbi, "createBasket", [name, symbol, components]);
      const [log] = parseEventLogs({ abi: factoryAbi, eventName: "BasketCreated", logs: receipt.logs });
      return ((log?.args as { basket?: Address } | undefined)?.basket ?? zeroAddress).toLowerCase() as Address;
    },
    [send],
  );

  const claimFaucet = useCallback(() => send("Claim faucet", FAUCET, faucetAbi, "claim", []), [send]);

  return { address, send, approveIfNeeded, mint, redeem, createBasket, claimFaucet };
}
