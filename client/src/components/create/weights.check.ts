// bun run src/components/create/weights.check.ts
import assert from "node:assert/strict";
import { evenSplit, pctToBps, weightsError } from "./weights";

assert.deepEqual(evenSplit(3), [3334, 3333, 3333]);
assert.equal(evenSplit(7).reduce((a, b) => a + b), 10000);
assert.equal(pctToBps("12.5"), 1250);
assert.equal(pctToBps("100"), 10000);
assert.equal(pctToBps("100.01"), null);
assert.equal(pctToBps("1.234"), null);
assert.equal(pctToBps("abc"), null);
assert.equal(weightsError(evenSplit(3)), null);
assert.match(weightsError([5000, 4000]) ?? "", /90\.00%/);
assert.ok(weightsError([10000, 0]));
assert.ok(weightsError([]));
console.log("ok");
