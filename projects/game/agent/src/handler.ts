/**
 * handler.ts — AgentServiceServer gRPC handler implementations.
 *
 * Implements GetAgent, ListMessages, and Connect RPCs for the AgentService
 * defined in game.proto.
 *
 * The handler delegates adapter lifecycle to SessionAgentStore.  Each
 * SessionAgent owns its adapter and manages profile binding/switching.
 */

import * as grpc from "@grpc/grpc-js";
import { randomUUID } from "node:crypto";
import { info, warn, error } from "@dominion/common-js-logs";

import { HumanMessage, AIMessage, SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";

import type { AgentServiceHandlers } from "../game_types/projects/game/AgentService";
import type { Agent as AgentMessage } from "../game_types/projects/game/Agent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { Message as MessageProto } from "../game_types/projects/game/Message";

import type { PromptClient } from "./prompt-client";
import type { SessionAgentStore } from "./session-agent";

const FrameSender = {
  FRAME_SENDER_UNSPECIFIED: "FRAME_SENDER_UNSPECIFIED",
  FRAME_SENDER_USER: "FRAME_SENDER_USER",
  FRAME_SENDER_AGENT: "FRAME_SENDER_AGENT",
  FRAME_SENDER_SYSTEM: "FRAME_SENDER_SYSTEM",
} as const;

export class Handler implements AgentServiceHandlers {
  [name: string]: any;

  private promptClient: PromptClient;
  private sessionAgentStore: SessionAgentStore;
  private mutexes: Map<string, Promise<void>>;

  constructor(
    promptClient: PromptClient,
    sessionAgentStore: SessionAgentStore,
  ) {
    this.promptClient = promptClient;
    this.sessionAgentStore = sessionAgentStore;
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
    const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
    const state = sessionAgent.getAdapterState();

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
    let currentInvokeId = "";
    let sequence = 0;

    const nextSequence = (invokeId: string): number => {
      if (invokeId !== currentInvokeId) {
        currentInvokeId = invokeId;
        sequence = 0;
      }
      return sequence++;
    };

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

      if (frame.payload === "status" || frame.status) {
        const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
        const state = sessionAgent.getAdapterState();
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

      if (
        frame.payload === "screenshot" ||
        frame.screenshot ||
        frame.payload === "operation" ||
        frame.operation
      ) {
        return;
      }

      const senderValue =
        typeof frame.sender === "string"
          ? frame.sender
          : FrameSender.FRAME_SENDER_USER;
      if (
        (frame.payload === "text" || frame.text) &&
        senderValue === FrameSender.FRAME_SENDER_USER
      ) {
        const userText = frame.text?.content ?? "";

        let effectiveProfileName = frame.agentProfileName ?? "";
        if (!effectiveProfileName) {
          const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
          const state = sessionAgent.getAdapterState();
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
          const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);

          const adapter = await sessionAgent.getOrCreateAdapter(
            effectiveProfileName,
            () => this.promptClient.getProfile(effectiveProfileName),
          );

          let blockCount = 0;
          for await (const block of adapter.generateTurn(
            sessionId,
            userText,
          )) {
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

          {
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
      try {
        stream.end();
      } catch {
        // Already closed.
      }
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

    const sessionId = extractSessionId(parent);

    try {
      const sessionAgent = this.sessionAgentStore.get(sessionId);
      const adapter = sessionAgent?.getAdapter();

      if (!adapter) {
        callback(null, { messages: [] });
        return;
      }

      const state = await adapter.getState(sessionId);

      const rawMessages: BaseMessage[] = state?.values?.messages ?? [];
      const checkpointTs: string | undefined = state?.createdAt as
        | string
        | undefined;
      const result: MessageProto[] = [];

      for (const msg of rawMessages) {
        if (msg instanceof SystemMessage) {
          continue;
        }

        if (!(msg instanceof HumanMessage) && !(msg instanceof AIMessage)) {
          continue;
        }

        const sender =
          msg instanceof HumanMessage
            ? FrameSender.FRAME_SENDER_USER
            : FrameSender.FRAME_SENDER_AGENT;

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

        if (!content && type !== "text") continue;

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

function extractSessionId(parent: string): string {
  const match = parent.match(/^sessions\/([^/]+?)(?:\/agent)?$/);
  return match ? match[1] : parent;
}
