/**
 * model-provider.test.ts — Tests for parseModelSpec, inferProvider, and
 * ModelProviderCache.
 */

import { describe, expect, it, vi } from "vitest";

import {
  parseModelSpec,
  inferProvider,
  ModelProviderCache,
} from "./model-provider.js";
import type { ProviderFactory, LLMProvider } from "./model-provider.js";

// ===========================================================================
// parseModelSpec
// ===========================================================================

describe("parseModelSpec", () => {
  it("strips the provider prefix from a {provider}/{model} spec", () => {
    expect(parseModelSpec("opencode-go/deepseek-v4-pro")).toBe("deepseek-v4-pro");
    expect(parseModelSpec("opencode-go/qwen3.7-max")).toBe("qwen3.7-max");
  });

  it("preserves slashes in the model name after the provider prefix", () => {
    expect(parseModelSpec("opencode-go/meta/llama-3")).toBe("meta/llama-3");
  });

  it("returns the input as-is when no provider prefix is present", () => {
    expect(parseModelSpec("deepseek-v4-pro")).toBe("deepseek-v4-pro");
  });

  it("returns empty string for provider-only spec", () => {
    expect(parseModelSpec("opencode-go/")).toBe("");
  });
});

// ===========================================================================
// inferProvider
// ===========================================================================

describe("inferProvider", () => {
  it("routes DeepSeek models through the OpenAI-compatible endpoint", () => {
    expect(inferProvider("deepseek-v4-pro")).toBe("openai");
    expect(inferProvider("deepseek-v4-flash")).toBe("openai");
  });

  it("routes Kimi and GLM models through the OpenAI-compatible endpoint", () => {
    expect(inferProvider("kimi-k2.7-code")).toBe("openai");
    expect(inferProvider("glm-5.1")).toBe("openai");
  });

  it("routes Qwen models through the Anthropic-compatible endpoint", () => {
    expect(inferProvider("qwen3.7-max")).toBe("anthropic");
    expect(inferProvider("qwen3.7-plus")).toBe("anthropic");
    expect(inferProvider("qwen3.6-plus")).toBe("anthropic");
  });

  it("routes MiniMax models through the Anthropic-compatible endpoint", () => {
    expect(inferProvider("minimax-m3")).toBe("anthropic");
    expect(inferProvider("minimax-m2.7")).toBe("anthropic");
  });

  it("is case-insensitive", () => {
    expect(inferProvider("Qwen3.7-Max")).toBe("anthropic");
    expect(inferProvider("DeepSeek-V4-Pro")).toBe("openai");
  });
});

// ===========================================================================
// ModelProviderCache
// ===========================================================================

describe("ModelProviderCache", () => {
  it("creates provider on first call and caches by bare model name", async () => {
    const created: string[] = [];
    const factory: ProviderFactory = async (bareModel) => {
      created.push(bareModel);
      return {} as never;
    };
    const cache = new ModelProviderCache("", "", "", factory);

    await cache.getProvider("opencode-go/gpt-4o");
    await cache.getProvider("opencode-go/gpt-4o");

    expect(created).toEqual(["gpt-4o"]);
  });

  it("creates separate providers for different models", async () => {
    const created: string[] = [];
    const factory: ProviderFactory = async (bareModel) => {
      created.push(bareModel);
      return {} as never;
    };
    const cache = new ModelProviderCache("", "", "", factory);

    await cache.getProvider("opencode-go/gpt-4o");
    await cache.getProvider("opencode-go/minimax-m1");

    expect(created).toEqual(["gpt-4o", "minimax-m1"]);
  });

  it("returns the cached instance on repeated calls", async () => {
    const factory: ProviderFactory = vi.fn(async () => ({ id: 1 }) as never);
    const cache = new ModelProviderCache("", "", "", factory);

    const first = await cache.getProvider("gpt-4o");
    const second = await cache.getProvider("gpt-4o");

    expect(first).toBe(second);
    expect(factory).toHaveBeenCalledTimes(1);
  });

  it("routes OpenAI-family models to the OpenAI base URL", async () => {
    const calls: Array<{ provider: LLMProvider; baseUrl: string }> = [];
    const factory: ProviderFactory = async (bareModel, provider, baseUrl) => {
      calls.push({ provider, baseUrl });
      return {} as never;
    };
    const cache = new ModelProviderCache(
      "https://opencode.ai/zen/go/v1",
      "https://opencode.ai/zen/go",
      "secret",
      factory,
    );

    await cache.getProvider("opencode-go/deepseek-v4-pro");

    expect(calls).toEqual([
      { provider: "openai", baseUrl: "https://opencode.ai/zen/go/v1" },
    ]);
  });

  it("routes Anthropic-family models to the Anthropic base URL", async () => {
    const calls: Array<{ provider: LLMProvider; baseUrl: string }> = [];
    const factory: ProviderFactory = async (bareModel, provider, baseUrl) => {
      calls.push({ provider, baseUrl });
      return {} as never;
    };
    const cache = new ModelProviderCache(
      "https://opencode.ai/zen/go/v1",
      "https://opencode.ai/zen/go",
      "secret",
      factory,
    );

    await cache.getProvider("opencode-go/qwen3.7-max");

    expect(calls).toEqual([
      { provider: "anthropic", baseUrl: "https://opencode.ai/zen/go" },
    ]);
  });
});
