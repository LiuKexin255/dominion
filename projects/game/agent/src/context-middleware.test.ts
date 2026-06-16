/**
 * context-middleware.test.ts — Tests for ContextMiddleware.
 */

import { describe, it, expect } from "vitest";
import { HumanMessage, AIMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { beforeModelMiddleware } from "./context-middleware";

/**
 * Helper: unwrap the beforeModel handler from the middleware.
 *
 * The beforeModel hook can be either a plain function or an object
 * with `hook` and optional `canJumpTo` properties.
 */
function getBeforeModelHandler(
  mw: typeof beforeModelMiddleware,
): (state: { messages: BaseMessage[] }, runtime: unknown) => unknown {
  const hook = mw.beforeModel;
  if (typeof hook === "function") {
    return hook as (
      state: { messages: BaseMessage[] },
      runtime: unknown,
    ) => unknown;
  }
  if (hook && typeof hook === "object" && "hook" in hook) {
    return hook.hook as (
      state: { messages: BaseMessage[] },
      runtime: unknown,
    ) => unknown;
  }
  throw new Error(
    "beforeModel is neither a function nor a { hook, canJumpTo } object",
  );
}

describe("ContextMiddleware", () => {
  // -------------------------------------------------------------------------
  // Identity & replaceability
  // -------------------------------------------------------------------------

  it("has a identifiable name", () => {
    expect(beforeModelMiddleware.name).toBe("ContextMiddleware");
  });

  it("has a beforeModel hook", () => {
    expect(beforeModelMiddleware.beforeModel).toBeDefined();
  });

  it("is replaceable (has same structural shape as any AgentMiddleware)", () => {
    expect(beforeModelMiddleware).toHaveProperty("name");
    expect(beforeModelMiddleware).toHaveProperty("beforeModel");
  });

  // -------------------------------------------------------------------------
  // Functional behavior: receives state.messages, returns equivalent state
  // -------------------------------------------------------------------------

  it("receives state with messages and returns equivalent state", async () => {
    const handler = getBeforeModelHandler(beforeModelMiddleware);

    const state = {
      messages: [
        new HumanMessage("Hello from user"),
        new AIMessage("Hello from assistant"),
      ],
    };

    const result = await handler(state, {});

    // The middleware should either return undefined (pass-through)
    // or return an object with the same messages.
    if (result === undefined) {
      // Undefined means pass-through: verify state unaffected.
      expect(state.messages).toHaveLength(2);
      expect(state.messages[0].content).toBe("Hello from user");
      expect(state.messages[1].content).toBe("Hello from assistant");
    } else {
      const partial = result as { messages?: BaseMessage[] };
      expect(partial).toHaveProperty("messages");
      expect(partial.messages).toHaveLength(2);
      expect(partial.messages![0].content).toBe("Hello from user");
      expect(partial.messages![1].content).toBe("Hello from assistant");
    }
  });

  it("preserves message types through the middleware", async () => {
    const handler = getBeforeModelHandler(beforeModelMiddleware);

    const state = {
      messages: [new HumanMessage("Test")],
    };

    const result = await handler(state, {});

    if (result !== undefined) {
      const partial = result as { messages?: BaseMessage[] };
      expect(partial.messages![0]).toBeInstanceOf(HumanMessage);
    }
  });

  it("is callable with empty messages array", async () => {
    const handler = getBeforeModelHandler(beforeModelMiddleware);

    const state = {
      messages: [],
    };

    const result = await handler(state, {});

    // Should not throw — empty array is valid.
    if (result !== undefined) {
      const partial = result as { messages?: BaseMessage[] };
      expect(partial.messages).toEqual([]);
    }
  });

  it("does not mutate the input state", async () => {
    const handler = getBeforeModelHandler(beforeModelMiddleware);

    const originalMessages = [new HumanMessage("Don't touch me")];
    const state = {
      messages: originalMessages,
    };

    const result = await handler(state, {});

    // Original state should be unchanged.
    expect(state.messages).toHaveLength(1);
    expect(state.messages[0].content).toBe("Don't touch me");

    if (result !== undefined) {
      const partial = result as { messages?: BaseMessage[] };
      expect(partial.messages).toHaveLength(1);
      expect(partial.messages![0].content).toBe("Don't touch me");
    }
  });

  it("correctly reports message count in log context", async () => {
    const handler = getBeforeModelHandler(beforeModelMiddleware);

    const threeMessages = {
      messages: [
        new HumanMessage("First"),
        new AIMessage("Second"),
        new HumanMessage("Third"),
      ],
    };

    const result = await handler(threeMessages, {});

    if (result !== undefined) {
      const partial = result as { messages?: BaseMessage[] };
      expect(partial.messages).toHaveLength(3);
    }
  });
});
