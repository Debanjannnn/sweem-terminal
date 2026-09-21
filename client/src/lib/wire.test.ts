import { expect, test } from "bun:test";
import { fromWad, toUnits, txTier, wadToUnits } from "./api";

test("toUnits scales decimals without float drift", () => {
  expect(toUnits("1.5", 6)).toBe("1500000");
  expect(toUnits("0.000001", 6)).toBe("1");
  expect(toUnits("0.0000001", 6)).toBe("0");
  expect(toUnits("12", 18)).toBe("12000000000000000000");
  expect(toUnits("0", 18)).toBe("0");
});

test("wadToUnits truncates a wad down to a token's decimals", () => {
  expect(wadToUnits("1500000000000000000", 6)).toBe("1500000");
  expect(wadToUnits("1", 6)).toBe("0");
  expect(wadToUnits("2000000000000000000", 18)).toBe("2000000000000000000");
});

test("fromWad reads a wad string as a number and NaN for nothing", () => {
  expect(fromWad("2500000000000000000")).toBe(2.5);
  expect(Number.isNaN(fromWad(undefined))).toBe(true);
});

test("tx tiers follow monad finality, and an error outranks them", () => {
  const base = { intent_key: "k", confirmation: "latest", submitted_at: "" };
  expect(txTier(base)).toBe("seen");
  expect(txTier({ ...base, confirmation: "safe" })).toBe("safe");
  expect(txTier({ ...base, confirmation: "finalized" })).toBe("final");
  expect(txTier({ ...base, confirmation: "finalized", error: "reverted" })).toBe("failed");
  expect(txTier(undefined)).toBe(undefined);
});

test("the socket is shared across a strict-mode double mount", async () => {
  let opened = 0;
  class FakeSocket {
    static OPEN = 1;
    readyState = 0;
    onopen: (() => void) | null = null;
    onmessage: ((e: { data: string }) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;
    sent: string[] = [];
    constructor() {
      opened++;
      queueMicrotask(() => {
        this.readyState = 1;
        this.onopen?.();
      });
    }
    send(s: string) {
      this.sent.push(s);
    }
    close() {
      this.readyState = 3;
      this.onclose?.();
    }
  }
  (globalThis as { WebSocket?: unknown }).WebSocket = FakeSocket;

  const { subscribe } = await import("./ws");
  const drop = subscribe("tick.kuru.mon-usdc", () => {});
  drop(); // strict mode tears the first mount down immediately
  const drop2 = subscribe("tick.kuru.mon-usdc", () => {});
  expect(opened).toBe(1);
  drop2();
});
