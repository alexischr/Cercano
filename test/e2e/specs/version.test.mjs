import { test } from "node:test";
import assert from "node:assert/strict";
import { withCli, exitCode } from "../helpers/cli.mjs";

test("--version prints the version and exits without a server", async (t) => {
  await withCli(t, { args: ["--version"], agent: false }, async ({ term }) => {
    await term.getByText("cercano-cli ve2e").expect();
    assert.equal(await exitCode(term), 0);
  });
});
