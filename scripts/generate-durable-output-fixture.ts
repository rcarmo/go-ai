// Generate exact decoded snapshots using the pinned upstream runtime module.
// Usage: bun scripts/generate-durable-output-fixture.ts /path/to/pi-mono-v100-exact
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
const revision = "a13d35a742c6ef8462812a28fbe1d8c8b7431c32";
const checkout = path.resolve(process.argv[2] ?? "");
if (!process.argv[2] || execFileSync("git", ["-C", checkout, "rev-parse", "HEAD"], { encoding: "utf8" }).trim() !== revision) {
  throw new Error("Expected pinned pi-durable 1.0.0 checkout " + revision);
}
const relative = "packages/durable/src/harness/output.ts";
const source = fs.readFileSync(path.join(checkout, relative), "utf8");
const pinned = execFileSync("git", ["-C", checkout, "show", revision + ":" + relative], { encoding: "utf8" });
if (source !== pinned) throw new Error("Upstream output module has local edits");
const { boundOutput, OutputBuffer } = await import(path.join(checkout, relative));
const cases = [];
let seed = 0x12345678;
const random = () => { seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0; return seed; };
for (const retain of ["head", "tail"] as const) for (const maxBytes of [0, 1, 3, 5, 9, 17, 40]) for (const maxLines of [0, 1, 2, 5]) {
  const limits = { maxBytes, maxLines, retain };
  const buffer = new OutputBuffer(limits);
  const chunks = [];
  let text = "";
  for (let i = 0; i < 20; i++) {
    let chunk = "";
    const alphabet = ["a", "é", "中", "🙂", "\n", "\t", "\u0007", "\r"];
    for (let j = 0, n = random() % 8; j < n; j++) chunk += alphabet[random() % alphabet.length];
    text += chunk;
    buffer.push(chunk);
    chunks.push({ input: chunk, ...buffer.snapshot() });
  }
  cases.push({ limits, chunks, bounded: boundOutput(text, limits) });
}
const byteCases=[];
for(const limits of [{maxBytes:30,maxLines:3,retain:"head"},{maxBytes:9,maxLines:2,retain:"tail"}]){
  for(const inputs of [
    [[0xf0,0x9f],[0x98,0x80,10],"x",[0xe2,0x82]],
    [[0xe2],"x",[0xe2,0x82]],
    [[0xef],[0xbb,0xbf,97],[0xef,0xbb,0xbf]],
    [[0xe0,0x80,0x80],[0xed,0xa0,0x80],[0xf4,0x90,0x80,0x80]],
    [[0xe2,0x82,97],[0xc0,0xaf],[0xf0,0x9f,0x98,97]],
    [[0xc2],[0x80],[0xf0,0x9f,0x98,0x80],"text\nmore\n"]
  ]){const buffer=new OutputBuffer(limits);const chunks=[];for(const input of inputs){buffer.push(typeof input==="string"?input:new Uint8Array(input));chunks.push({input,...buffer.snapshot()})};buffer.end();byteCases.push({limits,chunks,ended:buffer.snapshot()})}
}
const output = path.join(import.meta.dir, "../durable/testdata/output-reference.json");
fs.writeFileSync(output, JSON.stringify({ source: relative, revision, cases,byteCases }));
console.log(`${cases.length} cases, ${cases.length * 20} pinned snapshots -> ${output}`);
