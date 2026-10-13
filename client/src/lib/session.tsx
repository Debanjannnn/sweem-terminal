"use client";

import {
  PrivyProvider,
  usePrivy,
  useSigners,
  useExportWallet,
  type WalletWithMetadata,
} from "@privy-io/react-auth";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { deleteDelegation, errMsg, getMe, putDelegation } from "./api";
import { chain, POLICY_ID, PRIVY_APP_ID, SIGNER_ID } from "./config";
import type { Me } from "./types";

export type Session = {
  ready: boolean;
  authenticated: boolean;
  login: () => void;
  logout: () => Promise<void>;
  wallet: WalletWithMetadata | undefined;
  address: `0x${string}` | undefined;
  mainAddress: string | undefined; // external wallet used to log in; undefined for email logins
  me: Me | null;
  delegated: boolean;
  exportWallet: () => Promise<void>;
  delegate: () => Promise<void>;
  revoke: () => Promise<void>;
  delegating: boolean;
  delegationError: string | null;
  signerConfigured: boolean;
};

const Ctx = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(Ctx);
  if (!s) throw new Error("useSession outside SessionProvider");
  return s;
}

function embeddedWallet(accounts: readonly unknown[] | undefined): WalletWithMetadata | undefined {
  return (accounts as WalletWithMetadata[] | undefined)?.find(
    (a) =>
      a.type === "wallet" &&
      a.chainType === "ethereum" &&
      (a.walletClientType === "privy" || a.walletClientType === "privy-v2"),
  );
}

const externalWallet = (accounts: readonly unknown[] | undefined) =>
  (accounts as WalletWithMetadata[] | undefined)?.find((a) => a.type === "wallet" && a.chainType === "ethereum" && !a.walletClientType?.startsWith("privy"));

function SessionInner({ children }: { children: React.ReactNode }) {
  const { ready, authenticated, user, login, logout } = usePrivy();
  const { addSigners, removeSigners } = useSigners();
  const { exportWallet } = useExportWallet();

  const [me, setMe] = useState<Me | null>(null);
  const [delegating, setDelegating] = useState(false);
  const [delegationError, setDelegationError] = useState<string | null>(null);

  const wallet = embeddedWallet(user?.linkedAccounts);

  // failures keep the last /me (null until the first success); live = drop a reply that lands after logout/user switch
  const refreshMe = useCallback((live: () => boolean = () => true) => getMe().then((m) => live() && setMe(m), () => {}), []);

  useEffect(() => {
    if (!ready || !authenticated) return;
    let live = true;
    void refreshMe(() => live);
    return () => {
      live = false;
    };
  }, [ready, authenticated, user?.id, refreshMe]);

  const doLogout = useCallback(async () => {
    setMe(null);
    await logout();
  }, [logout]);

  const doExportWallet = useCallback(async () => {
    if (wallet) await exportWallet({ address: wallet.address });
  }, [wallet, exportWallet]);

  // busy/error wrapper for delegate + revoke; refreshes /me after
  const delegation = async (fn: () => Promise<unknown>) => {
    setDelegating(true);
    setDelegationError(null);
    try {
      await fn();
      await refreshMe();
    } catch (e) {
      setDelegationError(errMsg(e));
    } finally {
      setDelegating(false);
    }
  };

  // Privy signer first (gives the wallet its server id), then register it with the api.
  const delegate = async () => {
    if (!wallet) return;
    if (!SIGNER_ID) return setDelegationError("NEXT_PUBLIC_PRIVY_SIGNER_ID is not set");
    await delegation(async () => {
      let w = wallet;
      if (!w.delegated || !w.id) {
        const res = await addSigners({
          address: wallet.address,
          signers: [{ signerId: SIGNER_ID, policyIds: POLICY_ID ? [POLICY_ID] : [] }],
        });
        w = embeddedWallet(res.user.linkedAccounts) ?? w;
      }
      if (!w.id) throw new Error("wallet has no server id after adding signer");
      await putDelegation(w.id, w.address);
    });
  };

  const revoke = async () => {
    if (!wallet) return;
    await delegation(async () => {
      await removeSigners({ address: wallet.address });
      await deleteDelegation();
    });
  };

  return (
    <Ctx.Provider
      value={{
        ready,
        authenticated,
        login,
        logout: doLogout,
        wallet,
        address: wallet?.address as `0x${string}` | undefined,
        mainAddress: externalWallet(user?.linkedAccounts)?.address,
        me,
        delegated: !!me?.delegation?.active,
        exportWallet: doExportWallet,
        delegate,
        revoke,
        delegating,
        delegationError,
        signerConfigured: SIGNER_ID !== "",
      }}
    >
      {children}
    </Ctx.Provider>
  );
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  if (!PRIVY_APP_ID) {
    return (
      <div className="p-8 text-sm text-red-400">
        NEXT_PUBLIC_PRIVY_APP_ID is not set. Copy .env.example to .env.local.
      </div>
    );
  }
  return (
    <PrivyProvider
      appId={PRIVY_APP_ID}
      config={{
        loginMethods: ["email", "wallet"],
        defaultChain: chain,
        supportedChains: [chain],
        appearance: {
          theme: "dark",
          accentColor: "#c4f56a",
          landingHeader: "Sign in to sweem",
          loginMessage: "Trade and earn on Monad.",
          walletChainType: "ethereum-only",
        },
        embeddedWallets: { ethereum: { createOnLogin: "all-users" } },
      }}
    >
      <SessionInner>{children}</SessionInner>
    </PrivyProvider>
  );
}

// key null = idle; changing key refetches; reload() refetches keeping current data; pollMs = reload on an interval.
export function useAsync<T>(key: string | null, fn: () => Promise<T>, pollMs?: number) {
  const [nonce, setNonce] = useState(0);
  const [s, setS] = useState<{ key: string | null; nonce: number; data: T | null; error: string | null }>({
    key: null,
    nonce: -1,
    data: null,
    error: null,
  });

  useEffect(() => {
    if (key === null) return;
    let live = true;
    fn()
      .then((data) => live && setS({ key, nonce, data, error: null }))
      .catch((e) => live && setS({ key, nonce, data: null, error: errMsg(e) }));
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  useEffect(() => {
    if (key === null || !pollMs) return;
    const t = setInterval(reload, pollMs);
    return () => clearInterval(t);
  }, [key, pollMs, reload]);
  const same = key !== null && s.key === key;
  return {
    data: same ? s.data : null,
    error: same ? s.error : null,
    loading: key !== null && !(same && s.nonce === nonce),
    reload,
  };
}
