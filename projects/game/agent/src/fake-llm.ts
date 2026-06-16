/**
 * fake-llm.ts — Deterministic fake AgentAdapter for testing.
 *
 * Returns hardcoded responses without any real API calls.  Persists
 * exchanged messages to the supplied MemorySaver so that ListMessages
 * and reconnect history work in large tests.
 */

import { HumanMessage, AIMessage } from "@langchain/core/messages";
import {
  MemorySaver,
  MessagesAnnotation,
  StateGraph,
} from "@langchain/langgraph";

import type { ContentBlock, AgentAdapter } from "./llm";

export class FakeLlmAdapter implements AgentAdapter {
  constructor(private checkpointer: MemorySaver) {}

  async *generateTurn(
    threadId: string,
    userMessage: string,
  ): AsyncIterable<ContentBlock> {
    const reasoning = "Processing your message...";
    const text = `Hello! This is a simulated response. You said: ${userMessage}`;

    await saveMessages(this.checkpointer, threadId, [
      new HumanMessage(userMessage),
    ]);

    yield { type: "reasoning", reasoning };
    yield { type: "text", text };

    await saveMessages(this.checkpointer, threadId, [
      new AIMessage({
        content: [
          { type: "reasoning", reasoning },
          { type: "text", text },
        ],
      }),
    ]);
  }
}

async function saveMessages(
  checkpointer: MemorySaver,
  threadId: string,
  messages: Array<HumanMessage | AIMessage>,
): Promise<void> {
  const graph = new StateGraph(MessagesAnnotation)
    .addNode("pass", async () => ({}))
    .addEdge("__start__", "pass")
    .addEdge("pass", "__end__")
    .compile({ checkpointer });
  await graph.invoke(
    { messages },
    { configurable: { thread_id: threadId } },
  );
}
