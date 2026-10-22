import { test } from "node:test";
import { withCli, ready } from "../helpers/cli.mjs";

test("/help lists the slash commands", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.submit("/help");
    // The list is taller than the viewport, which follows the tail; assert on
    // entries near the end of the alphabetical list.
    for (const line of [
      "/help  —  List available slash commands.",
      "/quit (exit)  —  Leave the REPL.",
      "/setup  —",
      "/tools  —",
    ]) {
      await term.getByText(line).expect();
    }
  });
});

test("an unknown command prefix suggests the full command", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.submit("/hel");
    await term.getByText("unknown command /hel — did you mean /help?").expect();
  });
});

test("an unknown command points at /help", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.submit("/zzz");
    await term.getByText("unknown command /zzz (try /help)").expect();
  });
});

test("Tab completes a unique slash command prefix", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.type("/hel");
    await term.press("Tab");
    await term.getByText("▶ /help").expect();
    await term.press("Enter");
    await term.getByText("/help  —  List available slash commands.").expect();
  });
});
