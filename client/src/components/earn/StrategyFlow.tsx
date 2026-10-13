"use client";

import { useEffect, useMemo, useRef } from "react";
import { BaseEdge, Handle, Position, ReactFlow, ReactFlowProvider, getSmoothStepPath, useReactFlow, type Edge, type EdgeProps, type Node, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { ArrowDownUp, Droplets, Landmark, Layers, type LucideIcon } from "lucide-react";
import type { SourceKind } from "@/lib/types";
import { TokenIcon } from "@/components/TokenIcon";
import { cn } from "@/lib/utils";
import { pct } from "./Backtest";
import { KIND_LABEL, LP_ASSUMED_APR, PERP, venueName } from "./sources";

export type FlowLeg = { kind: SourceKind; venue: string; weightBps: number; apy: number | null; leverage: number };

const KIND_ICON: Record<SourceKind, LucideIcon> = { lending: Landmark, staking: Layers, lp: Droplets, basis: ArrowDownUp };

type CardData = { [k: string]: unknown; title: string; sub?: string; positive?: boolean; symbol?: string; kind?: SourceKind; ends?: "left" | "right" };
type CardNode = Node<CardData, "card">;

function Card({ data }: NodeProps<CardNode>) {
  const Icon = data.kind ? KIND_ICON[data.kind] : null;
  return (
    <div className={cn("flex h-full items-center gap-2.5 rounded-xl border px-3", data.ends === "left" ? "border-primary/40 bg-primary/5" : "border-border bg-card")}>
      {data.ends !== "left" && <Handle type="target" position={Position.Left} className="!opacity-0" />}
      {data.symbol && <TokenIcon symbol={data.symbol} size={22} />}
      {Icon && (
        <span className="grid size-[22px] shrink-0 place-items-center rounded-full bg-secondary text-muted-foreground">
          <Icon className="size-3" />
        </span>
      )}
      <div className="min-w-0">
        <div className="truncate text-sm font-medium leading-tight">{data.title}</div>
        {data.sub && <div className={cn("tnum mt-1 truncate text-[11px] leading-tight", data.positive ? "text-positive" : "text-muted-foreground")}>{data.sub}</div>}
      </div>
      {data.ends !== "right" && <Handle type="source" position={Position.Right} className="!opacity-0" />}
    </div>
  );
}

function FanEdge({ id, sourceX, sourceY, targetX, targetY, style }: EdgeProps) {
  const [path] = getSmoothStepPath({ sourceX, sourceY, targetX, targetY, sourcePosition: Position.Right, targetPosition: Position.Left, borderRadius: 12 });
  return <BaseEdge id={id} path={path} style={style} />;
}

const nodeTypes = { card: Card };
const edgeTypes = { fan: FanEdge };

// uniform height: unequal heights bow same-row edges
const ROW = 72;
const HEIGHT = 56;
const WIDTH = { wallet: 150, kind: 170, venue: 190 };
const COL = { wallet: 0, kind: 190, venue: 400 };

export const fmtAmt = (n: number) => n.toLocaleString("en-US", { maximumFractionDigits: n >= 100 ? 2 : 4 });

// leaf venues per source; basis splits into spot (L/(L+1)) and the perp short's margin
function leaves(l: FlowLeg, asset: string, amt: number): CardData[] {
  const apy = l.apy === null ? "-" : pct(l.apy);
  if (l.kind === "basis") {
    const L = l.leverage;
    return [
      { title: "Kuru spot", sub: `${fmtAmt((amt * L) / (L + 1))} ${asset} long` },
      { title: `Perpl ${PERP[asset]?.name ?? ""} short`, sub: `${L}× · ${apy} funding`, positive: l.apy !== null },
    ];
  }
  if (l.kind === "lp") return [{ title: "Kuru ActiveVault", sub: `~${pct(LP_ASSUMED_APR, 0)} est. · 4d lock`, positive: true }];
  return [{ title: venueName(l.venue), sub: `${apy} APY`, positive: l.apy !== null }];
}

export function StrategyFlow({ asset, amount, legs }: { asset: string; amount: number; legs: FlowLeg[] }) {
  const { nodes, edges, rows } = useMemo(() => {
    const nodes: CardNode[] = [];
    const edges: Edge[] = [];
    const node = (id: string, x: number, y: number, w: number, data: CardData): CardNode => ({ id, type: "card", position: { x, y }, data, style: { width: w, height: HEIGHT }, draggable: false });
    const edge = (source: string, target: string): Edge => ({ id: `${source}>${target}`, source, target, type: "fan", animated: true });

    let row = 0;
    for (const l of legs) {
      const amt = (amount * l.weightBps) / 10000;
      const ls = leaves(l, asset, amt);
      const kind = `kind:${l.kind}`;
      nodes.push(node(kind, COL.kind, (row + (ls.length - 1) / 2) * ROW, WIDTH.kind, { title: KIND_LABEL[l.kind], sub: `${l.weightBps / 100}% · ${fmtAmt(amt)} ${asset}`, kind: l.kind }));
      edges.push(edge("wallet", kind));
      ls.forEach((d, j) => {
        const id = `${kind}:${j}`;
        nodes.push(node(id, COL.venue, (row + j) * ROW, WIDTH.venue, { ...d, ends: "right" }));
        edges.push(edge(kind, id));
      });
      row += ls.length;
    }
    nodes.unshift(node("wallet", COL.wallet, ((row - 1) * ROW) / 2, WIDTH.wallet, { title: "Your wallet", sub: `${fmtAmt(amount)} ${asset}`, symbol: asset, ends: "left" }));
    return { nodes, edges, rows: row };
  }, [asset, amount, legs]);

  if (legs.length === 0) return <div className="flex h-40 items-center justify-center px-6 text-center text-sm text-muted-foreground">Pick a source to see where funds go</div>;

  return (
    <ReactFlowProvider>
      <Diagram nodes={nodes} edges={edges} height={rows * ROW + 24} />
    </ReactFlowProvider>
  );
}

// refit on resize: panel can mount before its column settles
function Diagram({ nodes, edges, height }: { nodes: CardNode[]; edges: Edge[]; height: number }) {
  const box = useRef<HTMLDivElement>(null);
  const { fitView } = useReactFlow();

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(() => void fitView({ padding: 0.06, maxZoom: 1, duration: 150 }));
    ro.observe(el);
    return () => ro.disconnect();
  }, [fitView]);

  // refit when the shape changes (source added/removed)
  useEffect(() => {
    void fitView({ padding: 0.06, maxZoom: 1, duration: 150 });
  }, [nodes.length, fitView]);

  return (
    <div ref={box} className="px-2 pb-2" style={{ height }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        fitViewOptions={{ padding: 0.06, maxZoom: 1 }}
        minZoom={0.3}
        maxZoom={1}
        proOptions={{ hideAttribution: true }}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag={false}
        zoomOnScroll={false}
        zoomOnPinch={false}
        zoomOnDoubleClick={false}
        preventScrolling={false}
      />
    </div>
  );
}
