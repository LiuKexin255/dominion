/**
 * handler.ts — AgentServiceServer gRPC handler implementations.
 *
 * Implements GetAgent, ListMessages, and Connect RPCs for the AgentService
 * defined in game.proto using a SessionAgent/AgentAdapter model.
 *
 * CreateAgent and DeleteAgent have been removed — agent binding is managed
 * on-demand by AdapterManager during Connect, and adapter state is queried
 * via GetAgent.  All conversation state lives in a shared MemorySaver
 * keyed by sessionId.
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

import type { PromptClient } from "./prompt-client";
import type { Connection } from "./connection-registry";
import { ConnectionRegistry } from "./connection-registry";
import { AdapterManager } from "./adapter-manager";

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
// Handler
// ---------------------------------------------------------------------------

export class Handler implements AgentServiceHandlers {
  /** Index signature required by grpc.UntypedServiceImplementation. */
  [name: string]: any;

  /** Prompt service client for fetching agent profiles. */
  private promptClient: PromptClient;

  /** Per-session connection registry (kick + alive checks). */
  private connectionRegistry: ConnectionRegistry;

  /** Adapter lifecycle manager (bind/unbind on profile switch). */
  private adapterManager: AdapterManager;

  /** Shared in-memory checkpointer. One instance for all sessions. */
  private checkpointer: MemorySaver;

  /** Compiled StateGraph for reading checkpoint state (ListMessages). */
  private graph: any;

  /** Provider API key / secret. */
  private providerSecret: string;

  /** Same-session execution mutexes (FIFO promise chains). */
  private mutexes: Map<string, Promise<void>>;

  constructor(
    promptClient: PromptClient,
    adapterManager: AdapterManager,
    connectionRegistry: ConnectionRegistry,
    checkpointer: MemorySaver,
    graph: any,
    providerSecret: string,
  ) {
    this.promptClient = promptClient;
    this.adapterManager = adapterManager;
    this.connectionRegistry = connectionRegistry;
    this.checkpointer = checkpointer;
    this.graph = graph;
    this.providerSecret = providerSecret;
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
  // GetAgent
  // -----------------------------------------------------------------------

  GetAgent: grpc.handleUnaryCall<{ sessionId?: string }, AgentMessage> = (
    call,
    callback,
  ) => {
    const sessionId = call.request.sessionId ?? "";
    const state = this.adapterManager.getAdapterState(sessionId);

    const agent: AgentMessage = {
      name: `sessions/${sessionId}/agent`,
      sessionId,
      agentProfileName: state.activeProfileName ?? "",
      createTime: timestampNow(),
    };

    callback(null, agent);
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
      agentProfileName?: string;
    },
    AgentFrame
  > = (stream) => {
    /** Tracks sequence number per invoke (turnId). */
    let currentInvokeId = "";
    let sequence = 0;

    /** Registered sessionId for this stream (set on first frame). */
    let registeredSessionId: string | null = null;

    /** Wraps the gRPC stream duplex as a Connection for the registry. */
    const connection: Connection = {
      close() {
        try {
          (stream as any).end();
        } catch {
          // Stream may already be closed.
        }
      },
    };

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

      // Register this stream with the session on first frame.
      if (registeredSessionId === null && sessionId) {
        registeredSessionId = sessionId;
        await this.connectionRegistry.register(sessionId, connection);
      }

      // --- status payload: connection probe ---
      if (frame.payload === "status" || frame.status) {
        const state = this.adapterManager.getAdapterState(sessionId);
        const statusFrame: AgentFrame = buildFrame(
          sessionId,
          invokeId,
          FrameSender.FRAME_SENDER_SYSTEM,
          {
            status: {
              status: state.isBound ? "idle" : "unknown",
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

        // Determine effective profile name for this turn.
        let effectiveProfileName = frame.agentProfileName ?? "";
        if (!effectiveProfileName) {
          const state = this.adapterManager.getAdapterState(sessionId);
          if (state.isBound && state.activeProfileName) {
            effectiveProfileName = state.activeProfileName;
          }
        }

        if (!effectiveProfileName) {
          warn("no agent profile name for text frame", {
            sessionId,
            invokeId,
          });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              warn: {
                message: "agent_profile_name required",
              },
            },
          );
          stream.write(warnFrame);
          return;
        }

        await this.acquireMutex(sessionId);
        try {
          // Validate profile exists before creating adapter.
          let profile: { model: string; systemPrompt: string };
          try {
            profile = await this.promptClient.getProfile(effectiveProfileName);
          } catch (err: unknown) {
            const details =
              err instanceof Error ? err.message : "Profile not found";
            warn("agent profile not found, discarding user text", {
              sessionId,
              invokeId,
              profileName: effectiveProfileName,
              error: details,
            });
            const warnFrame: AgentFrame = buildFrame(
              sessionId,
              invokeId,
              FrameSender.FRAME_SENDER_SYSTEM,
              {
                warn: {
                  message: `Agent profile not found: ${effectiveProfileName}`,
                },
              },
            );
            stream.write(warnFrame);
            // Do NOT write to history — return without calling generateTurn.
            return;
          }

          // Get or create adapter for this session+profile.
          const adapter = await this.adapterManager.getOrCreateAdapter(
            sessionId,
            effectiveProfileName,
            this.promptClient,
            this.checkpointer,
          );

          // Stream content blocks from the adapter.
          let blockCount = 0;
          for await (const block of adapter.generateTurn(
            profile.model,
            profile.systemPrompt,
            sessionId,
            userText,
            this.checkpointer,
            this.providerSecret,
          )) {
            // Check if this connection has been kicked mid-stream.
            if (!this.connectionRegistry.isAlive(sessionId, connection)) {
              info("connection kicked mid-stream", {
                sessionId,
                invokeId,
                blockCount,
              });
              break;
            }

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
                  agentProfileName: effectiveProfileName,
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
                  agentProfileName: effectiveProfileName,
                  text: { content: block.text },
                },
              );
              stream.write(textFrame);
            }
          }

          // Emit wait frame after all response frames (if still alive).
          if (this.connectionRegistry.isAlive(sessionId, connection)) {
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
                agentProfileName: effectiveProfileName,
                wait: {},
              },
            );
            stream.write(waitFrame);
          }
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
            { agentProfileName: effectiveProfileName, wait: {} },
          );
          stream.write(waitFrame);
        } finally {
          this.releaseMutex(sessionId);
        }
      }
    });

    stream.on("error", (err: Error) => {
      error("connect stream error", { error: err.message });
      if (registeredSessionId) {
        this.connectionRegistry.unregister(registeredSessionId, connection);
      }
      try {
        stream.end();
      } catch {
        // Already closed.
      }
    });

    stream.on("end", () => {
      info("connect stream ended");
      if (registeredSessionId) {
        this.connectionRegistry.unregister(registeredSessionId, connection);
      }
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

    try {
      // Read checkpoint state for this thread via the compiled graph.
      const state: any = await this.graph.getState({
        configurable: { thread_id: sessionId },
      });

      const rawMessages: BaseMessage[] = state?.values?.messages ?? [];
      const checkpointTs: string | undefined = state?.createdAt as
        | string
        | undefined;
      const result: MessageProto[] = [];

      for (const msg of rawMessages) {
        // Skip system messages.
        if (msg instanceof SystemMessage) {
          continue;
        }

        // Only include HumanMessage and AIMessage.
        if (!(msg instanceof HumanMessage) && !(msg instanceof AIMessage)) {
          continue;
        }

        // Determine sender.
        const sender =
          msg instanceof HumanMessage
            ? FrameSender.FRAME_SENDER_USER
            : FrameSender.FRAME_SENDER_AGENT;

        // Determine type and content.
        let type = "text";
        let content = "";

        if (typeof msg.content === "string") {
          content = msg.content;
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
        const createTime = checkpointTs
          ? timestampFromMs(new Date(checkpointTs).getTime())
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
 * Extract sessionId from a parent resource name like "sessions/{id}" or "sessions/{id}/agent".
 * Returns the {id} portion, or the full string if it doesn't match the pattern.
 */
function extractSessionId(parent: string): string {
  const match = parent.match(/^sessions\/([^/]+?)(?:\/agent)?$/);
  return match ? match[1] : parent;
}
