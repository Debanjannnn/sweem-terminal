"use client";

import { useEffect, useState } from "react";
import { WS_URL } from "./config";

// One shared socket; topics refcounted by listener sets; resubscribes after reconnect.
type Listener = (data: unknown) => void;
const topics = new Map<string, Set<Listener>>();
let ws: WebSocket | null = null;
let timer: ReturnType<typeof setTimeout> | null = null;
let attempt = 0;

function send(op: "sub" | "unsub", list: string[]) {
  if (ws?.readyState === WebSocket.OPEN && list.length) ws.send(JSON.stringify({ op, topics: list }));
}

function connect() {
  timer = null;
  const sock = new WebSocket(WS_URL);
  ws = sock;
  sock.onopen = () => {
    attempt = 0;
    send("sub", [...topics.keys()]);
  };
  sock.onmessage = (e) => {
    try {
      const m = JSON.parse(e.data as string) as { topic?: string; data?: unknown };
      if (m.topic) topics.get(m.topic)?.forEach((fn) => fn(m.data));
    } catch {}
  };
  sock.onclose = () => {
    if (ws !== sock) return;
    ws = null;
    if (topics.size) timer = setTimeout(connect, Math.min(1000 * 2 ** attempt++, 15000));
  };
}

export function subscribe(topic: string, fn: Listener): () => void {
  let set = topics.get(topic);
  if (!set) {
    set = new Set();
    topics.set(topic, set);
    if (!ws && !timer) connect();
    else send("sub", [topic]);
  }
  set.add(fn);
  return () => {
    set.delete(fn);
    if (set.size === 0 && topics.get(topic) === set) {
      topics.delete(topic);
      send("unsub", [topic]);
    }
  };
}

// topic e.g. `nav.${basket}`, `book.${market}`, `trades.${market}` (lowercase addrs); null = off
export function useTopic<T>(raw: string | null): T | null {
  const topic = raw?.toLowerCase() ?? null;
  const [last, setLast] = useState<{ topic: string; data: T } | null>(null);
  useEffect(() => {
    if (!topic) return;
    return subscribe(topic, (data) => setLast({ topic, data: data as T }));
  }, [topic]);
  return last && last.topic === topic ? last.data : null;
}
