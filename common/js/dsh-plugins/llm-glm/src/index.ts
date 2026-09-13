/**
 * cordis plugin entry for the GLM Responses LLM adapter. Export shape
 * follows the official adapter plugin contract (name / inject / Config /
 * apply) per the adapter cookbook; registration is effect-based and
 * disposed with the fiber.
 * Contract: specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §2.
 */

import type { Context } from "@deepseek-ai/cordis";
import { RetryPolicySchema } from "@deepseek-ai/dsh-llm";
import { MAX_TIMER_DELAY_MS } from "@deepseek-ai/dsh-timeout";
import z from "@deepseek-ai/schemastery";

import { DEFAULT_STREAM_IDLE_TIMEOUT_MS, GlmResponsesAdapter } from "./adapter.js";
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
// retryPolicy/streamIdleTimeoutMs are optional; the watchdog window is
// bounded by the largest delay Node schedules without clamping it to one
// millisecond (MAX_TIMER_DELAY_MS, dsh-timeout).
export const Config: z<GlmConfig> = z.object({
  apiKeyEnv: z.string(),
  baseURL: z.string(),
  models: z.array(z.object({ id: z.string(), contextWindow: z.number() })),
  retryPolicy: RetryPolicySchema,
  streamIdleTimeoutMs: z
    .number()
    .min(Number.MIN_VALUE)
    .max(MAX_TIMER_DELAY_MS)
    .default(DEFAULT_STREAM_IDLE_TIMEOUT_MS),
}) as unknown as z<GlmConfig>;

export function apply(ctx: Context, config: GlmConfig): void {
  ctx.llm.registerAdapter([PROVIDER_ROUTE], new GlmResponsesAdapter(config));
}
