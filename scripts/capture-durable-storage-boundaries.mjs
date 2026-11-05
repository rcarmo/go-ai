// Capture actual original public Storage boundary behaviour, without native aliases.
// Usage: bun run scripts/capture-durable-storage-boundaries.mjs REFERENCE_ROOT OUTPUT
import { pathToFileURL } from "node:url";
import { readFile, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";

if (process.argv.length !== 4) throw Error("expected reference root and output path");
const reference = resolve(process.argv[2]);
const manifest = JSON.parse(await readFile(join(reference, "node_modules/@earendil-works/pi-durable/package.json"), "utf8"));
if (manifest.name !== "@earendil-works/pi-durable" || manifest.version !== "1.5.0") throw Error("wrong reference package");
const d = await import(pathToFileURL(join(reference, "node_modules/@earendil-works/pi-durable/dist/index.js")));
const { BACKGROUND_CONTEXT: context } = await import(pathToFileURL(join(reference, "node_modules/@earendil-works/chord/dist/context/index.js")));
const storage = new d.MemoryStorage();
const receipts = [];
const receive = (index, seq) => receipts.push({ index, seq });
try {
  const freshRootPresent = (await storage.conversation(1, context)) !== undefined;
  const emptyBatch = await storage.commit([[]], receive, context);
  const conversation = { id: 99 };
  const explicitID = await storage.commit([[{ type: "conversation", value: conversation }]], receive, context);
  const storedConversation = await storage.conversation(99, context);
  const nextMint = await storage.mintId(context);
  const submission = { id: 101, conversationId: 99, type: "continue", status: "unanswered", reason: "" };
  const emptyContinueReason = await storage.commit([[{ type: "submission", value: submission }]], receive, context);
  const storedSubmission = await storage.submission(101, context);
  await writeFile(resolve(process.argv[3]), JSON.stringify({
    reference: { package: manifest.name, version: manifest.version, engine: "MemoryStorage", target: "f9a26365568127461c47a07a37dcb42c140a3ac5" },
    freshRootPresent, emptyBatch, conversation, explicitID, storedConversation,
    nextMint, submission, emptyContinueReason, storedSubmission, receipts,
  }, null, 2) + "\n");
} finally {
  await storage.close(context);
}
