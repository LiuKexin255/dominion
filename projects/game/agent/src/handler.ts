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
import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";
import type { AgentOperationResultFrame } from "../game_types/projects/game/AgentOperationResultFrame";
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
        image?: {
          data?: Uint8Array | string;
          encoding?: string;
          widthPx?: number;
          heightPx?: number;
        };
      } | null;
      operationResult?: {
        operationId?: string;
        status?: string | number;
        message?: string;
        screenshot?: {
          data?: Uint8Array | string;
          encoding?: string;
          widthPx?: number;
          heightPx?: number;
        } | null;
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
        sa.getBridge().handleResult(frame.operationResult as AgentOperationResultFrame);
        return;
      }

      if (frame.payload === "user_turn" || frame.userTurn) {
        const userTurn = frame.userTurn as {
          text?: string;
          image?: {
            data?: Uint8Array | string;
            encoding?: string;
            widthPx?: number;
            heightPx?: number;
          };
        };
        const userText = userTurn?.text ?? "";
        const image = userTurn?.image;

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

          const adapter = await sa.getOrCreateAdapter(
            effectiveProfileName,
            () => this.promptClient.getProfile(effectiveProfileName),
          );

          const turnContent: TurnContent = { text: userText };
          if (image?.data && image?.encoding) {
            turnContent.imageData = bytesToBase64String(image.data);
            turnContent.imageMimeType = encodingToMime(image.encoding);
            turnContent.imageWidthPx = image.widthPx;
            turnContent.imageHeightPx = image.heightPx;
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
      const createTime = checkpointTs
        ? timestampFromMs(new Date(checkpointTs).getTime())
        : undefined;
      const result: MessageProto[] = [];

      for (const msg of rawMessages) {
        const msgType = msg._getType?.() ?? "";

        if (msgType === "system") {
          continue;
        }

        if (msgType !== "human" && msgType !== "ai" && msgType !== "tool") {
          continue;
        }

        // Tool messages are best-effort reconstructed as operation_result
        // Messages so history renders identically to live operation results.
        if (msgType === "tool") {
          const operationResult = reconstructOperationResult(msg.content);
          if (operationResult) {
            result.push({
              name: `sessions/${sessionId}/agent/messages/${msg.id}`,
              messageId: msg.id,
              sender: FrameSender.FRAME_SENDER_SYSTEM,
              type: "operation_result",
              operationResult,
              createTime,
            });
          }
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
            const base64Data = extractBase64FromImageBlock(imgBlock);
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

        // AI tool_calls are best-effort reconstructed as operation Messages so
        // history renders identically to live agent operation requests.
        if (msgType === "ai") {
          const toolCalls = extractToolCalls(msg);
          for (let i = 0; i < toolCalls.length; i++) {
            const call = toolCalls[i];
            const operation = toolCallToOperationFrame(call);
            if (!operation) continue;
            const callId = call.id ?? String(i);
            result.push({
              name: `sessions/${sessionId}/agent/messages/${msg.id}-${callId}`,
              messageId: `${msg.id}-${callId}`,
              sender: FrameSender.FRAME_SENDER_AGENT,
              type: "operation",
              operation,
              createTime,
            });
          }
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

function bytesToBase64(value: Uint8Array): string {
  return Buffer.from(value).toString("base64");
}

function bytesToBase64String(data: Uint8Array | string): string {
  if (typeof data === "string") return data;
  return Buffer.from(data).toString("base64");
}

function encodingToMime(encoding: string): string {
  const subtype = encoding.replace(/^IMAGE_ENCODING_/, "").toLowerCase();
  return `image/${subtype || "png"}`;
}

function stripDataUrlPrefix(value: string): string {
  return value.replace(/^data:image\/[^;]+;base64,/, "");
}

function extractBase64FromImageBlock(block: any): string {
  const rawCandidates: unknown[] = [
    block?.data,
    block?.image_url?.url,
    block?.image,
    block?.source?.data,
  ];

  for (const raw of rawCandidates) {
    if (typeof raw === "string" && raw.length > 0) {
      return stripDataUrlPrefix(raw);
    }
    if (raw instanceof Uint8Array && raw.length > 0) {
      return bytesToBase64(raw);
    }
    if (Buffer.isBuffer(raw) && raw.length > 0) {
      return raw.toString("base64");
    }
  }
  return "";
}

// ---------------------------------------------------------------------------
// Operation history reconstruction helpers
//
// ListMessages best-effort reconstructs AgentOperationFrame / AgentOperation
// ResultFrame from LangChain message state so operation history renders
// identically to the live operation stream.
// ---------------------------------------------------------------------------

/** Minimal shape of a LangChain tool_call carried on an AIMessage. */
interface ToolCallLike {
  name?: string;
  args?: Record<string, unknown>;
  id?: string;
}

/** Minimal shape of a content block inside a BaseMessage content array. */
interface ContentBlockLike {
  type?: string;
  text?: string;
  reasoning?: string;
  image_url?: { url?: string };
  data?: unknown;
}

/** Matches the pixel-dimension annotation emitted by mouse tools. */
const PIXEL_SIZE_PATTERN = /图片像素尺寸[：:]?\s*(\d+)\s*[×xX*]\s*(\d+)/;

/** Extract tool_calls from a BaseMessage (AIMessage carries them directly). */
function extractToolCalls(msg: BaseMessage): ToolCallLike[] {
  const calls = (msg as unknown as { tool_calls?: unknown }).tool_calls;
  return Array.isArray(calls) ? (calls as ToolCallLike[]) : [];
}

/** Coerce a tool argument value to a finite int32, defaulting to 0. */
function toInt32(value: unknown): number {
  if (typeof value === "number" && Number.isFinite(value)) {
    return Math.trunc(value);
  }
  if (typeof value === "string") {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

/** Map a mouse_click click_type arg to the proto AgentMouseAction string. */
function clickTypeToAction(clickType: unknown): string {
  if (typeof clickType !== "string" || !clickType) {
    return "AGENT_MOUSE_ACTION_UNSPECIFIED";
  }
  return `AGENT_MOUSE_ACTION_${clickType.toUpperCase()}`;
}

/**
 * Best-effort map a LangChain tool_call to an AgentOperationFrame.
 *
 * Recognises the mouse_move and mouse_click tools; unknown tools return null
 * (no operation Message is emitted for them).
 */
function toolCallToOperationFrame(
  call: ToolCallLike,
): AgentOperationFrame | null {
  const name = call.name;
  const args = call.args ?? {};
  if (name === "mouse_move") {
    return {
      mouse: {
        action: "AGENT_MOUSE_ACTION_MOVE",
        xPx: toInt32(args.x_px),
        yPx: toInt32(args.y_px),
      },
    };
  }
  if (name === "mouse_click") {
    return {
      mouse: {
        action: clickTypeToAction(args.click_type),
        xPx: 0,
        yPx: 0,
      },
    };
  }
  return null;
}

/** Infer AgentOperationResultStatus from the result message text. */
function inferOperationStatus(message: string): string {
  const lower = message.toLowerCase();
  return lower.includes("ok") || lower.includes("succeeded")
    ? "AGENT_OPERATION_RESULT_STATUS_SUCCEEDED"
    : "AGENT_OPERATION_RESULT_STATUS_FAILED";
}

/**
 * Best-effort reconstruct an AgentOperationResultFrame from a ToolMessage's
 * content blocks.
 *
 * - text block (non-annotation) → message
 * - image_url block → screenshot.data (base64, data-url prefix stripped)
 * - pixel-size annotation text → screenshot widthPx / heightPx
 * - status inferred from message ("ok"/"succeeded" → SUCCEEDED, else FAILED)
 *
 * String content (when the ToolMessage carries a plain string) is used as the
 * message directly.
 */
function reconstructOperationResult(
  content: BaseMessage["content"],
): AgentOperationResultFrame | null {
  const blocks: ContentBlockLike[] = Array.isArray(content)
    ? (content as ContentBlockLike[])
    : [];

  let message = "";
  let screenshotData = "";
  let widthPx = 0;
  let heightPx = 0;

  for (const block of blocks) {
    if (block.type === "text" && typeof block.text === "string") {
      const dims = block.text.match(PIXEL_SIZE_PATTERN);
      if (dims) {
        widthPx = Number.parseInt(dims[1], 10) || 0;
        heightPx = Number.parseInt(dims[2], 10) || 0;
      } else if (!message) {
        message = block.text;
      }
    } else if (block.type === "image_url" && block.image_url?.url) {
      screenshotData = stripDataUrlPrefix(block.image_url.url);
    }
  }

  if (!message && typeof content === "string") {
    message = content;
  }

  const status = inferOperationStatus(message);

  const result: AgentOperationResultFrame = {
    status,
    message,
  };
  if (screenshotData) {
    result.screenshot = {
      data: screenshotData,
      widthPx,
      heightPx,
    };
  }
  return result;
}
