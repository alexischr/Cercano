import { test } from "node:test";
import { withCli, ready } from "../helpers/cli.mjs";

const WELCOME = "Setup — cloud providers";
const PREFERENCE = "Setup — configure Cercano";
const ABANDON_ARMED = "abandon setup?";

test("a missing config.yaml opens the setup wizard", async (t) => {
  await withCli(t, { seedConfig: false }, async ({ term }) => {
    await term.getByText(WELCOME).expect();
    await term.getByText("Add your cloud providers").expect();
    await term.getByText("▶ Add cloud provider…").expect();
    await term.getByText("Continue without connecting").expect();
    await term.getByText("↑/↓ move · enter select · esc back · q abandon").expect();
  });
});

test("Enter advances the wizard and Esc steps back", async (t) => {
  await withCli(t, { seedConfig: false }, async ({ term }) => {
    await term.getByText(WELCOME).expect();
    await term.press("Down");
    await term.getByText("▶ Continue without connecting").expect();
    await term.press("Enter");
    await term.getByText(PREFERENCE).expect();
    await term.getByText("How would you like Cercano to work?").expect();
    await term.press("Escape");
    await term.getByText(WELCOME).expect();
  });
});

test("Down selects Continue without connecting", async (t) => {
  await withCli(t, { seedConfig: false }, async ({ term }) => {
    await term.getByText(WELCOME).expect();
    await term.press("Down");
    await term.getByText("▶ Continue without connecting").expect();
    await term.getByText("▶ Add cloud provider…").expect({ not: true });
  });
});

test("q asks before abandoning; any other key keeps going", async (t) => {
  await withCli(t, { seedConfig: false }, async ({ term }) => {
    await term.getByText(WELCOME).expect();
    await term.press("q");
    await term.getByText(ABANDON_ARMED).expect();
    await term.press("Down");
    await term.getByText(ABANDON_ARMED).expect({ not: true });
    await term.getByText(WELCOME).expect();
  });
});

test("q twice abandons setup and lands on the chat prompt", async (t) => {
  await withCli(t, { seedConfig: false }, async ({ term }) => {
    await term.getByText(WELCOME).expect();
    await term.press("q");
    await term.getByText(ABANDON_ARMED).expect();
    await term.press("q");
    await ready(term);
    await term.getByText(WELCOME).expect({ not: true });
  });
});

test("/setup reopens the wizard from chat", async (t) => {
  await withCli(t, {}, async ({ term }) => {
    await ready(term);
    await term.submit("/setup");
    await term.getByText("Setup —").expect();
  });
});

// The armed status is one ~190-character line that gets clipped at normal
// widths, cutting off the instruction that says how to confirm.
test(
  "the abandon prompt shows how to confirm at 120 columns",
  { todo: "#68: wizard status line is clipped before 'press q again to confirm'" },
  async (t) => {
    await withCli(t, { seedConfig: false, cols: 120 }, async ({ term }) => {
      await term.getByText(WELCOME).expect();
      await term.press("q");
      await term.getByText("press q again to confirm", { whitespace: "normalize" }).expect({ timeout: 3_000 });
    });
  },
);
