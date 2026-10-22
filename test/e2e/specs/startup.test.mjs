import { test } from "node:test";
import { withCli, ready } from "../helpers/cli.mjs";

test("starts against a running agent and shows the prompt", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.getByText("local-first ai coprocessor").expect();
    // The status line reflects the sandbox's model override, so the agent
    // really is the isolated one and not a developer's running instance.
    await term.getByText("o:fake-model").expect();
  });
});
