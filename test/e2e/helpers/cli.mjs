// withCli wires one spec end to end: sandbox, optional agent and fake Ollama,
// and cercano-cli running in a tui-test PTY. Teardown always runs; on failure
// it keeps a tui-test trace under artifacts/ and prints the agent log.
import { join } from "node:path";
import { TuiTest } from "@microsoft/tui-test";
import { createSandbox } from "./sandbox.mjs";
import { stopSandboxProcesses } from "./owned-agent.mjs";
import { startAgent } from "./agent.mjs";
import { startFakeOllama } from "./fake-ollama.mjs";
import { ARTIFACTS_DIR, CLI_BIN } from "./paths.mjs";

export const PLACEHOLDER = "type a message, /help for commands";

export async function withCli(
  t,
  { args = [], agent = true, seedConfig = true, ollama = false, cols = 120, rows = 40, bin = CLI_BIN, env = {} } = {},
  fn,
) {
  const fake = ollama ? await startFakeOllama(typeof ollama === "object" ? ollama : {}) : null;
  const sandbox = await createSandbox({ seedConfig, ollamaUrl: fake?.url });
  Object.assign(sandbox.env, env);
  const name = t.name.replace(/[^\w-]+/g, "_");
  const term = TuiTest.ephemeral(`cercano-${name}`, {
    timeouts: { text: 15_000 },
    trace: { mode: "on-failure", directory: join(ARTIFACTS_DIR, "traces") },
    artifacts: { dir: join(ARTIFACTS_DIR, "failures", name), onFailure: "text" },
  });

  let server = null;
  let failed = false;
  try {
    if (agent) server = await startAgent(sandbox);
    await term.run(bin, args, { cols, rows, env: sandbox.env, cwd: sandbox.root });
    return await fn({ term, sandbox, agent: server, ollama: fake });
  } catch (err) {
    failed = true;
    const screen = await term.text().catch(() => "<no screen>");
    t.diagnostic(`screen at failure:\n${screen}`);
    if (server) t.diagnostic(`agent log:\n${server.log()}`);
    if (fake) t.diagnostic(`fake ollama requests:\n${JSON.stringify(fake.requests.map((r) => r.path))}`);
    throw err;
  } finally {
    await term.close({ failed }).catch(() => {});
    try {
      await server?.stop();
      // A closed listener is not process exit: shutdown can still flush config.
      // Also stop detached hosts before deleting any sandbox files.
      await stopSandboxProcesses(sandbox);
    } finally {
      await fake?.close();
    }
    sandbox.cleanup();
  }
}

// exitCode waits for the program to exit and returns its status.
export async function exitCode(term, timeout = 10_000) {
  await term.waitExit({ timeout });
  return (await term.state()).exited;
}

// ready waits until the chat prompt is idle and accepting input.
export async function ready(term) {
  await term.getByText(PLACEHOLDER).expect();
}
