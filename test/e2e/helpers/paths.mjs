import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const E2E_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const BIN_DIR = process.env.CERCANO_E2E_BIN ?? join(E2E_DIR, ".bin");
export const CLI_BIN = join(BIN_DIR, "cercano-cli");
export const AGENT_BIN = join(BIN_DIR, "cercano");
export const ARTIFACTS_DIR = join(E2E_DIR, "artifacts");

for (const bin of [CLI_BIN, AGENT_BIN]) {
  if (!existsSync(bin)) {
    throw new Error(`${bin} is missing; run \`npm run build\` in test/e2e first`);
  }
}
