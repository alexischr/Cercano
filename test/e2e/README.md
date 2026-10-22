# cercano-cli end-to-end tests

Black-box tests that run the real `cercano-cli` and `cercano agent` binaries in a
pseudo-terminal, press keys and assert on the rendered screen. They use
[`@microsoft/tui-test`](https://github.com/microsoft/tui-test) (pinned to
0.1.0) with Node's built-in `node:test` runner.

```bash
cd test/e2e
npm ci
npm run build   # builds .bin/cercano and .bin/cercano-cli side by side
npm test
```

Requires Go (for the build), Node 20+, and `lsof`/`ps` (for auto-launch
cleanup). Runs on macOS and Linux. CI uses Node 22.13.0.

Run one file or one test:

```bash
node --test specs/chat.test.mjs
node --test --test-name-pattern="Esc cancels" specs/chat.test.mjs
```

## Isolation

Each test gets a sandbox (`helpers/sandbox.mjs`), so the suite never touches
`~/.config/cercano` or a running agent:

- `HOME`, `XDG_CONFIG_HOME` and `TMPDIR` point into a fresh `/tmp/ce2e-*`
  directory. It sits under `/tmp` because the agent's worker sockets live
  under `$TMPDIR`, and macOS caps Unix socket paths at 104 bytes.
- `CERCANO_PORT` is a free port, never the default 50052.
- A `config.yaml` is seeded with `locus_mode: open_only` and
  `open_runtime: ollama`, unless the test opts out to exercise the first-run
  wizard.
- `OLLAMA_URL` points at `helpers/fake-ollama.mjs`, which streams a scripted
  reply and records requests, or at an unused port when the test never chats.
  No cloud keys are set.

Most specs start `cercano agent` themselves (`helpers/agent.mjs`) and stop its
whole process group afterwards. `server-discovery.test.mjs` covers the CLI's
own auto-launch path. Its fallback cleanup (`helpers/owned-agent.mjs`) treats
port listeners only as candidates: it verifies the exact agent command, sandbox
HOME/TMPDIR/port, and auto-launch marker before signalling. It sends SIGTERM,
waits up to five seconds, then rechecks ownership and process start time before
SIGKILL. Unverifiable or unrelated processes are never signalled; an unsupported
ownership probe can therefore leave a failed run's agent for manual inspection.
Regression tests cover ownership rejection, escalation, identity changes, and a
real sandbox agent.

## Writing a spec

```js
import { test } from "node:test";
import { withCli, ready } from "../helpers/cli.mjs";

test("does the thing", async (t) => {
  await withCli(t, { ollama: { reply: "hi" } }, async ({ term, ollama }) => {
    await ready(term);
    await term.submit("hello");
    await term.getByText("hi").expect();
  });
});
```

`withCli` options: `args`, `agent` (start an agent, default true),
`seedConfig` (default true), `ollama` (true or `{ reply, chunkDelayMs }`),
`cols`/`rows` (default 120×40), `bin` and `env`.

Use `getByText(...).expect()` to wait for content. It polls until the 15s
timeout, so avoid fixed sleeps. Prefer structural assertions to
`expectSnapshot`: the renderer's cursor-movement bytes vary between frames,
which makes full-screen snapshots brittle.

## When a test fails

The failing screen, the agent log and the fake-Ollama request paths are printed
as test diagnostics. A full tui-test trace (`trace.html`, `session.cast`,
`current.svg`, `timeline.json`) is written to `artifacts/traces/`. CI uploads
`artifacts/` as `cli-e2e-traces`. Set `E2E_KEEP_SANDBOX=1` to keep the sandbox
directories for inspection.

Tests marked `{ todo }` document known UI issues; they run and report but don't
fail the suite.
