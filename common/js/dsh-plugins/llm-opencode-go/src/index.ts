/**
 * cordis plugin entry for the opencode-go Chat Completions LLM adapter.
 * Export shape follows the official adapter plugin contract (name / inject /
 * Config / apply) per the adapter cookbook; registration is effect-based and
 * disposed with the fiber.
 * Contract: specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md §2.
 */

import type { Context } from "@deepseek-ai/cordis";
import { RetryPolicySchema } from "@deepseek-ai/dsh-llm";
import { MAX_TIMER_DELAY_MS } from "@deepseek-ai/dsh-timeout";
import z from "@deepseek-ai/schemastery";

import { DEFAULT_STREAM_IDLE_TIMEOUT_MS, OpencodeGoChatAdapter } from "./adapter.js";
import type { OpencodeGoConfig } from "./adapter.js";

export const name = "llm-opencode-go";

export const inject = ["llm"];

export type { OpencodeGoConfig };

/** The provider route the adapter registers (consumed via agentOptions). */
const PROVIDER_ROUTE = "opencode-go";

/** Default token environment variable (contract §2; empty value → no Authorization). */
const DEFAULT_API_KEY_ENV = "OPENCODE_API_KEY";

/** Default endpoint base URL, version path included (contract §2). */
const DEFAULT_BASE_URL = "https://opencode.ai/zen/go/v1";

/**
 * The gateway's official Chat Completions route catalog: all 16 documented
 * ids (contract §2; https://opencode.ai/docs/zh-cn/go/ , 2026-09-12).
 * Responses/Anthropic-routed models and gateway-only undocumented ids are
 * excluded per FR-015. `contextWindow` is the models.dev `opencode-go`
 * snapshot, advisory and deployment-adjustable. The first entry honors
 * `OPENCODE_MODEL` so a deployment env can pick the default model without
 * rewriting the catalog.
 */
export const DEFAULT_MODELS = [
  { id: process.env.OPENCODE_MODEL || "glm-5.3", contextWindow: 1_000_000 },
  { id: "glm-5.3-flash", contextWindow: 1_000_000 },
  { id: "glm-5.2", contextWindow: 1_000_000 },
  { id: "glm-5.1", contextWindow: 202_752 },
  { id: "kimi-k3", contextWindow: 1_048_576 },
  { id: "kimi-k2.7-code", contextWindow: 262_144 },
  { id: "kimi-k2.6", contextWindow: 262_144 },
  { id: "longcat-2.0", contextWindow: 1_000_000 },
  { id: "deepseek-v4.1-flash", contextWindow: 1_000_000 },
  { id: "deepseek-v4-pro", contextWindow: 1_000_000 },
  { id: "deepseek-v4-flash", contextWindow: 1_000_000 },
  { id: "deepseek-v4-flash-vision-exp", contextWindow: 1_000_000 },
  { id: "mimo-v2.5", contextWindow: 1_000_000 },
  { id: "mimo-v2.5-pro", contextWindow: 1_048_576 },
  { id: "hy4-preview", contextWindow: 1_024_000 },
  { id: "hy3", contextWindow: 256_000 },
];

// Schemastery's ObjectS input shape (optional/null-able properties,
// mutable arrays) is deliberately wider than the validated output type, so
// the built schema cannot be directly assigned to z<OpencodeGoConfig>; the
// cast mirrors the official adapters' declared `Config: z<Config>` shape.
// `models` defaults to the full official catalog so a composition row that
// omits it still exposes every FR-015-eligible model; retryPolicy/
// streamIdleTimeoutMs are optional, the watchdog window bounded by the
// largest delay Node schedules without clamping it to one millisecond
// (MAX_TIMER_DELAY_MS, dsh-timeout).
export const Config: z<OpencodeGoConfig> = z.object({
  apiKeyEnv: z.string().default(DEFAULT_API_KEY_ENV),
  baseURL: z.string().default(DEFAULT_BASE_URL),
  models: z.array(z.object({ id: z.string(), contextWindow: z.number() })).default(DEFAULT_MODELS),
  retryPolicy: RetryPolicySchema,
  streamIdleTimeoutMs: z
    .number()
    .min(Number.MIN_VALUE)
    .max(MAX_TIMER_DELAY_MS)
    .default(DEFAULT_STREAM_IDLE_TIMEOUT_MS),
}) as unknown as z<OpencodeGoConfig>;

export function apply(ctx: Context, config: OpencodeGoConfig): void {
  ctx.llm.registerAdapter([PROVIDER_ROUTE], new OpencodeGoChatAdapter(config));
}
