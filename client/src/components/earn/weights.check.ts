// bun run src/components/earn/weights.check.ts
import assert from "node:assert/strict";
import { evenSplit, setWeight, weightsError } from "./weights";

assert.deepEqual(evenSplit(3), [3334, 3333, 3333]);
assert.equal(evenSplit(7).reduce((a, b) => a + b), 10000);
assert.equal(weightsError(evenSplit(3)), null);
assert.match(weightsError([5000, 4000]) ?? "", /90\.00%/);
assert.ok(weightsError([10000, 0]));
assert.ok(weightsError([]));
assert.deepEqual(setWeight([6550, 0], 1, 5000), [6550, 3450]);
assert.deepEqual(setWeight([6550, 3450], 0, 2000), [2000, 3450]);
assert.deepEqual(setWeight([10000, 0, 0], 2, 100), [10000, 0, 0]);
console.log("ok");
