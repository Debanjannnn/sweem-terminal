"use client";

import { useEffect, useState } from "react";
import { MARKET_WS_URL } from "./api";

export type Frame<T> = {
  topic: string;
  type: "snapshot" | "update" | "error";
  ts: number;
  data: T;
};

type Listener = (f: Frame<unknown>) => void;

const listeners = new Map<string, Set<Listener>>();
const lastFrame = new Map<string, Frame<unknown>>();
const pendingDrop = new Map<string, ReturnType<typeof setTimeout>>();

let socket: WebSocket | null = null;
let backoff = 500;
let reconnect: ReturnType<typeof setTimeout> | null = null;
let idleClose: ReturnType<typeof setTimeout> | null = null;

// strict mode mounts every effect twice; the socket is module state and a drop
// is deferred, so the second mount reuses the first mount's connection.
const GRACE_MS = 1000;

function send(op: "subscribe" | "unsubscribe", topics: string[]) {
  if (!topics.length || socket?.readyState !== WebSocket.OPEN) return;
  socket.send(JSON.stringify({ op, topics }));
}

function open() {
  if (idleClose) {
    clearTimeout(idleClose);
    idleClose = null;
  }
  if (socket && socket.readyState <= WebSocket.OPEN) return;
  socket = new WebSocket(MARKET_WS_URL);
  socket.onopen = () => {
    backoff = 500;
    send("subscribe", [...listeners.keys()]);
  };
  socket.onmessage = (e) => {
    let f: Frame<unknown>;
    try {
      f = JSON.parse(e.data as string) as Frame<unknown>;
    } catch {
      return;
    }
    if (!f.topic) return;
    lastFrame.set(f.topic, f);
    listeners.get(f.topic)?.forEach((fn) => fn(f));
  };
  socket.onclose = () => {
    socket = null;
    if (!listeners.size || reconnect) return;
    reconnect = setTimeout(() => {
      reconnect = null;
      open();
    }, backoff);
    backoff = Math.min(backoff * 2, 15_000);
  };
  socket.onerror = () => socket?.close();
}

export function subscribe(topic: string, fn: Listener): () => void {
  const drop = pendingDrop.get(topic);
  if (drop) {
    clearTimeout(drop);
    pendingDrop.delete(topic);
  }
  let set = listeners.get(topic);
  if (!set) {
    set = new Set();
    listeners.set(topic, set);
    send("subscribe", [topic]);
  }
  set.add(fn);
  open();
  // the hub replays the last frame per topic, but a second local subscriber
  // would wait for the next one without this
  const cached = lastFrame.get(topic);
  if (cached) fn(cached);

  return () => {
    set.delete(fn);
    if (set.size) return;
    pendingDrop.set(
      topic,
      setTimeout(() => {
        pendingDrop.delete(topic);
        if (listeners.get(topic)?.size) return;
        listeners.delete(topic);
        lastFrame.delete(topic);
        send("unsubscribe", [topic]);
        if (!listeners.size && !idleClose) {
          idleClose = setTimeout(() => {
            idleClose = null;
            if (listeners.size) return;
            socket?.close();
            socket = null;
          }, GRACE_MS);
        }
      }, GRACE_MS),
    );
  };
}

export function useTopic<T>(topic: string | null) {
  const [state, setState] = useState<{
    topic: string | null;
    data: T | null;
    error: string | null;
  }>({ topic, data: null, error: null });

  useEffect(() => {
    if (!topic) {
      setState({ topic: null, data: null, error: null });
      return;
    }
    return subscribe(topic, (f) => {
      if (f.type === "error") {
        setState({ topic, data: null, error: String(f.data) });
        return;
      }
      setState({ topic, data: f.data as T, error: null });
    });
  }, [topic]);

  // a stale frame from the previous topic must never read as this one's
  return state.topic === topic
    ? { data: state.data, error: state.error }
    : { data: null, error: null };
}
