import { test } from "node:test";
import assert from "node:assert/strict";
import { withCli, exitCode, ready, PLACEHOLDER } from "../helpers/cli.mjs";

const ARMED = "press ^C again to quit";

test("Ctrl-C on an empty prompt arms, a second Ctrl-C quits", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.press("Ctrl+C");
    await term.getByText(ARMED).expect();
    await term.press("Ctrl+C");
    assert.equal(await exitCode(term), 0);
  });
});

test("Ctrl-C with text clears the prompt instead of arming", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.type("draft that should vanish");
    await term.getByText("draft that should vanish").expect();
    await term.press("Ctrl+C");
    await term.getByText("draft that should vanish").expect({ not: true });
    await term.getByText(PLACEHOLDER).expect();
    await term.getByText(ARMED).expect({ not: true });
  });
});

test("/quit exits", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.submit("/quit");
    assert.equal(await exitCode(term), 0);
  });
});
