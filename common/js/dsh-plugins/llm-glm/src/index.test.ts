/**
 * Plugin-face tests for the cordis entry: the schemastery Config stays in
 * sync with GlmConfig (retryPolicy pass-through and the 300000ms
 * idle-watchdog default the adapter consumes; see
 * specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md
 * §1-§2).
 */

import { describe, expect, it } from "vitest";

import { DEFAULT_STREAM_IDLE_TIMEOUT_MS } from "./adapter.js";
import { Config, inject, name } from "./index.js";

const BASE = {
  apiKeyEnv: "GLM_API_KEY",
  baseURL: "https://glm.test/api/v1",
  models: [{ id: "glm-5.2", contextWindow: 1000000 }],
};

describe("llm-glm Config", () => {
  it("defaults streamIdleTimeoutMs and leaves retryPolicy unset", () => {
    const config = Config(BASE);

    expect(config.streamIdleTimeoutMs).toBe(DEFAULT_STREAM_IDLE_TIMEOUT_MS);
    expect(config.retryPolicy).toBeUndefined();
  });

  it("passes a configured retryPolicy through the schema", () => {
    const config = Config({ ...BASE, retryPolicy: { mode: "normal", maxRetries: 2 } });

    expect(config.retryPolicy).toMatchObject({ mode: "normal", maxRetries: 2 });
  });

  it("keeps the plugin identity contract", () => {
    expect(name).toBe("llm-glm");
    expect(inject).toEqual(["llm"]);
  });
});
