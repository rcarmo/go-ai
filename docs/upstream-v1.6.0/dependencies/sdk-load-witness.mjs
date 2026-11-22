import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";
import { registerHooks } from "node:module";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const entry = "node_modules/@google/genai/dist/node/index.mjs";
const target = pathToFileURL(join(process.env.SDK_LOAD_ROOT, entry)).href;
registerHooks({
	load(url, context, next) {
		const loaded = next(url, context);
		if (url === target) {
			if (loaded.format !== "module" || loaded.source == null) throw Error("SDK loader did not expose ESM source bytes");
			const bytes = Buffer.from(loaded.source);
			appendFileSync(process.env.SDK_LOAD_TRACE, JSON.stringify({
				entry, format: loaded.format, bytes: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex"),
			}) + "\n");
		}
		return loaded;
	},
});
