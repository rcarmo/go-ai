import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { CodemodeSandbox } from "@earendil-works/pi-codemode";

// Replay the exact guest script from the pinned namespaced-globals case with
// its original expectations. Use the public package entry and default worker.
const source = await readFile(new URL("./source-test.ts", import.meta.url), "utf8");
const selected = source.split("\n").slice(505, 523).join("\n");
assert.match(selected, /it\("groups namespaced globals and spreads arguments on request"/);
const script = selected.match(/const result = await sandbox\.execute\(`([\s\S]*?)`\);/)?.[1];
assert.notEqual(script, undefined);
const worker = "node_modules/@earendil-works/pi-codemode/dist/runtime/worker.js";
assert.equal(import.meta.resolve("@earendil-works/pi-codemode/worker"), new URL(worker, import.meta.url).href);

const seen = [];
const sandbox = new CodemodeSandbox({
	globals: [
		{ name: "models.list", spread: true, execute: args => void seen.push(args) },
		{ name: "models.first", execute: args => args },
	],
});
try {
	const result = await sandbox.execute(script);
	assert.equal(result.ok, true);
	assert.deepEqual(result.value, [["list", "first"], "a", false]);
	assert.deepEqual(seen, [["classifier", null, 3], []]);
	console.log(JSON.stringify({ worker, value: result.value, spreadCalls: seen }));
} finally {
	await sandbox.close();
}
