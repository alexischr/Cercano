import { test } from "node:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import assert from "node:assert/strict";
import { AGENT_BIN } from "../helpers/paths.mjs";
import { ownedAgentIdentity, stopOwnedListener, stopSandboxProcesses, ownedSandboxIdentity } from "../helpers/owned-agent.mjs";
import { createSandbox } from "../helpers/sandbox.mjs";
import { startAgent, waitForPort, portOpen } from "../helpers/agent.mjs";

const sandbox = { home: "/tmp/suite/home", tmp: "/tmp/suite/t", port: 12345 };
const environment = `${AGENT_BIN} agent HOME=${sandbox.home} TMPDIR=${sandbox.tmp} CERCANO_PORT=${sandbox.port} CERCANO_AUTOLAUNCHED=1`;
function probe({ command = `${AGENT_BIN} agent`, env = environment, start = "Thu Oct 22 10:00:00 2026" } = {}) {
  return (bin, args) => {
    if (bin === "lsof") return "123\n";
    if (args.at(-1) === "args=") return command;
    if (args.at(-1) === "command=") return env;
    if (args.at(-1) === "lstart=") return start;
    throw new Error("unexpected probe");
  };
}

test("cleanup never signals an unrelated listener or partially matching environment", async () => {
  for (const options of [
    { command: "unrelated-server" },
    { env: environment.replace(sandbox.home, `${sandbox.home}-other`) },
    { env: environment.replace(sandbox.tmp, `${sandbox.tmp}-other`) },
    { env: environment.replace("CERCANO_PORT=12345", "CERCANO_PORT=99999") },
    { env: environment.replace("CERCANO_AUTOLAUNCHED=1", "") },
  ]) {
    const signals = [];
    await stopOwnedListener(sandbox, { exec: probe(options), kill: (...args) => signals.push(args) });
    assert.deepEqual(signals, []);
  }
});

test("ownership inspection fails closed", () => {
  assert.equal(ownedAgentIdentity(123, sandbox, () => { throw new Error("ps unavailable"); }), null);
  for (const pid of [0, -1, NaN, 1]) assert.equal(ownedAgentIdentity(pid, sandbox, probe()), null);
});

test("owned cleanup sends TERM before bounded KILL", async () => {
  const signals = [];
  await stopOwnedListener(sandbox, { exec: probe(), kill: (...args) => signals.push(args), delay: async () => {} });
  assert.deepEqual(signals, [[123, "SIGTERM"], [123, "SIGKILL"]]);
});

test("cleanup revalidates identity before escalation", async () => {
  let replaced = false;
  const signals = [];
  await stopOwnedListener(sandbox, {
    exec: (bin, args) => probe({ start: replaced ? "a different process" : "original process" })(bin, args),
    kill: (...args) => { signals.push(args); replaced = true; },
    delay: async () => {},
  });
  assert.deepEqual(signals, [[123, "SIGTERM"]]);
});

test("ownership probes recognize and stop a real sandbox agent", async () => {
  const sb = await createSandbox();
  sb.env.CERCANO_AUTOLAUNCHED = "1";
  let agent;
  try {
    agent = await startAgent(sb);
    assert.ok(ownedAgentIdentity(agent.pid, sb));
    await stopOwnedListener({ ...sb, home: `${sb.home}-other` });
    assert.equal(await portOpen(sb.port), true, "the other sandbox agent must survive cleanup");
    await stopOwnedListener(sb);
    await waitForPort(sb.port, { open: false });
  } finally {
    await agent?.stop();
    await stopSandboxProcesses(sb);
    sb.cleanup();
  }
});

// A listener-only cleanup cannot see pid 123: its port has already closed.
// Its last config flush also launches a host, so cleanup must rescan after exit.
function processTable() {
  const processes = new Map([[123, { mode: "agent", start: "original" }]]);
  const exec = (bin, args) => {
    if (bin !== "ps") throw new Error("listener discovery is not sufficient");
    if (args[0] === "-axo") return [...processes].map(([pid, p]) => `${pid} ${AGENT_BIN} ${p.mode}`).join("\n");
    const p = processes.get(Number(args[args.indexOf("-p") + 1]));
    if (!p) throw new Error("exited");
    switch (args.at(-1)) {
      case "args=": return `${AGENT_BIN} ${p.mode}`;
      case "command=": return `${AGENT_BIN} ${p.mode} HOME=${p.home ?? sandbox.home} TMPDIR=${sandbox.tmp} CERCANO_PORT=${sandbox.port}`;
      case "stat=": return p.state ?? "S";
      case "lstart=": return p.start;
      default: throw new Error("unexpected ps");
    }
  };
  return { processes, exec };
}

test("sandbox teardown finds closed-port agents and rescans detached hosts after their final write", async () => {
  const { processes, exec } = processTable();
  const signals = [];
  let finalWrite = false;
  processes.set(999, { mode: "model-host", start: "unrelated", home: `${sandbox.home}-other` });
  await stopSandboxProcesses(sandbox, {
    exec,
    kill: (pid, signal) => { signals.push([pid, signal]); },
    delay: async () => {
      if (processes.has(123)) {
        finalWrite = true;
        processes.delete(123);
        processes.set(456, { mode: "model-host", start: "new host" });
      } else processes.delete(456);
    },
  });
  assert.equal(finalWrite, true);
  assert.deepEqual(signals, [[123, "SIGTERM"], [456, "SIGTERM"]]);
  assert.equal(processes.has(999), true, "unrelated live host survives");
});

test("sandbox teardown rechecks identity before escalation and waits for KILL exit", async () => {
  for (const replaced of [true, false]) {
    const { processes, exec } = processTable();
    const signals = [];
    let killed = false;
    await stopSandboxProcesses(sandbox, {
      exec,
      kill: (pid, signal) => {
        signals.push([pid, signal]);
        if (signal === "SIGTERM" && replaced) processes.set(pid, { mode: "unrelated", start: "reused" });
        if (signal === "SIGKILL") killed = true;
      },
      delay: async () => { if (killed) processes.delete(123); },
    });
    assert.deepEqual(signals, replaced ? [[123, "SIGTERM"]] : [[123, "SIGTERM"], [123, "SIGKILL"]]);
    if (!replaced) assert.equal(processes.has(123), false);
  }
});

test("sandbox teardown refuses file removal if an owned process will not exit", async () => {
  const { exec } = processTable();
  await assert.rejects(stopSandboxProcesses(sandbox, { exec, kill: () => {}, delay: async () => {} }), /did not exit; preserving/);
});

test("real sandbox teardown stops detached host but preserves another live sandbox", async () => {
  const a = await createSandbox();
  const b = await createSandbox();
  let first, second;
  try {
    first = await startAgent(a);
    second = await startAgent(b);
    const host = (sb) => JSON.parse(readFileSync(join(sb.home, ".cercano/run/modelhost/modelhost.json"), "utf8")).pid;
    const aHost = host(a), bHost = host(b);
    assert.ok(ownedSandboxIdentity(aHost, a), "first detached host is owned");
    assert.ok(ownedSandboxIdentity(bHost, b), "second detached host is owned");
    await stopSandboxProcesses(a);
    assert.equal(ownedSandboxIdentity(aHost, a), null);
    assert.ok(ownedSandboxIdentity(bHost, b), "unrelated detached host survives");
    assert.equal(await portOpen(a.port), false);
    assert.equal(await portOpen(b.port), true);
    assert.ok(ownedSandboxIdentity(second.pid, b));
  } finally {
    await first?.stop();
    await second?.stop();
    await stopSandboxProcesses(a);
    await stopSandboxProcesses(b);
    a.cleanup();
    b.cleanup();
  }
});

test("sandbox process ownership fails closed for partial environments and unexpected commands", () => {
  for (const options of [
    { command: `${AGENT_BIN} model-host --unknown` },
    { command: `${AGENT_BIN} worker --socket ${sandbox.tmp}-other/socket` },
    { env: environment.replace(sandbox.home, `${sandbox.home}-other`) },
    { env: environment.replace(sandbox.tmp, `${sandbox.tmp}-other`) },
    { env: environment.replace("CERCANO_PORT=12345", "CERCANO_PORT=99999") },
  ]) assert.equal(ownedSandboxIdentity(123, sandbox, probe(options)), null);
  assert.equal(ownedSandboxIdentity(123, sandbox, () => { throw new Error("inspection denied"); }), null);
  const { exec } = processTable();
  assert.ok(ownedSandboxIdentity(123, sandbox, exec)); // explicit agent, no auto-launch flag
});
