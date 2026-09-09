import { describe, expect, it, vi } from "vitest";

import type { Context } from "@deepseek-ai/cordis";

import { DEMO_ECHO_TOOL, apply } from "./index.js";

/** Minimal registry doubles — DI seam, no module interception. */
function mockCtx() {
  const register = vi.fn();
  const section = vi.fn();
  const ctx = { tools: { register }, systemPrompt: { section } } as unknown as Context;
  return { ctx, register, section };
}

describe("demo-echo plugin", () => {
  it("registers the tool and its guidance section in the same apply()", () => {
    const { ctx, register, section } = mockCtx();

    apply(ctx);

    expect(register).toHaveBeenCalledOnce();
    expect(section).toHaveBeenCalledOnce();
  });

  it("presents the demo_echo tool with a required text parameter", () => {
    const { ctx, register } = mockCtx();

    apply(ctx);

    const tool = register.mock.calls[0][0];
    expect(tool.name).toBe(DEMO_ECHO_TOOL);
    expect(tool.description).toBeTruthy();
    // defineTool compiles the parameter spec to raw JSON Schema
    // (parameterSchemaSpecToJsonSchema): requiredness becomes the object's
    // `required` string array.
    expect(tool.parameters.type).toBe("object");
    expect(tool.parameters.properties.text.type).toBe("string");
    expect(tool.parameters.required).toContain("text");
  });

  it("executes the tool deterministically", async () => {
    const { ctx, register } = mockCtx();

    apply(ctx);

    const tool = register.mock.calls[0][0];
    await expect(tool.execute({ text: "hello" }, {})).resolves.toBe("echo: hello");
  });

  it("registers guidance in the tool-guidance order band naming the tool", () => {
    const { ctx, section } = mockCtx();

    apply(ctx);

    const guidance = section.mock.calls[0][0];
    expect(guidance.order).toBeGreaterThanOrEqual(100);
    expect(guidance.order).toBeLessThan(200);
    expect(guidance.text).toContain(DEMO_ECHO_TOOL);
  });
});
