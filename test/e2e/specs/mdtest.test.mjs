import { test } from "node:test";
import { withCli } from "../helpers/cli.mjs";

// --mdtest pre-loads a markdown sample with no model call. Its wide table is
// drawn as a grid when it fits and transposed into "Key: value" records when
// it doesn't. Structural assertions rather than a screen snapshot: the
// renderer's cursor-movement bytes (tabs) vary frame to frame.
const END = "That's the end of the sample.";

test("--mdtest draws the wide table as a grid on a wide terminal", async (t) => {
  await withCli(t, { args: ["--mdtest"], cols: 140, rows: 40 }, async ({ term }) => {
    await term.getByText(END).expect();
    await term.getByText(String.raw`│ Tool\s+│ Tier │ Args\s+│ What it does\s+│`, { regex: true }).expect();
    await term.getByText("│ RunCommand │ W    │").expect();
    await term.getByText("Tool: RunCommand").expect({ not: true });
  });
});

test("--mdtest transposes the wide table on a narrow terminal", async (t) => {
  await withCli(t, { args: ["--mdtest"], cols: 60, rows: 40 }, async ({ term }) => {
    await term.getByText(END).expect();
    await term.getByText("Tool: RunCommand").expect();
    await term.getByText("Args: remote, branch").expect();
    await term.getByText("│ RunCommand │").expect({ not: true });
  });
});
