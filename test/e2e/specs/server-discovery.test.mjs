import { test } from "node:test";
import assert from "node:assert/strict";
import { copyFileSync, readFileSync, rmSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { withCli, exitCode, ready } from "../helpers/cli.mjs";
import { waitForPort, portOpen } from "../helpers/agent.mjs";
import { CLI_BIN } from "../helpers/paths.mjs";

test("without a reachable or launchable agent the CLI explains and exits 1", async (t) => {
  // A lone copy of the CLI: no sibling `cercano`, and a PATH without one (a
  // developer's PATH may well have a real install).
  const lone = join(process.env.TMPDIR ?? "/tmp", `cercano-cli-lone-${process.pid}`);
  copyFileSync(CLI_BIN, lone);
  t.after(() => rmSync(lone, { force: true }));

  await withCli(t, { agent: false, bin: lone, env: { PATH: "/usr/bin:/bin" } }, async ({ term, sandbox }) => {
    await term.getByText("could not auto-launch").expect();
    assert.equal(await exitCode(term), 1);
    assert.equal(await portOpen(sandbox.port), false);
  });
});

test("the CLI auto-launches its sibling agent, which exits after the last client", async (t) => {
  await withCli(t, { agent: false }, async ({ term, sandbox }) => {
    t.after(() => killListener(sandbox.port));
    await ready(term);
    assert.equal(await portOpen(sandbox.port), true, "auto-launched agent should be listening");
    const log = readFileSync(join(sandbox.tmp, "cercano-server.log"), "utf8");
    assert.match(log, /Server listening/);

    await term.press("Ctrl+C");
    await term.press("Ctrl+C");
    assert.equal(await exitCode(term), 0);
    await waitForPort(sandbox.port, { open: false, timeout: 15_000 });
  });
});

// Safety net so a failed run never leaves an auto-launched agent behind.
function killListener(port) {
  try {
    const pids = execFileSync("lsof", ["-ti", `tcp:${port}`, "-sTCP:LISTEN"], { encoding: "utf8" }).trim();
    for (const pid of pids.split("\n").filter(Boolean)) process.kill(Number(pid), "SIGKILL");
  } catch {
    // nothing listening
  }
}
