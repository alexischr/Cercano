// A port is discovery, never proof of ownership. Fail closed unless ps shows
// this build's agent with this sandbox's inherited environment. Like startAgent,
// allow graceful shutdown before escalating, and recheck identity each time.
import { execFileSync } from "node:child_process";
import { AGENT_BIN } from "./paths.mjs";

export function ownedAgentIdentity(pid, sandbox, exec = execFileSync) {
  if (!Number.isSafeInteger(pid) || pid <= 1) return null;
  try {
    const command = exec("ps", ["-p", String(pid), "-o", "args="], { encoding: "utf8" }).trim();
    if (command !== `${AGENT_BIN} agent`) return null;
    const environment = exec("ps", ["eww", "-p", String(pid), "-o", "command="], { encoding: "utf8" }).trim().split(/\s+/);
    for (const entry of [`HOME=${sandbox.home}`, `TMPDIR=${sandbox.tmp}`, `CERCANO_PORT=${sandbox.port}`, "CERCANO_AUTOLAUNCHED=1"]) {
      if (!environment.includes(entry)) return null;
    }
    return exec("ps", ["-p", String(pid), "-o", "lstart="], { encoding: "utf8" }).trim() || null;
  } catch {
    return null; // exited, inaccessible, or unsupported ps: never guess
  }
}

export async function stopOwnedListener(sandbox, { exec = execFileSync, kill = process.kill, delay = (ms) => new Promise((r) => setTimeout(r, ms)) } = {}) {
  let output;
  try {
    output = exec("lsof", ["-ti", `tcp:${sandbox.port}`, "-sTCP:LISTEN"], { encoding: "utf8" });
  } catch (err) {
    if (err.status === 1) return; // no listener
    throw err; // don't silently hide a broken cleanup tool
  }
  for (const raw of output.trim().split(/\s+/).filter(Boolean)) {
    const pid = Number(raw);
    const identity = ownedAgentIdentity(pid, sandbox, exec);
    if (!identity) continue;
    const signal = (name) => {
      if (ownedAgentIdentity(pid, sandbox, exec) !== identity) return false;
      try { kill(pid, name); } catch (err) { if (err.code !== "ESRCH") throw err; }
      return true;
    };
    if (!signal("SIGTERM")) continue;
    for (let i = 0; i < 50 && ownedAgentIdentity(pid, sandbox, exec) === identity; i++) await delay(100);
    signal("SIGKILL");
  }
}
