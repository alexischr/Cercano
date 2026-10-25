// Starts `cercano agent` for a sandbox so specs control its lifetime and can
// read its log, rather than relying on the CLI's auto-launch.
import { spawn } from "node:child_process";
import { openSync, closeSync, readFileSync } from "node:fs";
import { connect } from "node:net";
import { join } from "node:path";
import { AGENT_BIN } from "./paths.mjs";

export function portOpen(port) {
  return new Promise((resolve) => {
    const sock = connect({ port, host: "127.0.0.1" });
    sock.once("connect", () => {
      sock.destroy();
      resolve(true);
    });
    sock.once("error", () => resolve(false));
  });
}

export async function waitForPort(port, { open = true, timeout = 15_000 } = {}) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if ((await portOpen(port)) === open) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`port ${port} still ${open ? "closed" : "open"} after ${timeout}ms`);
}

export async function startAgent(sandbox) {
  const logPath = join(sandbox.root, "agent.log");
  const fd = openSync(logPath, "a");
  // Keep the agent independent of the test runner. Sandbox teardown verifies
  // and stops workers and detached model hosts separately.
  const child = spawn(AGENT_BIN, ["agent"], {
    env: { ...process.env, ...sandbox.env },
    stdio: ["ignore", fd, fd],
    detached: true,
  });
  closeSync(fd);

  let exited = null;
  child.once("exit", (code, signal) => (exited = { code, signal }));

  const log = () => {
    try {
      return readFileSync(logPath, "utf8");
    } catch {
      return "";
    }
  };

  try {
    await waitForPort(sandbox.port);
  } catch (err) {
    if (!exited) {
      const done = new Promise((resolve) => child.once("exit", resolve));
      child.kill("SIGKILL");
      await done;
    }
    throw new Error(`agent did not listen on ${sandbox.port} (exit=${JSON.stringify(exited)}):\n${log()}`, { cause: err });
  }

  return {
    pid: child.pid,
    logPath,
    log,
    async stop() {
      if (exited) return;
      const done = new Promise((r) => child.once("exit", r));
      child.kill("SIGTERM");
      const timer = setTimeout(() => child.kill("SIGKILL"), 5_000);
      await done;
      clearTimeout(timer);
    },
  };
}
