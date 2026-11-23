#!/usr/bin/env bash
# The kernel, not a fetch shim, restricts the Vitest worker and its children.
# Configure and check the fresh namespace before dropping all capabilities.
prepare_offline_loopback() {
  local scratch=$1
  cat > "$scratch/in-netns.sh" <<'BASH'
set -euo pipefail
[[ $(readlink /proc/self/ns/net) != "$1" ]]
shift
ip -j link show | jq -e '[.[].ifname]==["lo"]' > /dev/null
ip link set lo up
for family in -4 -6; do
  ip "$family" -j route show table all | jq -e 'all(.[]; .dev=="lo")' > /dev/null
done
exec setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs "$@"
BASH
  cat > "$scratch/check-netns.mjs" <<'JS'
import assert from "node:assert/strict";
import { createConnection, createServer } from "node:net";
import { once } from "node:events";

const server = createServer(socket => socket.end("loopback ready"));
server.listen(0, "127.0.0.1");
await once(server, "listening");
try {
  const socket = createConnection({ host: "127.0.0.1", port: server.address().port });
  let body = "";
  for await (const chunk of socket) body += chunk;
  assert.equal(body, "loopback ready");
} finally {
  server.close();
}
for (const host of ["192.0.2.1", "2001:db8::1"]) {
  const guard = AbortSignal.timeout(2000);
  const socket = createConnection({ host, port: 443, signal: guard });
  try {
    const [error] = await once(socket, "error", { signal: guard });
    assert.equal(error.code, "ENETUNREACH");
  } finally {
    socket.destroy();
  }
}
JS
}

run_offline_loopback() {
  local scratch=$1 host_netns
  shift
  host_netns=$(readlink /proc/self/ns/net)
  unshare --user --map-root-user --net env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    bash "$scratch/in-netns.sh" "$host_netns" "$@"
}
