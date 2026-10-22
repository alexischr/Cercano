import { test } from "node:test";
import { withCli, ready, PLACEHOLDER } from "../helpers/cli.mjs";

test("the UI survives shrinking and growing the terminal", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.resize(80, 24);
    await term.getByText(PLACEHOLDER).expect();
    await term.resize(160, 50);
    await term.getByText(PLACEHOLDER).expect();
    // Still interactive after the resizes.
    await term.submit("/zzz");
    await term.getByText("unknown command /zzz (try /help)").expect();
  });
});
