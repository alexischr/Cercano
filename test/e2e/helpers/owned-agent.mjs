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

// Unlike listener discovery, this also finds an agent that has closed its port
// but is still flushing config/SQLite, and the deliberately detached model host.
export function ownedSandboxIdentity(pid, sandbox, exec = execFileSync) {
  if (!Number.isSafeInteger(pid) || pid <= 1) return null;
  try {
    const command = exec("ps", ["-p", String(pid), "-o", "args="], { encoding: "utf8" }).trim();
    const worker = `${AGENT_BIN} worker --socket ${sandbox.tmp}/`;
    if (command !== `${AGENT_BIN} agent` && command !== `${AGENT_BIN} model-host` && !command.startsWith(worker)) return null;
    const env = exec("ps", ["eww", "-p", String(pid), "-o", "command="], { encoding: "utf8" }).trim().split(/\s+/);
    if (![ `HOME=${sandbox.home}`, `TMPDIR=${sandbox.tmp}`, `CERCANO_PORT=${sandbox.port}` ].every((entry) => env.includes(entry))) return null;
    const state = exec("ps", ["-p", String(pid), "-o", "stat="], { encoding: "utf8" }).trim();
    if (!state || state.startsWith("Z")) return null; // exited, awaiting reaping
    const started = exec("ps", ["-p", String(pid), "-o", "lstart="], { encoding: "utf8" }).trim();
    return started ? `${command}\n${started}` : null;
  } catch {
    return null; // no ownership evidence: never signal
  }
}

export async function stopSandboxProcesses(sandbox, {
  exec = execFileSync, kill = process.kill,
  delay = (ms) => new Promise((r) => setTimeout(r, ms)),
} = {}) {
  const identity = (pid) => ownedSandboxIdentity(pid, sandbox, exec);
  const discover = () => exec("ps", ["-axo", "pid=,args="], { encoding: "utf8" }).split("\n")
    .filter((line) => line.includes(`${AGENT_BIN} `))
    .map((line) => Number(line.trim().split(/\s+/)[0]))
    .map((pid) => ({ pid, id: identity(pid) })).filter(({ id }) => id);
  const stop = async ({ pid, id }) => {
    const signal = (name) => {
      if (identity(pid) !== id) return;
      try { kill(pid, name); } catch (err) { if (err.code !== "ESRCH") throw err; }
    };
    signal("SIGTERM");
    for (let i = 0; i < 50 && identity(pid) === id; i++) await delay(100);
    if (identity(pid) !== id) return;
    signal("SIGKILL");
    // A successful kill is not an exit acknowledgement. Do not remove files
    // while a process can still write (including after escalation).
    for (let i = 0; i < 50 && identity(pid) === id; i++) await delay(100);
    if (identity(pid) === id) throw new Error(`sandbox process ${pid} did not exit; preserving ${sandbox.root}`);
  };
  // Stop launchers first. Rescan only after they have exited so a host/worker
  // spawned during shutdown cannot escape cleanup. Never signal a whole group.
  for (const proc of discover().filter(({ id }) => id.startsWith(`${AGENT_BIN} agent\n`))) await stop(proc);
  for (const proc of discover()) await stop(proc);
  if (discover().length) throw new Error(`sandbox processes remain; preserving ${sandbox.root}`);
}
