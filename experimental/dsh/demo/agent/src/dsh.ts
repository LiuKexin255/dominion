/**
 * dsh.ts — composition boot for the demo chat agent.
 *
 * Resolves the fake-llm endpoint through Dominion service discovery, injects
 * it into the environment the cordis.yml `!!js` expression reads, then boots
 * the two-row composition manifest (agent spine + DeepSeek adapter) in-process
 * (B1 embedding, specs/047-dsh-chat-demo/spec.md FR-003/FR-011). Any failure is fail-loud: diagnostics are
 * logged and the process exits non-zero — a half-started composition never
 * serves traffic (specs/047-dsh-chat-demo/spec.md FR-009,
 * specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §1).
 */

import * as path from "node:path";
import { pathToFileURL } from "node:url";
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

/** Dominion resolver target for the fake-llm model endpoint (specs/047-dsh-chat-demo/spec.md FR-011). */
export const FAKE_LLM_TARGET = "dominion:///dsh-demo/fake-llm:8080";

/** Diagnostic prefix shared by every fail-loud message below. */
const BIN_NAME = "dsh-demo-agent";

/**
 * Injectable collaborators. Production wiring uses the real `boot` and a
 * plain Dominion resolver; tests substitute `vi.fn()` doubles through this
 * seam instead of module interception (style/javascript.md Mock convention).
 */
export interface DshBootDeps {
  resolver?: EndpointResolver;
  boot?: typeof boot;
}

/**
 * Absolute path of the composition manifest shipped next to the compiled
 * sources: the service root is the parent of `src/`, and `cordis.yml` travels
 * there as an `artifact_pkg_js` data file.
 */
export function cordisConfigPath(): string {
  return path.resolve(path.dirname(__filename), "..", "cordis.yml");
}

/**
 * Resolve fake-llm, inject `FAKE_LLM_BASE_URL`, and boot the composition.
 *
 * The resolve step must precede `boot` because the cordis.yml `!!js`
 * expression is evaluated synchronously while the Loader mounts the adapter
 * row (specs/047-dsh-chat-demo/research.md D2).
 *
 * @param deps - optional test doubles for the resolver and boot.
 * @returns the settled composition context.
 */
export async function bootDsh(deps: DshBootDeps = {}): Promise<DshContext> {
  const resolver = deps.resolver ?? createResolver();
  const doBoot = deps.boot ?? boot;
  try {
    const endpoints = await resolver.resolve(FAKE_LLM_TARGET);
    if (endpoints.length === 0) {
      throw new Error(`resolver returned no endpoints for ${FAKE_LLM_TARGET}`);
    }
    const baseURL = `http://${endpoints[0]}/v1`;
    process.env.FAKE_LLM_BASE_URL = baseURL;
    info("resolved fake-llm endpoint", {
      target: FAKE_LLM_TARGET,
      endpoint: endpoints[0],
      baseURL,
    });

    const configPath = cordisConfigPath();
    // boot's 5th parameter anchors bare plugin-name resolution at this
    // module, i.e. the service-root node_modules. The CJS entry's anchor is
    // the file URL of __filename — the equivalent of ESM's import.meta.url
    // (specs/047-dsh-chat-demo/research.md D10-1, D8).
    const ctx = await doBoot(
      BIN_NAME,
      configPath,
      undefined,
      undefined,
      pathToFileURL(__filename).href,
    );
    info("dsh composition booted", { configPath });
    return ctx;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    error("dsh boot failed, exiting (fail-loud)", {
      bin: BIN_NAME,
      config: cordisConfigPath(),
      target: FAKE_LLM_TARGET,
      error: message,
    });
    // Unconditional stderr line: the structured logger may itself be part of
    // the failing surface, and container log capture must never miss it.
    console.error(`[${BIN_NAME}] boot failed (fail-loud): ${message}`);
    process.exit(1);
  }
}
