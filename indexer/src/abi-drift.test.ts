import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// config.yaml carries hand-written ABI signatures. `envio codegen` validates
// that they PARSE, never that they still describe the deployed contract, so a
// renamed event or a widened integer changes topic0 and the indexer silently
// matches nothing, with no error. This test is the missing half of codegen:
// every event in config.yaml must be declared, byte for byte after
// normalisation, by the Solidity that emits it.

const root = (p: string) => fileURLToPath(new URL(`../../contracts/${p}`, import.meta.url));

// SweemVault emits no deposit/withdraw events of its own: it is an ERC-4626 and
// inherits OpenZeppelin's, so its ground truth spans two files.
const SOURCES: Record<string, string[]> = {
  SweemVault: ["src/SweemVault.sol", "lib/openzeppelin-contracts/contracts/interfaces/IERC4626.sol"],
  MarketRegistry: ["src/MarketRegistry.sol"],
  IndexOracle: ["src/IndexOracle.sol"],
  IndexPerpEngine: ["src/IndexPerpEngine.sol"],
};

// third-party contracts with no source in this repo. anything new landing here
// is unguarded, so the list is asserted exactly rather than skipped silently.
const UNCHECKED = ["KuruOrderBook"];

const TYPE_SOURCES = [...new Set(Object.values(SOURCES).flat())];

const stripComments = (s: string) =>
  s.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/\/\/[^\n]*/g, " ");

type Param = { type: string; indexed: boolean; name: string };

// splits a parameter list on commas that are not inside a tuple
function splitParams(s: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let cur = "";
  for (const ch of s) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === "," && depth === 0) {
      out.push(cur);
      cur = "";
      continue;
    }
    cur += ch;
  }
  if (cur.trim()) out.push(cur);
  return out.map((p) => p.trim()).filter(Boolean);
}

// `(address a, int256 b) params` -> type `(address a, int256 b)`, name `params`
function parseParam(raw: string): Param {
  const s = raw.trim().replace(/\s+/g, " ");
  let type: string;
  let rest: string;
  if (s.startsWith("(")) {
    let depth = 0;
    let end = -1;
    for (let i = 0; i < s.length; i++) {
      if (s[i] === "(") depth++;
      else if (s[i] === ")" && --depth === 0) {
        end = i;
        break;
      }
    }
    if (end < 0) throw new Error(`unbalanced tuple in parameter: ${raw}`);
    type = s.slice(0, end + 1);
    rest = s.slice(end + 1);
  } else {
    const [head, ...tail] = s.split(" ");
    type = head!;
    rest = tail.join(" ");
  }
  const words = rest.split(" ").filter(Boolean);
  const indexed = words[0] === "indexed";
  return { type, indexed, name: (indexed ? words[1] : words[0]) ?? "" };
}

const ELEMENTARY =
  /^(address|bool|string|bytes|bytes([1-9]|[12][0-9]|3[0-2])|u?int(8|16|24|32|40|48|56|64|72|80|88|96|104|112|120|128|136|144|152|160|168|176|184|192|200|208|216|224|232|240|248|256)?)$/;

type Universe = { structs: Map<string, Param[]>; contracts: Set<string> };

function buildUniverse(files: string[]): Universe {
  const structs = new Map<string, Param[]>();
  const contracts = new Set<string>();
  for (const f of files) {
    const src = stripComments(readFileSync(root(f), "utf8"));
    for (const m of src.matchAll(/\bstruct\s+(\w+)\s*\{([^{}]*)\}/g)) {
      const fields = m[2]!
        .split(";")
        .map((x) => x.trim())
        .filter(Boolean)
        .map(parseParam);
      structs.set(m[1]!, fields);
    }
    for (const m of src.matchAll(/\b(?:contract|interface|library)\s+(\w+)/g)) {
      contracts.add(m[1]!);
    }
  }
  return { structs, contracts };
}

// contract and interface types are `address`; structs expand to a named tuple,
// which is exactly how config.yaml has to spell MarketParams
function abiType(type: string, u: Universe, seen: string[] = []): string {
  const arr = /^(.*?)(\[\d*\])$/.exec(type);
  if (arr) return abiType(arr[1]!, u, seen) + arr[2]!;
  if (type === "uint") return "uint256";
  if (type === "int") return "int256";
  if (ELEMENTARY.test(type)) return type;
  if (type.startsWith("(")) {
    return `(${splitParams(type.slice(1, -1))
      .map((p) => canonParam(parseParam(p), u, seen))
      .join(", ")})`;
  }
  if (u.structs.has(type)) {
    if (seen.includes(type)) throw new Error(`recursive struct ${type}`);
    return `(${u.structs
      .get(type)!
      .map((f) => canonParam(f, u, [...seen, type]))
      .join(", ")})`;
  }
  if (u.contracts.has(type)) return "address";
  throw new Error(
    `unresolvable type "${type}". Add its source file to SOURCES/TYPE_SOURCES, ` +
      `or expand it by hand in config.yaml.`,
  );
}

// tuple members carry no `indexed`, so it is dropped inside expansions
function canonParam(p: Param, u: Universe, seen: string[] = []): string {
  const t = abiType(p.type, u, seen);
  return seen.length > 0 ? `${t} ${p.name}` : `${t}${p.indexed ? " indexed" : ""} ${p.name}`;
}

function canonSignature(name: string, params: string, u: Universe): string {
  const inner = splitParams(params)
    .map((p) => canonParam(parseParam(p), u))
    .join(", ");
  return `${name}(${inner})`;
}

function solidityEvents(files: string[], u: Universe): Map<string, string[]> {
  const byName = new Map<string, string[]>();
  for (const f of files) {
    const src = stripComments(readFileSync(root(f), "utf8"));
    for (const m of src.matchAll(/\bevent\s+(\w+)\s*\(([\s\S]*?)\)\s*(?:anonymous\s*)?;/g)) {
      const sig = canonSignature(m[1]!, m[2]!, u);
      byName.set(m[1]!, [...(byName.get(m[1]!) ?? []), sig]);
    }
  }
  return byName;
}

// config.yaml read as text: the `event:` lines are all this test needs
function configEvents(): Map<string, string[]> {
  const yaml = readFileSync(fileURLToPath(new URL("../config.yaml", import.meta.url)), "utf8");
  const out = new Map<string, string[]>();
  let contract: string | null = null;
  for (const line of yaml.split("\n")) {
    const c = /^\s{2}- name:\s*(\w+)\s*$/.exec(line);
    if (c) {
      contract = c[1]!;
      out.set(contract, []);
      continue;
    }
    const e = /^\s+- event:\s*"(.+)"\s*$/.exec(line);
    if (e && contract) out.get(contract)!.push(e[1]!);
  }
  return out;
}

const universe = buildUniverse(TYPE_SOURCES);
const config = configEvents();

describe("config.yaml ABI does not drift from contracts/src", () => {
  it("parses every contract block out of config.yaml", () => {
    expect([...config.keys()].sort()).toEqual([...Object.keys(SOURCES), ...UNCHECKED].sort());
    for (const [name, events] of config) expect(events.length, name).toBeGreaterThan(0);
  });

  for (const [contract, files] of Object.entries(SOURCES)) {
    describe(contract, () => {
      const declared = solidityEvents(files, universe);

      for (const raw of config.get(contract) ?? []) {
        const m = /^(\w+)\((.*)\)$/s.exec(raw);
        it(`${m?.[1] ?? raw} matches its Solidity declaration`, () => {
          expect(m, `unparseable config event: ${raw}`).not.toBeNull();
          const [, name, params] = m!;
          const candidates = declared.get(name!);
          expect(
            candidates,
            `config.yaml binds ${contract}.${name}, but no such event is declared in ` +
              `${files.join(", ")}. Declared: ${[...declared.keys()].join(", ")}`,
          ).toBeDefined();
          expect(candidates).toContain(canonSignature(name!, params!, universe));
        });
      }
    });
  }
});
