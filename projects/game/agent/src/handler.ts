/**
 * handler.ts — AgentServiceServer gRPC handler implementations.
 *
 * Implements CreateAgent, GetAgent, DeleteAgent, ListMessages, and Connect
 * RPCs for the AgentService defined in game.proto using a checkpoint-native
 * lifecycle: metadata replaces DialogRuntime, and all conversation state
 * lives in a shared MemorySaver keyed by sessionId.
 */

import * as grpc from "@grpc/grpc-js";
import { randomUUID } from "node:crypto";
import { info, warn, error } from "@dominion/common-js-logs";

import { HumanMessage, AIMessage, SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";

import type { AgentServiceHandlers } from "../game_types/projects/game/AgentService";
import type { Agent as AgentMessage } from "../game_types/projects/game/Agent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { Message as MessageProto } from "../game_types/projects/game/Message";

import type { LLMAdapter } from "./llm";
import type { PromptClient } from "./prompt-client";

// FrameSender enum values duplicated from proto because generated game_types/
// only provides .ts type files (not compiled .js); importing a runtime value
// would fail at require() time.
const FrameSender = {
  FRAME_SENDER_UNSPECIFIED: "FRAME_SENDER_UNSPECIFIED",
  FRAME_SENDER_USER: "FRAME_SENDER_USER",
  FRAME_SENDER_AGENT: "FRAME_SENDER_AGENT",
  FRAME_SENDER_SYSTEM: "FRAME_SENDER_SYSTEM",
} as const;

// ---------------------------------------------------------------------------
// AgentMetadata — lightweight snapshot of agent profile at creation time
// ---------------------------------------------------------------------------

interface AgentMetadata {
  sessionId: string;
  name: string;
  agentProfileName: string;
  model: string;
  systemPrompt: string;
  createTime: number;
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

export class Handler implements AgentServiceHandlers {
  /** Index signature required by grpc.UntypedServiceImplementation. */
  [name: string]: any;

  /** Prompt service client for fetching agent profiles. */
  private promptClient: PromptClient;

  /** LLM adapter for generating agent responses. */
  private llmAdapter: LLMAdapter;

  /** Shared in-memory checkpointer. One instance for all sessions. */
  private checkpointer: MemorySaver;

  /** Compiled StateGraph for reading checkpoint state (ListMessages). */
  private graph: any;

  /** Provider API key / secret. */
  private providerSecret: string;

  /** Lightweight agent metadata keyed by session ID. */
  private metadata: Map<string, AgentMetadata>;

  /** Same-session execution mutexes (FIFO promise chains). */
  private mutexes: Map<string, Promise<void>>;

  constructor(
    promptClient: PromptClient,
    llmAdapter: LLMAdapter,
    checkpointer: MemorySaver,
    graph: any,
    providerSecret: string,
  ) {
    this.promptClient = promptClient;
    this.llmAdapter = llmAdapter;
    this.checkpointer = checkpointer;
    this.graph = graph;
    this.providerSecret = providerSecret;
    this.metadata = new Map();
    this.mutexes = new Map();
  }

  // -----------------------------------------------------------------------
  // Same-session mutex helpers (FIFO, non-reentrant)
  // -----------------------------------------------------------------------

  private async acquireMutex(sessionId: string): Promise<void> {
    const prev = this.mutexes.get(sessionId) ?? Promise.resolve();
    let release!: () => void;
    const next = new Promise<void>((r) => {
      release = r;
    });
    this.mutexes.set(sessionId, prev.then(() => next));
    await prev;
    (this.mutexes as any)[`_release_${sessionId}`] = release;
  }

  private releaseMutex(sessionId: string): void {
    const release = (this.mutexes as any)[`_release_${sessionId}`];
    if (release) release();
  }

  // -----------------------------------------------------------------------
  // CreateAgent
  // -----------------------------------------------------------------------

  CreateAgent: grpc.handleUnaryCall<
    { sessionId?: string; agentProfileName?: string },
    AgentMessage
  > = async (call, callback) => {
    const sessionId = call.request.sessionId ?? "";
    const profileName = call.request.agentProfileName ?? "";

    if (!profileName) {
      return callback({
        code: grpc.status.NOT_FOUND,
        details: "agent_profile_name is required",
      } as grpc.ServiceError);
    }

    try {
      const profile = await this.promptClient.getProfile(profileName);

      const meta: AgentMetadata = {
        sessionId,
        name: `sessions/${sessionId}/agent`,
        agentProfileName: profileName,
        model: profile.model,
        systemPrompt: profile.systemPrompt,
        createTime: Date.now(),
      };
      this.metadata.set(sessionId, meta);

      const agent: AgentMessage = {
        name: meta.name,
        sessionId,
        agentProfileName: profileName,
        createTime: timestampFromMs(meta.createTime),
      };

      callback(null, agent);
    } catch (err: unknown) {
      const details =
        err instanceof Error ? err.message : "Failed to create agent";
      error("create agent failed", { sessionId, profileName, error: details });
      const code =
        err instanceof Error && "code" in err
          ? (err as grpc.ServiceError).code
          : grpc.status.INTERNAL;
      callback({
        code:
          code === grpc.status.NOT_FOUND
            ? grpc.status.NOT_FOUND
            : grpc.status.INTERNAL,
        details:
          err instanceof Error ? err.message : "Failed to create agent",
      } as grpc.ServiceError);
    }
  };

  // -----------------------------------------------------------------------
  // GetAgent
  // -----------------------------------------------------------------------

  GetAgent: grpc.handleUnaryCall<{ sessionId?: string }, AgentMessage> = (
    call,
    callback,
  ) => {
    const sessionId = call.request.sessionId ?? "";
    const meta = this.metadata.get(sessionId);

    if (!meta) {
      return callback({
        code: grpc.status.NOT_FOUND,
        details: `Agent not found for session: ${sessionId}`,
      } as grpc.ServiceError);
    }

    const agent: AgentMessage = {
      name: meta.name,
      sessionId,
      agentProfileName: meta.agentProfileName,
      createTime: timestampFromMs(meta.createTime),
    };

    callback(null, agent);
  };

  // -----------------------------------------------------------------------
  // DeleteAgent
  // -----------------------------------------------------------------------

  DeleteAgent: grpc.handleUnaryCall<{ sessionId?: string }, {}> = async (
    call,
    callback,
  ) => {
    const sessionId = call.request.sessionId ?? "";

    await this.acquireMutex(sessionId);
    try {
      if (this.metadata.has(sessionId)) {
        this.metadata.delete(sessionId);
        await this.checkpointer.deleteThread(sessionId);
      }
      // Idempotent: missing metadata = success.
      callback(null, {});
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to delete agent";
      error("delete agent failed", { sessionId, error: message });
      callback({
        code: grpc.status.INTERNAL,
        details: message,
      } as grpc.ServiceError);
    } finally {
      this.releaseMutex(sessionId);
    }
  };

  // -----------------------------------------------------------------------
  // Connect (bidirectional streaming)
  // -----------------------------------------------------------------------

  Connect: grpc.handleBidiStreamingCall<
    {
      sessionId?: string;
      frameId?: string;
      invokeId?: string;
      sequence?: number | string;
      sender?: string | number;
      payload?: string;
      text?: { content?: string } | null;
      status?: { status?: string } | null;
      screenshot?: unknown;
      echo?: unknown;
      operation?: unknown;
    },
    AgentFrame
  > = (stream) => {
    /** Tracks sequence number per invoke (turnId). */
    let currentInvokeId = "";
    let sequence = 0;

    /** Ensure sequence increments per frame within the same invoke. */
    const nextSequence = (invokeId: string): number => {
      if (invokeId !== currentInvokeId) {
        currentInvokeId = invokeId;
        sequence = 0;
      }
      return sequence++;
    };

    /** Build an AgentFrame with standard metadata. */
    const buildFrame = (
      sessionId: string,
      invokeId: string,
      sender: (typeof FrameSender)[keyof typeof FrameSender],
      payload: Partial<AgentFrame>,
    ): AgentFrame => ({
      sessionId,
      frameId: randomUUID(),
      invokeId,
      sequence: nextSequence(invokeId),
      sender,
      createTime: timestampNow(),
      ...payload,
    });

    stream.on("data", async (frame) => {
      const sessionId = frame.sessionId ?? "";
      const invokeId = frame.invokeId ?? "";

      // --- status payload: connection probe ---
      if (frame.payload === "status" || frame.status) {
        const meta = this.metadata.get(sessionId);
        const statusFrame: AgentFrame = buildFrame(
          sessionId,
          invokeId,
          FrameSender.FRAME_SENDER_SYSTEM,
          {
            status: {
              status: meta ? "idle" : "unknown",
            },
          },
        );
        stream.write(statusFrame);
        return;
      }

      // --- echo payload: echo back ---
      if (frame.payload === "echo" || frame.echo) {
        const echoData =
          frame.echo && typeof frame.echo === "object" && "data" in frame.echo
            ? (frame.echo as { data?: string }).data ?? ""
            : "";
        const echoFrame: AgentFrame = buildFrame(
          sessionId,
          invokeId,
          FrameSender.FRAME_SENDER_SYSTEM,
          {
            echo: { data: echoData },
          },
        );
        stream.write(echoFrame);
        return;
      }

      // --- deprecated payloads: silently ignore ---
      if (
        frame.payload === "screenshot" ||
        frame.screenshot ||
        frame.payload === "operation" ||
        frame.operation
      ) {
        return;
      }

      // --- text payload from user ---
      const senderValue =
        typeof frame.sender === "string"
          ? frame.sender
          : FrameSender.FRAME_SENDER_USER;
      if (
        (frame.payload === "text" || frame.text) &&
        senderValue === FrameSender.FRAME_SENDER_USER
      ) {
        const userText = frame.text?.content ?? "";
        const meta = this.metadata.get(sessionId);

        if (!meta) {
          warn("metadata not found for text frame", { sessionId, invokeId });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              warn: {
                message: `No agent found for session: ${sessionId}`,
              },
            },
          );
          stream.write(warnFrame);
          return;
        }

        await this.acquireMutex(sessionId);
        try {
          const contentIter = this.llmAdapter.generateTurn(
            meta.model,
            meta.systemPrompt,
            sessionId,
            userText,
            this.checkpointer,
            this.providerSecret,
          );

          let blockCount = 0;
          for await (const block of contentIter) {
            blockCount++;
            if (block.type === "reasoning") {
              info("writing thinking frame", {
                sessionId,
                invokeId,
                length: block.reasoning.length,
              });
              const thinkFrame: AgentFrame = buildFrame(
                sessionId,
                invokeId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  thinking: { content: block.reasoning },
                },
              );
              stream.write(thinkFrame);
            } else if (block.type === "text") {
              info("writing text frame", {
                sessionId,
                invokeId,
                length: block.text.length,
                preview: block.text.slice(0, 100),
              });
              const textFrame: AgentFrame = buildFrame(
                sessionId,
                invokeId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  text: { content: block.text },
                },
              );
              stream.write(textFrame);
            }
          }

          // Emit wait frame after all response frames.
          info("text processing completed", {
            sessionId,
            invokeId,
            blockCount,
          });
          const waitFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              wait: {},
            },
          );
          stream.write(waitFrame);
        } catch (err: unknown) {
          const message =
            err instanceof Error ? err.message : "Processing error";
          error("LLM processing failed", {
            sessionId,
            invokeId,
            error: message,
          });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              warn: { message: `Processing error: ${message}` },
            },
          );
          stream.write(warnFrame);

          const waitFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            { wait: {} },
          );
          stream.write(waitFrame);
        } finally {
          this.releaseMutex(sessionId);
        }
      }
    });

    stream.on("error", (err: Error) => {
      error("connect stream error", { error: err.message });
      // Unrecoverable: end the stream.
      stream.end();
    });

    stream.on("end", () => {
      info("connect stream ended");
    });
  };

  // -----------------------------------------------------------------------
  // ListMessages
  // -----------------------------------------------------------------------

  ListMessages: grpc.handleUnaryCall<
    { parent?: string },
    { messages?: MessageProto[] }
  > = async (call, callback) => {
    const parent = call.request.parent ?? "";

    // Extract sessionId from parent resource name "sessions/{id}/agent".
    const sessionId = extractSessionId(parent);
    const meta = this.metadata.get(sessionId);

    if (!meta) {
      return callback({
        code: grpc.status.NOT_FOUND,
        details: `Agent not found for session: ${sessionId}`,
      } as grpc.ServiceError);
    }

    try {
      const state = await this.graph.getState({
        configurable: { thread_id: sessionId },
      });

      const rawMessages: BaseMessage[] = state?.values?.messages ?? [];
      const result: MessageProto[] = [];

      for (const msg of rawMessages) {
        // Skip system control messages (empty content, tool results, etc.)
        if (!msg.content && !(msg instanceof HumanMessage || msg instanceof AIMessage)) {
          continue;
        }

        // Determine sender.
        let sender: typeof FrameSender[keyof typeof FrameSender];
        if (msg instanceof HumanMessage) {
          sender = FrameSender.FRAME_SENDER_USER;
        } else if (msg instanceof AIMessage) {
          sender = FrameSender.FRAME_SENDER_AGENT;
        } else if (msg instanceof SystemMessage) {
          sender = FrameSender.FRAME_SENDER_SYSTEM;
        } else {
          sender = FrameSender.FRAME_SENDER_SYSTEM;
        }

        // Determine type and content.
        let type = "text";
        let content = "";

        if (typeof msg.content === "string") {
          content = msg.content;
          // System messages that look like errors are "warn" type.
          if (msg instanceof SystemMessage || sender === FrameSender.FRAME_SENDER_SYSTEM) {
            type = "warn";
          }
        } else if (Array.isArray(msg.content)) {
          const reasoningBlocks = msg.content.filter(
            (b: any) => b.type === "reasoning",
          );
          const textBlocks = msg.content.filter(
            (b: any) => b.type === "text",
          );

          if (reasoningBlocks.length > 0 && textBlocks.length === 0) {
            type = "thinking";
            content = reasoningBlocks
              .map((b: any) => b.reasoning ?? "")
              .join("\n");
          } else {
            type = "text";
            content = msg.content
              .map((b: any) => b.text ?? b.reasoning ?? "")
              .join("");
          }
        }

        // Skip empty messages.
        if (!content && type !== "text") continue;

        // Best-effort timestamp from checkpoint snapshot.
        const createTime = state?.createdAt
          ? timestampFromMs(state.createdAt)
          : undefined;

        result.push({
          name: `sessions/${sessionId}/agent/messages/${msg.id}`,
          messageId: msg.id,
          sender,
          type,
          content,
          createTime,
        });
      }

      callback(null, { messages: result });
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to list messages";
      error("list messages failed", { sessionId, error: message });
      callback({
        code: grpc.status.INTERNAL,
        details: message,
      } as grpc.ServiceError);
    }
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function timestampNow(): { seconds: number; nanos: number } {
  const ms = Date.now();
  return {
    seconds: Math.floor(ms / 1000),
    nanos: (ms % 1000) * 1_000_000,
  };
}

function timestampFromMs(ms: number): { seconds: number; nanos: number } {
  return {
    seconds: Math.floor(ms / 1000),
    nanos: (ms % 1000) * 1_000_000,
  };
}

/**
 * Extract sessionId from a parent resource name like "sessions/{id}/agent".
 * Returns the {id} portion, or the full string if it doesn't match the pattern.
 */
function extractSessionId(parent: string): string {
  const match = parent.match(/^sessions\/(.+?)\/agent$/);
  return match ? match[1] : parent;
}
