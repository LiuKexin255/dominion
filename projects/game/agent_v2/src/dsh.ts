/**
 * dsh.ts — composition boot for the game agent_v2 service.
 *
 * Prepares the configuration surface the cordis.yml `!!js` expressions read
 * (GLM endpoint resolution + GLM API token injection, research
 * specs/049-agent-v2-dsh-init/research.md D9), then boots the two-row
 * composition manifest (agent spine + GLM Responses adapter) in-process
 * (B1 embedding, specs/049-agent-v2-dsh-init/spec.md FR-002). Any failure is
 * fail-loud: diagnostics are logged and the process exits non-zero — a
 * half-started composition never serves traffic. Diagnostics never contain
 * the token value (specs/049-agent-v2-dsh-init/spec.md SC-004).
 */

import * as fs from "node:fs";
import * as path from "node:path";
import { boot } from "@deepseek-ai/dsh-app-boot";
import { error, info } from "@dominion/common-js-logs";
import { createResolver } from "@dominion/common-js-resolver";
import type { EndpointResolver } from "@dominion/common-js-resolver";

/**
 * The cordis Context returned by `boot()`. Derived from `boot`'s own return
 * type so this package never imports `@deepseek-ai/cordis` directly (it is a
 * transitive peer of the framework core, not one of our declared deps).
 */
export type DshContext = Awaited<ReturnType<typeof boot>>;

/** Production model endpoint when neither GLM_BASE_URL nor GLM_LLM_TARGET is set (specs/049-agent-v2-dsh-init/spec.md FR-007). */
export const GLM_DEFAULT_BASE_URL = "https://open.bigmodel.cn/api/v1";

/** Logical secret file name under $DOMINION_SECRET_DIR (specs/049-agent-v2-dsh-init/spec.md FR-008). */
export const GLM_SECRET_FILE = "glm-api-token";

/** DOMINION_SECRET_DIR fallback, matching the existing game agent convention (projects/game/agent/src/server.ts:124). */
const SECRET_DIR_FALLBACK = "/etc/secrets";

/** Diagnostic prefix shared by every fail-loud message below. */
const BIN_NAME = "game-agent-v2";

/**
 * Injectable collaborators. Production wiring uses the real `boot`, a plain
 * Dominion resolver, and `process.env`; tests substitute `vi.fn()` doubles
 * and a plain object env through this seam instead of module interception
 * (style/javascript.md Mock convention).
 */
export interface DshBootDeps {
  resolver?: EndpointResolver;
  boot?: typeof boot;
  /** Environment read/written instead of `process.env` (test seam). */
  env?: Record<string, string | undefined>;
  /** Overrides $DOMINION_SECRET_DIR for the token file lookup (test seam). */
  secretDir?: string;
  /** Reads the token file; defaults to fs.readFileSync utf8, trimmed. */
  readSecretFile?: (file: string) => string;
}

/**
 * Absolute path of the composition manifest shipped next to the compiled
 * sources: the service root is the parent of `src/`, and `cordis.yml` travels
 * there as an `artifact_pkg_js` data file.
 */
export function cordisConfigPath(): string {
  return path.resolve(import.meta.dirname, "..", "cordis.yml");
}

/**
 * Inject `GLM_API_KEY` and `GLM_BASE_URL`, then boot the composition.
 *
 * The endpoint resolution must precede `boot` because the cordis.yml `!!js`
 * expression is evaluated synchronously while the Loader mounts the adapter
 * row (specs/049-agent-v2-dsh-init/research.md D9). The token is read in this
 * single host-side spot and injected as an env value — the plugin itself does
 * no file IO (the adapter cookbook convention:
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md).
 *
 * @param deps - optional test doubles for the resolver, boot, env, and secret reader.
 * @returns the settled composition context.
 */
export async function bootDsh(deps: DshBootDeps = {}): Promise<DshContext> {
  const env = deps.env ?? process.env;
  const doBoot = deps.boot ?? boot;
  try {
    env.GLM_BASE_URL = await resolveBaseURL(deps);
    env.GLM_API_KEY = readGlmApiKey(deps);

    const configPath = cordisConfigPath();
    // boot's 5th parameter anchors bare plugin-name resolution at this
    // module, i.e. the service-root node_modules; import.meta.url is this
    // module's own file URL (specs/047-dsh-chat-demo/research.md D10-1).
    const ctx = await doBoot(BIN_NAME, configPath, undefined, undefined, import.meta.url);
    info("dsh composition booted", { configPath });
    return ctx;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    error("dsh boot failed, exiting (fail-loud)", {
      bin: BIN_NAME,
      config: cordisConfigPath(),
      error: message,
    });
    // Unconditional stderr line: the structured logger may itself be part of
    // the failing surface, and container log capture must never miss it.
    // `message` carries only paths/codes, never the token value (SC-004).
    console.error(`[${BIN_NAME}] boot failed (fail-loud): ${message}`);
    process.exit(1);
  }
}

/**
 * Endpoint precedence (specs/049-agent-v2-dsh-init/research.md D9):
 * GLM_BASE_URL as-is > GLM_LLM_TARGET resolved through Dominion service
 * discovery (+ the `/v1` version path) > the production GLM endpoint.
 */
async function resolveBaseURL(deps: DshBootDeps): Promise<string> {
  const env = deps.env ?? process.env;
  const direct = env.GLM_BASE_URL;
  if (direct) {
    return direct;
  }
  const target = env.GLM_LLM_TARGET;
  if (target) {
    const resolver = deps.resolver ?? createResolver();
    const endpoints = await resolver.resolve(target);
    if (endpoints.length === 0) {
      throw new Error(`resolver returned no endpoints for ${target}`);
    }
    return `http://${endpoints[0]}/v1`;
  }
  return GLM_DEFAULT_BASE_URL;
}

/**
 * Read the GLM token file into `GLM_API_KEY`. Missing or empty token is a
 * fail-loud startup error; the error names the path and the secret file,
 * never the value (specs/049-agent-v2-dsh-init/spec.md SC-004;
 * specs/002-deploy-secret-config/contracts/secret-config.md §5).
 */
function readGlmApiKey(deps: DshBootDeps): string {
  const env = deps.env ?? process.env;
  const dir = deps.secretDir ?? env.DOMINION_SECRET_DIR ?? SECRET_DIR_FALLBACK;
  const file = path.join(dir, GLM_SECRET_FILE);
  const read = deps.readSecretFile ?? readSecretFile;
  // Trim on the consumer side so an injected reader observes the raw file
  // while the injected value still normalizes (trailing newline).
  const token = read(file).trim();
  if (!token) {
    throw new Error(
      `GLM API token missing: secret file "${GLM_SECRET_FILE}" at ${file} is absent or empty; check the k8s secret binding (specs/002-deploy-secret-config/contracts/secret-config.md §2)`,
    );
  }
  return token;
}

function readSecretFile(file: string): string {
  try {
    return fs.readFileSync(file, "utf8");
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new Error(`secret file "${GLM_SECRET_FILE}" unreadable at ${file}: ${message}`);
  }
}
