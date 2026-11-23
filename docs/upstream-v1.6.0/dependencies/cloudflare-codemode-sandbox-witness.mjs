import { WorkerEntrypoint } from "cloudflare:workers";
import { cloudflareQuickJSWasm } from "@earendil-works/pi-codemode/cloudflare";
import quickjs from "quickjs-wasi/quickjs.wasm";
export { default, CodemodeSandbox } from "./example/sandbox/src/index.ts";

// Report the module used by the unchanged sandbox class without changing it.
export class ModuleWitness extends WorkerEntrypoint {
	inspect() {
		if (cloudflareQuickJSWasm !== quickjs || !(quickjs instanceof WebAssembly.Module)) {
			throw Error("The sandbox did not receive the compiled QuickJS module");
		}
		return {
			exports: WebAssembly.Module.exports(quickjs).length,
			auditSections: WebAssembly.Module.customSections(quickjs, "audit").map(bytes => [...new Uint8Array(bytes)]),
		};
	}
}
