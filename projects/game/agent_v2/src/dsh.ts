/**
 * dsh.ts — composition boot for the game agent_v2 service.
 *
 * Prepares the configuration surface the cordis.yml `!!js` expressions read
 * (GLM + opencode-go endpoint resolution and API token injection, research
 * specs/049-agent-v2-dsh-init/research.md D9 and
 * specs/063-llm-reliability-opencode-go/research.md D13; preset template root
 * resolution, specs/060-agent-v2-team-optimize/contracts/deploy-env.md §2),
 * then boots the composition manifest (direct-composed dsh core plugins + the
 * saolei plugin set, specs/051-agent-v2-dsh-migration/contracts/
 * saolei-plugins.md §5) in-process (B1 embedding,
 * specs/049-agent-v2-dsh-init/spec.md FR-002). Resolver, template-root, and
 * boot failures are fail-loud — a half-started composition never serves
 * traffic; a missing API token is tolerated: the host boots WITHOUT that
 * token and model requests then skip the Authorization header
 * (specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §3 义务 6;
 * specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md §3).
 * Diagnostics never contain a token value
 * (specs/049-agent-v2-dsh-init/spec.md SC-004).
 */

import * as fs from "node:fs";
import * as path from "node:path";
import { boot } from "@deepseek-ai/dsh-app-boot";
import { ENV_DOMINION_ARTIFACT_DIR, ENV_DOMINION_SECRET_DIR } from "@dominion/common-js-constants";
import { error, info, warn } from "@dominion/common-js-logs";
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

/** Production opencode-go endpoint when neither OPENCODE_BASE_URL nor OPENCODE_LLM_TARGET is set (specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md §1). */
export const OPENCODE_DEFAULT_BASE_URL = "https://opencode.ai/zen/go/v1";

/** Logical opencode-go secret file name under $DOMINION_SECRET_DIR (specs/063-llm-reliability-opencode-go/research.md D13). */
export const OPENCODE_SECRET_FILE = "opencode-api-token";

/** DOMINION_SECRET_DIR fallback, matching the deployment secret-mount convention (specs/049-agent-v2-dsh-init/spec.md FR-008). */
const SECRET_DIR_FALLBACK = "/etc/secrets";

/** Explicit template-root override env, read here and by cordis.yml (specs/060-agent-v2-team-optimize/contracts/deploy-env.md §2). */
const PRESET_TEMPLATES_ROOT_ENV = "PRESET_TEMPLATES_ROOT";

/** Shipped pool-template directory under $DOMINION_ARTIFACT_DIR (agent_v2/BUILD.bazel artifact_pkg_js data). */
const PRESET_TEMPLATES_DIR_NAME = "preset-templates";

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
 * Inject `GLM_API_KEY`, `GLM_BASE_URL`, `OPENCODE_API_KEY`,
 * `OPENCODE_BASE_URL`, and the resolved `PRESET_TEMPLATES_ROOT`, then boot
 * the composition.
 *
 * The endpoint resolution must precede `boot` because the cordis.yml `!!js`
 * expression is evaluated synchronously while the Loader mounts the adapter
 * rows (specs/049-agent-v2-dsh-init/research.md D9). Each token is read in
 * this single host-side spot and injected as an env value — the plugins
 * themselves do no file IO (the adapter cookbook convention:
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md).
 * The template root is resolved the same way: the roster rows read
 * `process.env.PRESET_TEMPLATES_ROOT` when the agent-presets row mounts
 * (specs/060-agent-v2-team-optimize/contracts/deploy-env.md §2).
 *
 * @param deps - optional test doubles for the resolver, boot, env, and secret reader.
 * @returns the settled composition context.
 */
export async function bootDsh(deps: DshBootDeps = {}): Promise<DshContext> {
  const env = deps.env ?? process.env;
  const doBoot = deps.boot ?? boot;
  try {
    env.GLM_BASE_URL = await resolveEndpoint(
      deps,
      "GLM_BASE_URL",
      "GLM_LLM_TARGET",
      GLM_DEFAULT_BASE_URL,
    );
    const glmApiKey = resolveApiKey(deps, "GLM", "GLM_API_KEY", GLM_SECRET_FILE);
    if (glmApiKey !== undefined) {
      env.GLM_API_KEY = glmApiKey;
    }
    env.OPENCODE_BASE_URL = await resolveEndpoint(
      deps,
      "OPENCODE_BASE_URL",
      "OPENCODE_LLM_TARGET",
      OPENCODE_DEFAULT_BASE_URL,
    );
    const opencodeApiKey = resolveApiKey(
      deps,
      "OpenCode",
      "OPENCODE_API_KEY",
      OPENCODE_SECRET_FILE,
    );
    if (opencodeApiKey !== undefined) {
      env.OPENCODE_API_KEY = opencodeApiKey;
    }
    env[PRESET_TEMPLATES_ROOT_ENV] = resolvePresetTemplatesRoot(env);

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
 * Endpoint precedence shared by both LLM rows (specs/049-agent-v2-dsh-init/
 * research.md D9): the explicit `*_BASE_URL` as-is > `*_LLM_TARGET` resolved
 * through Dominion service discovery (+ the `/v1` version path) > the
 * production endpoint.
 */
async function resolveEndpoint(
  deps: DshBootDeps,
  directEnv: string,
  targetEnv: string,
  fallback: string,
): Promise<string> {
  const env = deps.env ?? process.env;
  const direct = env[directEnv];
  if (direct) {
    return direct;
  }
  const target = env[targetEnv];
  if (target) {
    const resolver = deps.resolver ?? createResolver();
    const endpoints = await resolver.resolve(target);
    if (endpoints.length === 0) {
      throw new Error(`resolver returned no endpoints for ${target}`);
    }
    return `http://${endpoints[0]}/v1`;
  }
  return fallback;
}

/**
 * Resolve the roster template root
 * (specs/060-agent-v2-team-optimize/contracts/deploy-env.md §2): an explicit
 * `PRESET_TEMPLATES_ROOT` wins for local/test runs, otherwise it derives
 * `${DOMINION_ARTIFACT_DIR}/preset-templates` from the platform-injected
 * artifact directory. With neither set the composition cannot resolve its
 * system roots, so the resolution throws and the boot exits fail-loud; the
 * error names both variables. Blank values count as unset, consistent with
 * the GLM credential resolution above.
 */
function resolvePresetTemplatesRoot(env: Record<string, string | undefined>): string {
  const override = env[PRESET_TEMPLATES_ROOT_ENV]?.trim();
  if (override) {
    return override;
  }
  const artifactDir = env[ENV_DOMINION_ARTIFACT_DIR]?.trim();
  if (artifactDir) {
    return path.join(artifactDir, PRESET_TEMPLATES_DIR_NAME);
  }
  throw new Error(
    `cannot resolve preset templates root: set ${PRESET_TEMPLATES_ROOT_ENV} or provide ${ENV_DOMINION_ARTIFACT_DIR}`,
  );
}

/**
 * Resolve one LLM API key in three levels (specs/049-agent-v2-dsh-init/
 * research.md D9; specs/063-llm-reliability-opencode-go/research.md D13): a
 * pre-set env value wins (trimmed, whitespace-only counts as unset);
 * otherwise the provider's token file is read. A missing file, a read error,
 * or empty content all count as absent — the function returns `undefined` and
 * the caller boots WITHOUT the env value: the plugin then sends model
 * requests without an Authorization header (glm-llm-plugin.md §3 义务 6 /
 * opencode-go-plugin.md §3 — the fake endpoint ignores credentials; a real
 * endpoint's 401 surfaces as the first turn's `turn_end{ERROR}`). The absence
 * warning names the env and the secret file path, never any key value
 * (specs/049-agent-v2-dsh-init/spec.md SC-004).
 */
function resolveApiKey(
  deps: DshBootDeps,
  label: string,
  envName: string,
  secretFile: string,
): string | undefined {
  const env = deps.env ?? process.env;
  const envKey = env[envName]?.trim();
  if (envKey) {
    return envKey;
  }
  const dir = deps.secretDir ?? env[ENV_DOMINION_SECRET_DIR] ?? SECRET_DIR_FALLBACK;
  const file = path.join(dir, secretFile);
  const read = deps.readSecretFile ?? readSecretFile;

  let reason: string;
  try {
    // Trim on the consumer side so an injected reader observes the raw file
    // while the value still normalizes (trailing newline).
    const token = read(file).trim();
    if (token) {
      return token;
    }
    reason = "absent or empty";
  } catch (err) {
    reason = err instanceof Error ? err.message : String(err);
  }
  warn(`${label} API token unavailable; booting without ${envName}`, {
    env: envName,
    secretFile: file,
    reason,
  });
  return undefined;
}

function readSecretFile(file: string): string {
  return fs.readFileSync(file, "utf8");
}
