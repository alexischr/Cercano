import { test } from "node:test";
import assert from "node:assert/strict";
import { withCli, ready, PLACEHOLDER } from "../helpers/cli.mjs";

// Alt+Enter / Shift+Enter compose a newline rather than submitting (#64).
for (const chord of ["Alt+Enter", "Shift+Enter"]) {
  test(`${chord} inserts a newline instead of submitting`, async (t) => {
    await withCli(t, {}, async ({ term }) => {
      await ready(term);
      await term.type("first line");
      await term.press(chord);
      await term.type("second line");

      const first = await (await term.getByText("first line").wait()).location();
      const second = await (await term.getByText("second line").wait()).location();
      assert.equal(second.start.row, first.start.row + 1, "second line should sit on the next row");

      // Still composing: nothing was submitted, so the splash banner is up
      // and the prompt is not back to its placeholder.
      await term.getByText("local-first ai coprocessor").expect();
      await term.getByText(PLACEHOLDER).expect({ not: true });
    });
  });
}
