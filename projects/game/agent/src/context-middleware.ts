/**
 * context-middleware.ts — ContextMiddleware for LangChain agent.
 *
 * Implements a beforeModel middleware hook that is identity-compatible:
 * receives agent state with messages and returns it unchanged.
 * Provides identifiable logging for debugging and observability.
 *
 * In future releases, this middleware will be the interception point for
 * context management, summarization, and message pruning.
 */

import { createMiddleware } from "langchain";
import type { AgentMiddleware } from "langchain";
import { info } from "@dominion/common-js-logs";
import type { BaseMessage } from "@langchain/core/messages";

/**
 * ContextMiddleware is a LangChain agent middleware that hooks the beforeModel
 * lifecycle event. In this release it is identity-compatible: the agent state
 * (including messages) is passed through without modification.
 *
 * The middleware is identifiable by its `name` property, making it replaceable
 * in the middleware array during agent construction.
 *
 * @example
 * ```typescript
 * import { createAgent } from "langchain";
 * import { beforeModelMiddleware } from "./context-middleware";
 *
 * const agent = createAgent({
 *   model: chatModel,
 *   middleware: [beforeModelMiddleware],
 * });
 */
export const beforeModelMiddleware: AgentMiddleware<any, any, any, any, any> = createMiddleware({
  name: "ContextMiddleware",

  /**
   * beforeModel hook — called before each model invocation.
   *
   * @param state  - The current agent state (includes messages and other built-in fields).
   * @param _runtime  - Runtime context (unused in identity-compatible release).
   * @returns Partial state update (identity pass-through in this release).
   */
  beforeModel: async (
    state: { messages: BaseMessage[] },
    _runtime: unknown,
  ) => {
    const count = state.messages?.length ?? 0;
    info("ContextMiddleware beforeModel invoked", {
      messageCount: count,
    });

    // Identity-compatible: explicitly return messages unchanged.
    return { messages: state.messages };
  },
});
