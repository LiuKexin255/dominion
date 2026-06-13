/**
 * runtime.ts — DialogRuntime: per-session dialog state machine.
 *
 * Manages one agent session: copies profile data at creation time,
 * maintains conversation history, queues messages while processing,
 * enforces strict FIFO ordering, and never calls LLM concurrently
 * for the same session.
 */

import { AIMessage, HumanMessage, SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { error } from "@dominion/common-js-logs";

import type { ContentBlock, LLMAdapter } from "./llm";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Role for conversation messages within a session. */
export type MessageRole = "user" | "agent" | "system";

/** A single message in the session conversation history. */
export interface ConversationMessage {
  role: MessageRole;
  content: string;
  turnId: string;
  timestamp: number;
}

/** Possible runtime statuses. */
export type RuntimeStatus = "idle" | "processing" | "failed";

/** A message waiting in the FIFO queue. */
interface QueuedMessage {
  text: string;
  turnId: string;
}

// ---------------------------------------------------------------------------
// DialogRuntime
// ---------------------------------------------------------------------------

export class DialogRuntime {
  readonly sessionId: string;
  readonly profileName: string;
  readonly copiedModel: string;
  readonly copiedSystemPrompt: string;

  /** Full session conversation history (chronological order). */
  history: ConversationMessage[] = [];

  /** FIFO queue of messages received while status=processing. */
  queue: QueuedMessage[] = [];

  status: RuntimeStatus = "idle";

  /** Updated on every message received and every processing completion. */
  lastActivityAt: number = Date.now();

  readonly createdAt: number;

  /** Set by delete() — marks instance for external cleanup. */
  private _deleted = false;

  private constructor(
    sessionId: string,
    profileName: string,
    copiedModel: string,
    copiedSystemPrompt: string,
  ) {
    this.sessionId = sessionId;
    this.profileName = profileName;
    this.copiedModel = copiedModel;
    this.copiedSystemPrompt = copiedSystemPrompt;
    this.createdAt = Date.now();
    this.lastActivityAt = this.createdAt;
  }

  // -----------------------------------------------------------------------
  // Factory
  // -----------------------------------------------------------------------

  /**
   * Create a new DialogRuntime by copying profile data.
   *
   * Data is copied at creation time — the instance is independent of
   * the original profile (deleting the profile does not affect the
   * active instance).
   */
  static createWithProfile(
    sessionId: string,
    profileName: string,
    model: string,
    systemPrompt: string,
  ): DialogRuntime {
    return new DialogRuntime(sessionId, profileName, model, systemPrompt);
  }

  // -----------------------------------------------------------------------
  // State machine
  // -----------------------------------------------------------------------

  /**
   * Process a user message through the dialog state machine.
   *
   * State transitions:
   * - idle + message → processing → (stream LLM) → idle → (drain queue)
   * - processing + message → enqueue → empty iterable
   * - LLM error → yield warning ContentBlock → idle → (drain queue)
   *
   * @returns Async iterable of ContentBlock in streaming order.
   *          Empty iterable when the message is queued.
   */
  async *processMessage(
    text: string,
    turnId: string,
    llmAdapter: LLMAdapter,
    providerSecret: string,
  ): AsyncIterable<ContentBlock> {
    // Guard: deleted instances reject new messages.
    if (this._deleted) {
      return;
    }

    // If already processing, enqueue and return empty iterable.
    if (this.status === "processing") {
      this.queue.push({ text, turnId });
      this.lastActivityAt = Date.now();
      return;
    }

    // Start processing.
    this.status = "processing";
    this.lastActivityAt = Date.now();

    // Convert existing history (before the current message) for the LLM.
    const llmHistory = this.toBaseMessages();
    // Record the user message in our conversation history.
    this.history.push({
      role: "user",
      content: text,
      turnId,
      timestamp: Date.now(),
    });

    try {
      let agentText = "";

      for await (const block of llmAdapter.generateTurn(
        this.copiedSystemPrompt,
        llmHistory,
        text,
        providerSecret,
      )) {
        yield block;
        if (block.type === "text") {
          agentText += block.text;
        }
      }

      // Record the agent response in history.
      this.history.push({
        role: "agent",
        content: agentText,
        turnId,
        timestamp: Date.now(),
      });

      this.status = "idle";
      this.lastActivityAt = Date.now();

      // Drain the queue: process the next queued message.
      const next = this.queue.shift();
      if (next) {
        yield* this.processMessage(
          next.text,
          next.turnId,
          llmAdapter,
          providerSecret,
        );
      }
    } catch (err: unknown) {
      const errorMsg =
        err instanceof Error ? err.message : "Unknown error";
      error("LLM generateTurn failed", { sessionId: this.sessionId, turnId, error: errorMsg });
      const warningBlock: ContentBlock = {
        type: "text",
        text: `Warning: ${errorMsg}`,
      };
      yield warningBlock;

      this.history.push({
        role: "system",
        content: `Error: ${errorMsg}`,
        turnId,
        timestamp: Date.now(),
      });

      // Recoverable: transition back to idle so queued messages can proceed.
      this.status = "idle";
      this.lastActivityAt = Date.now();

      const next = this.queue.shift();
      if (next) {
        yield* this.processMessage(
          next.text,
          next.turnId,
          llmAdapter,
          providerSecret,
        );
      }
    }
  }

  // -----------------------------------------------------------------------
  // Lifecycle
  // -----------------------------------------------------------------------

  /**
   * Check whether this instance is eligible for cleanup.
   *
   * Eligible when status is idle AND the inactivity duration exceeds
   * the given threshold.
   *
   * @param thresholdMs - Inactivity threshold in milliseconds.
   * @returns true if eligible for cleanup.
   */
  cleanup(thresholdMs: number): boolean {
    if (this.status !== "idle") {
      return false;
    }
    return Date.now() - this.lastActivityAt > thresholdMs;
  }

  /** Mark this instance for removal. */
  delete(): void {
    this._deleted = true;
  }

  isDeleted(): boolean {
    return this._deleted;
  }

  /** Return the current status string. */
  getStatus(): RuntimeStatus {
    return this.status;
  }

  // -----------------------------------------------------------------------
  // Internal helpers
  // -----------------------------------------------------------------------

  /**
   * Convert conversation history to LangChain BaseMessage array.
   * Excludes the last message if it is the current user message
   * (which is passed separately as userMessage to generateTurn).
   */
  private toBaseMessages(): BaseMessage[] {
    return this.history.map((msg): BaseMessage => {
      switch (msg.role) {
        case "user":
          return new HumanMessage(msg.content);
        case "agent":
          return new AIMessage(msg.content);
        case "system":
          return new SystemMessage(msg.content);
        default: {
          const _exhaustive: never = msg.role;
          return new SystemMessage(String(_exhaustive));
        }
      }
    });
  }
}
