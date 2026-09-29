"use client";

import { fmtBps, fmtPct, fmtUsd, getBook } from "@/lib/api";
import { useAsync } from "@/lib/session";
import { baseToken } from "@/lib/tokens";
import { useTopic } from "@/lib/ws";
import type { Book, Component, LiveNav, Market } from "@/lib/types";
import { Panel, Skeleton } from "@/components/ui";
import { TokenIcon } from "@/components/TokenIcon";

const num = (s: string | undefined) => (s === undefined ? null : Number(s));

export function Composition({
  components,
  nav,
  markets,
}: {
  components: Component[];
  nav: LiveNav | null;
  markets: Market[] | null;
}) {
  const total = Number(nav?.totalNav ?? 0);
  return (
    <Panel>
      <h2 className="px-6 pb-4 pt-6 text-2xl font-medium">Composition</h2>
      <div className="overflow-x-auto border-t border-border">
        <table className="w-full min-w-[640px] text-sm">
          <thead className="text-left text-xs text-muted-foreground">
            <tr className="[&>th]:px-6 [&>th]:py-3 [&>th]:font-normal">
              <th>Token</th>
              <th className="text-right">Target / actual</th>
              <th className="text-right">Holdings</th>
              <th className="text-right">Mid</th>
              <th className="text-right">Bid / ask</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border border-t border-border">
            {components.map((c) => {
              const live = nav?.components.find((x) => x.market === c.market);
              const sym = baseToken(c.market, markets)?.symbol;
              const value = num(live?.value);
              return (
                <tr key={c.market} className="[&>td]:px-6 [&>td]:py-4">
                  <td>
                    <span className="inline-flex items-center gap-2 font-medium">
                      {sym ? <TokenIcon symbol={sym} size={20} /> : <Skeleton className="size-5 rounded-full" />}
                      {sym ?? "…"}
                    </span>
                  </td>
                  <td className="tnum text-right">
                    {fmtBps(c.weightBps)}
                    <span className="text-muted-foreground">
                      {" / "}
                      {value !== null && total > 0 ? fmtPct((value / total) * 100) : "—"}
                    </span>
                  </td>
                  <td className="tnum text-right">
                    {live ? (
                      <>
                        {Number(live.balance).toLocaleString("en-US", { maximumFractionDigits: 6 })}
                        <div className="text-xs text-muted-foreground">{fmtUsd(Number(live.value))}</div>
                      </>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="tnum text-right">{live ? fmtUsd(Number(live.mid)) : "—"}</td>
                  <td className="tnum text-right">
                    <BookTop market={c.market} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Panel>
  );
}

function BookTop({ market }: { market: string }) {
  const snap = useAsync(`book:${market}`, () => getBook(market));
  const book = useTopic<Book>(`book.${market}`) ?? snap.data;
  if (!book) return snap.loading ? <Skeleton className="ml-auto h-4 w-24" /> : <span className="text-muted-foreground">—</span>;
  const px = (l: Book["bids"][number] | undefined) => (l ? fmtUsd(Number(l[0])) : "—");
  return (
    <span>
      <span className="text-positive">{px(book.bids[0])}</span>
      <span className="text-muted-foreground"> / </span>
      <span className="text-destructive">{px(book.asks[0])}</span>
    </span>
  );
}
