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

import type { ContentBlock, AgentAdapter, AdapterStateSnapshot } from "./llm";

export class FakeLlmAdapter implements AgentAdapter {
  private readonly graph;

  constructor(checkpointer: MemorySaver) {
    this.graph = new StateGraph(MessagesAnnotation)
      .addNode("pass", async () => ({}))
      .addEdge("__start__", "pass")
      .addEdge("pass", "__end__")
      .compile({ checkpointer });
  }

  async *generateTurn(
    threadId: string,
    userMessage: string,
  ): AsyncIterable<ContentBlock> {
    const reasoning = "Processing your message...";
    const text = `Hello! This is a simulated response. You said: ${userMessage}`;

    await this.graph.invoke(
      { messages: [new HumanMessage(userMessage)] },
      { configurable: { thread_id: threadId } },
    );

    yield { type: "reasoning", reasoning };
    yield { type: "text", text };

    await this.graph.invoke(
      {
        messages: [
          new AIMessage({
            content: [
              { type: "reasoning", reasoning },
              { type: "text", text },
            ],
          }),
        ],
      },
      { configurable: { thread_id: threadId } },
    );
  }

  async getState(threadId: string): Promise<AdapterStateSnapshot | null> {
    const snapshot = await this.graph.getState({
      configurable: { thread_id: threadId },
    });
    if (!snapshot) return null;
    return {
      values: snapshot.values ?? {},
      createdAt: snapshot.createdAt,
    };
  }
}
