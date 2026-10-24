// Per-test isolation: a throwaway HOME/XDG_CONFIG_HOME/TMPDIR and a free agent
// port, so a test never reads the developer's ~/.config/cercano or attaches to
// the real agent on :50052.
import { mkdtempSync, mkdirSync, rmSync, writeFileSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:net";

export async function freePort() {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

// createSandbox returns the directories and env for one test. Unless
// seedConfig is false it writes a config.yaml so the CLI skips the setup
// wizard; the seeded config runs open_only against Ollama at ollamaUrl (a fake
// server, or an unused port when the test never chats).
export async function createSandbox({ seedConfig = true, ollamaUrl } = {}) {
  // The agent puts worker Unix sockets under $TMPDIR/cercano-workers/, and
  // macOS caps socket paths at 104 bytes; its default /var/folders/... tmpdir
  // nested under a sandbox overflows that. Use the short /tmp on Unix.
  const base = process.platform === "win32" ? tmpdir() : "/tmp";
  // realpath: on macOS /tmp is a symlink to /private/tmp.
  const root = realpathSync(mkdtempSync(join(base, "ce2e-")));
  const home = join(root, "home");
  const configDir = join(home, ".config", "cercano");
  const tmp = join(root, "t");
  mkdirSync(configDir, { recursive: true });
  mkdirSync(tmp, { recursive: true });

  const port = await freePort();
  const ollama = ollamaUrl ?? `http://127.0.0.1:${await freePort()}`;
  const configPath = join(configDir, "config.yaml");

  // tui-test merges this over the inherited environment, so anything that
  // could leak the developer's setup is overridden explicitly. Empty values
  // read as unset to the Go side.
  const env = {
    HOME: home,
    XDG_CONFIG_HOME: join(home, ".config"),
    TMPDIR: tmp,
    TERM: "xterm-256color",
    CERCANO_PORT: String(port),
    OLLAMA_URL: ollama,
    CERCANO_OPEN_RUNTIME: "ollama",
    CERCANO_OPEN_MODEL: "fake-model",
    CERCANO_CONVERSATIONS_DB: join(configDir, "conversations.db"),
    CERCANO_WIZARD_STATE: join(configDir, "wizard_state.yaml"),
    CERCANO_UI_CONFIG: join(configDir, "ui.yaml"),
    CERCANO_COLORPROBE_LOG: join(root, "colorprobe.log"),
    CERCANO_LOCAL_RUNTIME: "",
    CERCANO_LOCAL_MODEL: "",
    CERCANO_EMBEDDING_MODEL: "",
    GEMINI_API_KEY: "",
    ANTHROPIC_API_KEY: "",
    OPENAI_API_KEY: "",
  };

  const sandbox = {
    root,
    home,
    configDir,
    configPath,
    tmp,
    port,
    ollamaUrl: ollama,
    env,
    writeConfig(extra = "") {
      writeFileSync(
        configPath,
        [
          "locus_mode: open_only",
          "open_runtime: ollama",
          `ollama_url: ${ollama}`,
          `port: "${port}"`,
          // Pin every local tier, not only Everyday (CERCANO_OPEN_MODEL).
          // Chat defaults to Premium; side calls may select other tiers.
          "models:",
          "  open:",
          "    overrides:",
          "      ollama:",
          ...["most_capable", "everyday", "fast_light", "fast_light_text", "embedding", "vision"]
            .map((tier) => `        ${tier}: fake-model`),
          extra,
        ].join("\n") + "\n",
      );
    },
    cleanup() {
      if (!process.env.E2E_KEEP_SANDBOX) rmSync(root, { recursive: true, force: true });
    },
  };
  if (seedConfig) sandbox.writeConfig();
  return sandbox;
}
