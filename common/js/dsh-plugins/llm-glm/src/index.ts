/**
 * cordis plugin entry for the GLM Responses LLM adapter. Export shape
 * follows the official adapter plugin contract (name / inject / Config /
 * apply) per the adapter cookbook; registration is effect-based and
 * disposed with the fiber.
 * Contract: specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §2.
 */

import type { Context } from "@deepseek-ai/cordis";
import z from "@deepseek-ai/schemastery";

import { GlmResponsesAdapter } from "./adapter.js";
import type { GlmConfig } from "./adapter.js";

export const name = "llm-glm";

export const inject = ["llm"];

export type { GlmConfig };

/** The provider route the adapter registers (consumed via agentOptions). */
const PROVIDER_ROUTE = "glm-responses";

// Schemastery's ObjectS input shape (optional/null-able properties,
// mutable arrays) is deliberately wider than the validated output type, so
// the built schema cannot be directly assigned to z<GlmConfig>; the cast
// mirrors the official adapters' declared `Config: z<Config>` shape.
export const Config: z<GlmConfig> = z.object({
  apiKeyEnv: z.string(),
  baseURL: z.string(),
  models: z.array(z.object({ id: z.string(), contextWindow: z.number() })),
}) as unknown as z<GlmConfig>;

export function apply(ctx: Context, config: GlmConfig): void {
  ctx.llm.registerAdapter([PROVIDER_ROUTE], new GlmResponsesAdapter(config));
}
