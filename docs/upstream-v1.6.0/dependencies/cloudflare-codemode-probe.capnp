using Workerd = import "workerd.capnp";

const config :Workerd.Config = (
  services = [
    (name = "agent", worker = (
      compatibilityDate = "2026-04-22",
      modules = [(name = "agent.mjs", esModule = embed "agent.mjs")],
      bindings = [
        (name = "SESSION", durableObjectNamespace = "Session"),
        (name = "CODEMODE_SANDBOX", durableObjectNamespace = (className = "CodemodeSandbox", serviceName = "sandbox")),
      ],
      durableObjectNamespaces = [(className = "Session", uniqueKey = "pi-codemode-agent-evidence", enableSql = true)],
      durableObjectStorage = (inMemory = void),
    )),
    (name = "sandbox", worker = (
      compatibilityDate = "2026-04-22",
      modules = [
        (name = "sandbox.mjs", esModule = embed "sandbox.mjs"),
        (name = "quickjs-wasi/quickjs.wasm", wasm = embed "quickjs.wasm"),
      ],
      durableObjectNamespaces = [(className = "CodemodeSandbox", uniqueKey = "pi-codemode-sandbox-evidence", enableSql = true)],
      durableObjectStorage = (inMemory = void),
    )),
    (name = "probe", worker = (
      compatibilityDate = "2026-04-22",
      modules = [(name = "test.mjs", esModule = embed "test.mjs")],
      bindings = [
        (name = "AGENT", service = "agent"),
        (name = "MODULE", service = (name = "sandbox", entrypoint = "ModuleWitness")),
        (name = "SCRIPT", text = embed "request.txt"),
      ],
    )),
  ],
);
