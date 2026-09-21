"use client";

import {
  getAccessToken,
  PrivyProvider,
  usePrivy,
  useSigners,
  type WalletWithMetadata,
} from "@privy-io/react-auth";
import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { defineChain } from "viem";
import { CHAIN_ID, errorMessage, walletFetch, type Me } from "./api";

const APP_ID = process.env.NEXT_PUBLIC_PRIVY_APP_ID ?? "";
const SIGNER_ID = process.env.NEXT_PUBLIC_PRIVY_SIGNER_ID ?? "";
const POLICY_ID = process.env.NEXT_PUBLIC_PRIVY_POLICY_ID ?? "";

// neither Monad chain ships in viem/chains
export const monad = defineChain({
  id: 143,
  name: "Monad",
  nativeCurrency: { name: "Monad", symbol: "MON", decimals: 18 },
  rpcUrls: { default: { http: ["https://rpc.monad.xyz"] } },
  blockExplorers: { default: { name: "Monad Explorer", url: "https://explorer.monad.xyz" } },
});

export const monadTestnet = defineChain({
  id: 10143,
  name: "Monad Testnet",
  nativeCurrency: { name: "Monad", symbol: "MON", decimals: 18 },
  rpcUrls: { default: { http: ["https://testnet-rpc.monad.xyz"] } },
  testnet: true,
  blockExplorers: {
    default: { name: "Monad Explorer", url: "https://testnet.monadexplorer.com" },
  },
});

export const chain = CHAIN_ID === 143 ? monad : monadTestnet;

export type Session = {
  ready: boolean;
  authenticated: boolean;
  login: () => void;
  logout: () => Promise<void>;
  address: string | null;
  walletId: string | null;
  delegated: boolean;
  delegate: () => Promise<void>;
  revoke: () => Promise<void>;
  delegating: boolean;
  signerConfigured: boolean;
  policyId: string;
  signerId: string;
  error: string | null;
  me: Me | null;
  token: () => Promise<string>;
};

const Ctx = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(Ctx);
  if (!s) throw new Error("useSession outside SessionProvider");
  return s;
}

// both client types exist: checking only "privy" misses every v2 wallet
function embeddedWallet(accounts: readonly unknown[] | undefined) {
  return (accounts as WalletWithMetadata[] | undefined)?.find(
    (a) =>
      a.type === "wallet" &&
      a.chainType === "ethereum" &&
      (a.walletClientType === "privy" || a.walletClientType === "privy-v2"),
  );
}

function SessionInner({ children }: { children: React.ReactNode }) {
  const { ready, authenticated, user, login, logout } = usePrivy();
  const { addSigners, removeSigners } = useSigners();
  const [delegating, setDelegating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [me, setMe] = useState<Me | null>(null);

  const wallet = embeddedWallet(user?.linkedAccounts);
  const address = wallet?.address ?? null;
  const walletId = wallet?.id ?? null;
  const delegated = wallet?.delegated ?? false;

  const token = useCallback(async () => {
    const t = await getAccessToken();
    if (!t) throw new Error("This session has no Privy access token, so sign in again.");
    return t;
  }, []);

  const synced = useRef<string | null>(null);
  useEffect(() => {
    if (!ready || !authenticated || !address) return;
    const sig = `${address}|${walletId ?? ""}|${delegated}`;
    if (synced.current === sig) return;
    synced.current = sig;
    void (async () => {
      try {
        setMe(
          await walletFetch<Me>("/v1/me", await token(), {
            method: "POST",
            body: JSON.stringify({
              wallet_address: address,
              privy_wallet_id: walletId ?? "",
            }),
          }),
        );
        setError(null);
      } catch (e) {
        synced.current = null;
        setError(errorMessage(e));
      }
    })();
  }, [ready, authenticated, address, walletId, delegated, token]);

  const delegate = useCallback(async () => {
    if (!address) return;
    if (!SIGNER_ID) {
      setError(
        "NEXT_PUBLIC_PRIVY_SIGNER_ID is not set, so this app cannot name the signer that would execute for you.",
      );
      return;
    }
    setDelegating(true);
    setError(null);
    try {
      await addSigners({
        address,
        signers: [{ signerId: SIGNER_ID, policyIds: POLICY_ID ? [POLICY_ID] : [] }],
      });
      synced.current = null;
    } catch (e) {
      setError(errorMessage(e));
      throw e;
    } finally {
      setDelegating(false);
    }
  }, [address, addSigners]);

  // privy removes every signer on this wallet; there is no selective removal
  const revoke = useCallback(async () => {
    if (!address) return;
    setDelegating(true);
    setError(null);
    try {
      await removeSigners({ address });
      synced.current = null;
    } catch (e) {
      setError(errorMessage(e));
      throw e;
    } finally {
      setDelegating(false);
    }
  }, [address, removeSigners]);

  const doLogout = useCallback(async () => {
    synced.current = null;
    setMe(null);
    setError(null);
    await logout();
  }, [logout]);

  return (
    <Ctx.Provider
      value={{
        ready,
        authenticated,
        login,
        logout: doLogout,
        address,
        walletId,
        delegated,
        delegate,
        revoke,
        delegating,
        signerConfigured: SIGNER_ID !== "",
        signerId: SIGNER_ID,
        policyId: POLICY_ID,
        error,
        me,
        token,
      }}
    >
      {children}
    </Ctx.Provider>
  );
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  if (!APP_ID) {
    return (
      <div className="p-8 text-sm text-destructive">
        NEXT_PUBLIC_PRIVY_APP_ID is not set, so no session can be opened. Copy .env.example
        to .env.local and fill it in.
      </div>
    );
  }
  return (
    <PrivyProvider
      appId={APP_ID}
      config={{
        loginMethods: ["email", "wallet"],
        appearance: {
          theme: "dark",
          accentColor: "#c4f56a",
          landingHeader: "Sign in to sweem",
          walletChainType: "ethereum-only",
        },
        defaultChain: chain,
        supportedChains: [chain],
        embeddedWallets: { ethereum: { createOnLogin: "all-users" } },
      }}
    >
      <SessionInner>{children}</SessionInner>
    </PrivyProvider>
  );
}
