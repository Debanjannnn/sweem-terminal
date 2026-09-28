import { ChevronDown } from "lucide-react";

const QA: readonly { q: string; a: string }[] = [
  {
    q: "What is a basket?",
    a: "An ERC-20 on Monad that holds a fixed-weight mix of tokens bought on Kuru order books. Buying mints shares with USDC; selling redeems them back to USDC.",
  },
  {
    q: "Who holds my funds?",
    a: "You do. Shares sit in your own Privy embedded wallet, and the tokens behind them sit in the basket contract, not with us.",
  },
  {
    q: "What does automation need?",
    a: "Recurring buys and stop-losses need a delegated signer bound to a policy that only allows basket mints and redeems. Revoke it any time and every schedule stops.",
  },
  {
    q: "Where do test tokens come from?",
    a: "The Kuru testnet faucet: claim USDC, WETH, cbBTC and XAUT once every 12 hours from the wallet menu.",
  },
];

export function Faq() {
  return (
    <section className="space-y-2">
      <h2 className="px-1 pb-1 text-sm font-medium text-muted-foreground">
        Questions
      </h2>
      {QA.map(({ q, a }) => (
        <details
          key={q}
          className="group rounded-xl border border-border bg-card/60 px-4 [&[open]]:bg-card"
        >
          <summary className="flex cursor-pointer list-none items-center gap-3 py-3.5 text-sm font-medium marker:hidden">
            {q}
            <ChevronDown className="ml-auto size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />
          </summary>
          <p className="pb-4 pr-7 text-sm leading-relaxed text-muted-foreground">
            {a}
          </p>
        </details>
      ))}
    </section>
  );
}
