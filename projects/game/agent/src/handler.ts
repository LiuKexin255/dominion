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

import type { BaseMessage } from "@langchain/core/messages";

import type { AgentServiceHandlers } from "../game_types/projects/game/AgentService";
import type { Agent as AgentMessage } from "../game_types/projects/game/Agent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { Message as MessageProto } from "../game_types/projects/game/Message";

import type { PromptClient } from "./prompt-client";
import type { SessionAgentStore } from "./session-agent";
import type { TurnContent } from "./llm";

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
  private heldMutexes: Set<string>;

  constructor(
    promptClient: PromptClient,
    sessionAgentStore: SessionAgentStore,
  ) {
    this.promptClient = promptClient;
    this.sessionAgentStore = sessionAgentStore;
    this.mutexes = new Map();
    this.heldMutexes = new Set();
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
    this.heldMutexes.add(sessionId);
    (this.mutexes as any)[`_release_${sessionId}`] = release;
  }

  private releaseMutex(sessionId: string): void {
    const release = (this.mutexes as any)[`_release_${sessionId}`];
    if (release) {
      this.heldMutexes.delete(sessionId);
      release();
    }
  }

  private isMutexHeld(sessionId: string): boolean {
    return this.heldMutexes.has(sessionId);
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
  // RefreshAgent
  // -----------------------------------------------------------------------

  RefreshAgent: grpc.handleUnaryCall<{ name?: string }, {}> = (
    call,
    callback,
  ) => {
    const sessionId = extractSessionId(call.request.name ?? "");
    info("refresh agent requested", { sessionId });

    if (this.isMutexHeld(sessionId)) {
      warn("refresh agent rejected: turn in-flight", { sessionId });
      callback({
        code: grpc.status.FAILED_PRECONDITION,
        details: "cannot refresh agent while a turn is in-flight",
      } as grpc.ServiceError);
      return;
    }

    const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
    sessionAgent.invalidateAdapter();
    info("refresh agent completed", { sessionId });

    callback(null, {});
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
      userTurn?: {
        text?: string;
        screenshot?: {
          screenshotId?: string;
          data?: string;
          encoding?: string;
          widthPx?: number;
          heightPx?: number;
        };
      } | null;
      operationResult?: {
        operationId?: string;
        status?: string | number;
        message?: string;
      } | null;
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

    // Track sessions whose bridge sink was registered on this stream. The
    // bridge is per-SessionAgent, so reconnect cleanup must iterate them all.
    const activeSessions = new Set<string>();
    const cleanupSinks = () => {
      for (const sid of activeSessions) {
        try {
          const sa = this.sessionAgentStore.getOrCreate(sid);
          sa.getBridge().unregisterSink();
        } catch {
        }
      }
      activeSessions.clear();
    };

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

      if (frame.payload === "operation_result" || frame.operationResult) {
        const sa = this.sessionAgentStore.getOrCreate(sessionId);
        sa.getBridge().handleResult(frame.operationResult as any);
        return;
      }

      if (frame.payload === "user_turn" || frame.userTurn) {
        const userTurn = frame.userTurn as {
          text?: string;
          screenshot?: {
            screenshotId?: string;
            data?: string;
            encoding?: string;
          };
        };
        const userText = userTurn?.text ?? "";
        const screenshot = userTurn?.screenshot;

        let effectiveProfileName = frame.agentProfileName ?? "";
        if (!effectiveProfileName) {
          const sa = this.sessionAgentStore.getOrCreate(sessionId);
          const state = sa.getAdapterState();
          if (state.isBound && state.activeProfileName) {
            effectiveProfileName = state.activeProfileName;
          }
        }

        if (!effectiveProfileName) {
          warn("no agent profile name for user_turn frame", {
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
          const sa = this.sessionAgentStore.getOrCreate(sessionId);

          sa.getBridge().registerSink((operationEnvelope: AgentFrame) => {
            stream.write(operationEnvelope);
          });
          activeSessions.add(sessionId);

          sa.getBridge().setCurrentScreenshotId(screenshot?.screenshotId ?? "");

          const adapter = await sa.getOrCreateAdapter(
            effectiveProfileName,
            () => this.promptClient.getProfile(effectiveProfileName),
          );

          const turnContent: TurnContent = { text: userText };
          if (screenshot?.data && screenshot?.encoding) {
            turnContent.screenshotId = screenshot.screenshotId;
            turnContent.screenshotData = screenshot.data;
            turnContent.screenshotMimeType = `image/${screenshot.encoding}`;
          }

          let blockCount = 0;
          for await (const block of adapter.generateTurn(
            sessionId,
            turnContent,
          )) {
            blockCount++;
            if (block.type === "reasoning") {
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

          info("user_turn processing completed", {
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
      cleanupSinks();
      try {
        stream.end();
      } catch {
        // Already closed.
      }
    });

    stream.on("end", () => {
      info("connect stream ended");
      cleanupSinks();
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
        const msgType = msg._getType?.() ?? "";

        if (msgType === "system") {
          continue;
        }

        if (msgType !== "human" && msgType !== "ai") {
          continue;
        }

        const sender =
          msgType === "human"
            ? FrameSender.FRAME_SENDER_USER
            : FrameSender.FRAME_SENDER_AGENT;

        const segments: { type: string; content: string }[] = [];

        if (typeof msg.content === "string") {
          segments.push({ type: "text", content: msg.content });
        } else if (Array.isArray(msg.content)) {
          const reasoningBlocks = msg.content.filter(
            (b: any) => b.type === "reasoning",
          );
          const textBlocks = msg.content.filter(
            (b: any) => b.type === "text",
          );
          const imageBlocks = msg.content.filter(
            (b: any) => b.type === "image" || b.type === "image_url",
          );

          const reasoning = reasoningBlocks
            .map((b: any) => b.reasoning ?? "")
            .join("\n");
          if (reasoning) {
            segments.push({ type: "thinking", content: reasoning });
          }

          const text = textBlocks
            .map((b: any) => b.text ?? "")
            .join("");
          if (text) {
            segments.push({ type: "text", content: text });
          }

          for (const imgBlock of imageBlocks) {
            const data = imgBlock.data ?? (imgBlock.image_url as any)?.url ?? "";
            const base64Data = data.replace(/^data:image\/[^;]+;base64,/, "");
            if (base64Data) {
              segments.push({ type: "image_data", content: base64Data });
            }
          }

          if (segments.length === 0) {
            const fallback = msg.content
              .map((b: any) => b.text ?? b.reasoning ?? "")
              .join("");
            if (fallback) {
              segments.push({ type: "text", content: fallback });
            }
          }
        }

        for (const seg of segments) {
          if (!seg.content && seg.type !== "text") continue;

          const multi = segments.length > 1;
          const segId = multi ? `${msg.id}-${seg.type}` : msg.id;

          const createTime = checkpointTs
            ? timestampFromMs(new Date(checkpointTs).getTime())
            : undefined;

          if (seg.type === "image_data") {
            result.push({
              name: `sessions/${sessionId}/agent/messages/${segId}`,
              messageId: segId,
              sender,
              type: "image",
              content: "imageData",
              imageData: seg.content,
              createTime,
            });
            continue;
          }

          result.push({
            name: `sessions/${sessionId}/agent/messages/${segId}`,
            messageId: segId,
            sender,
            type: seg.type,
            content: "text",
            text: seg.content,
            createTime,
          });
        }
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
