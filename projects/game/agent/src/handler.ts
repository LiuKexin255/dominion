/**
 * handler.ts — AgentServiceServer gRPC handler implementations.
 *
 * Implements GetAgent, ListMessages, and Connect RPCs for the AgentService
 * defined in game.proto.
 *
 * The handler delegates adapter lifecycle to SessionAgentStore.  Each
 * SessionAgent owns its adapter and manages profile binding/switching.
 *
 * Frame contract (Part model): every AgentFrame carries exactly one payload —
 * a PartBlock of content OR a single control signal (wait/warn/status).  User
 * turns and agent output are both content frames distinguished only by
 * `sender`; tool results are content frames carrying a ToolResultPart.
 */

import * as grpc from "@grpc/grpc-js";
import { randomUUID } from "node:crypto";
import { info, warn, error } from "@dominion/common-js-logs";

import type { BaseMessage } from "@langchain/core/messages";

import type { AgentServiceHandlers } from "../game_types/projects/game/AgentService";
import type { Agent as AgentMessage } from "../game_types/projects/game/Agent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { Part } from "../game_types/projects/game/Part";
import type { ImagePart } from "../game_types/projects/game/ImagePart";
import type { MouseMovePart } from "../game_types/projects/game/MouseMovePart";
import type { MouseClickPart } from "../game_types/projects/game/MouseClickPart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type { Message as MessageProto } from "../game_types/projects/game/Message";

import type { PromptClient } from "./prompt-client";
import type { SessionAgentStore } from "./session-agent";
import type { TurnContent } from "./llm";

/**
 * FrameSender enum values (proto string literals). Defined locally rather
 * than imported as a value so the handler has no runtime dependency on the
 * generated game_types modules (which are not resolvable from the test
 * runfiles tree); all other game_types references are type-only.
 */
const FrameSender = {
  FRAME_SENDER_UNSPECIFIED: "FRAME_SENDER_UNSPECIFIED",
  FRAME_SENDER_USER: "FRAME_SENDER_USER",
  FRAME_SENDER_AGENT: "FRAME_SENDER_AGENT",
  FRAME_SENDER_SYSTEM: "FRAME_SENDER_SYSTEM",
} as const;

const StatusSignalStatus = {
  STATUS_SIGNAL_STATUS_UNSPECIFIED: "STATUS_SIGNAL_STATUS_UNSPECIFIED",
  STATUS_SIGNAL_STATUS_ACTIVE: "STATUS_SIGNAL_STATUS_ACTIVE",
  STATUS_SIGNAL_STATUS_IDLE: "STATUS_SIGNAL_STATUS_IDLE",
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

  GetAgent: grpc.handleUnaryCall<{ name?: string }, AgentMessage> = (
    call,
    callback,
  ) => {
    const name = call.request.name ?? "";
    const sessionId = extractSessionId(name);
    const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
    const state = sessionAgent.getAdapterState();

    const agent: AgentMessage = {
      name,
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

  Connect: grpc.handleBidiStreamingCall<AgentFrame, AgentFrame> = (stream) => {
    // Oneof case names for AgentFrame.payload (game.proto). proto-loader only
    // populates the `payload` discriminator during (de)serialization; outbound
    // raw frame objects built here must carry it explicitly so the frame is
    // self-describing and matches the contract the handler itself relies on
    // when reading inbound frames (`frame.payload === "content"` etc.).
    const PAYLOAD_ONEOF_KEYS = ["content", "wait", "warn", "status"] as const;

    const buildFrame = (
      sessionId: string,
      sender: (typeof FrameSender)[keyof typeof FrameSender],
      payload: Partial<AgentFrame>,
    ): AgentFrame => {
      const payloadKind = PAYLOAD_ONEOF_KEYS.find((k) => k in payload);
      return {
        sessionId,
        frameId: randomUUID(),
        sender,
        createTime: timestampNow(),
        ...(payloadKind ? { payload: payloadKind } : {}),
        ...payload,
      };
    };

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

    // Per-turn AbortControllers for sessions with an in-flight generateTurn.
    // Cleared in finally after the turn resolves OR aborted by abortAllTurns
    // on stream end/error.
    const activeTurns = new Map<string, AbortController>();
    const abortAllTurns = () => {
      // Snapshot values first: finally blocks will asynchronously delete
      // entries as each aborted turn unwinds.
      for (const controller of [...activeTurns.values()]) {
        controller.abort();
      }
      activeTurns.clear();
    };

    stream.on("data", async (frame) => {
      const sessionId = frame.sessionId ?? "";

      if (frame.payload === "status") {
        const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
        const state = sessionAgent.getAdapterState();
        const statusFrame: AgentFrame = buildFrame(
          sessionId,
          FrameSender.FRAME_SENDER_SYSTEM,
          {
            status: {
              status: state.isBound
                ? StatusSignalStatus.STATUS_SIGNAL_STATUS_IDLE
                : StatusSignalStatus.STATUS_SIGNAL_STATUS_UNSPECIFIED,
            },
          },
        );
        stream.write(statusFrame);
        return;
      }

      // Control signals that terminate a turn / flag a warning. The agent
      // never initiates on these, so inbound wait/warn are acknowledged as
      // no-ops (logged) rather than driving any turn state.
      if (frame.payload === "wait") {
        info("wait signal received from peer", { sessionId });
        return;
      }
      if (frame.payload === "warn") {
        const message = frame.warn?.message ?? "";
        warn("warn signal received from peer", { sessionId, message });
        return;
      }

      if (frame.payload === "content") {
        const parts = frame.content?.parts ?? [];

        // Tool results from the desktop arrive as content carrying
        // ToolResultPart(s); route them to the bridge before any user-turn
        // handling.
        const toolResults = parts.filter((p: Part) => p.toolResult);
        if (toolResults.length > 0) {
          const sa = this.sessionAgentStore.getOrCreate(sessionId);
          for (const p of toolResults) {
            sa.getBridge().handleResult(p.toolResult as ToolResultPart);
          }
          return;
        }

        // Only user-sent content drives a turn.
        if (frame.sender !== FrameSender.FRAME_SENDER_USER) {
          return;
        }

        const userText = parts
          .map((p: Part) => p.text?.content ?? "")
          .join("");
        const imagePart = parts.map((p: Part) => p.image).find(Boolean);

        let effectiveProfileName = frame.agentProfileName ?? "";
        if (!effectiveProfileName) {
          const sa = this.sessionAgentStore.getOrCreate(sessionId);
          const state = sa.getAdapterState();
          if (state.isBound && state.activeProfileName) {
            effectiveProfileName = state.activeProfileName;
          }
        }

        if (!effectiveProfileName) {
          warn("no agent profile name for user content frame", { sessionId });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
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
        const controller = new AbortController();
        activeTurns.set(sessionId, controller);
        try {
          const sa = this.sessionAgentStore.getOrCreate(sessionId);

          sa.getBridge().registerSink((contentEnvelope: AgentFrame) => {
            stream.write(contentEnvelope);
          });
          activeSessions.add(sessionId);

          const adapter = await sa.getOrCreateAdapter(
            effectiveProfileName,
            () => this.promptClient.getProfile(effectiveProfileName),
          );

          const turnContent: TurnContent = { text: userText };
          if (imagePart?.data) {
            turnContent.imageData = bytesToBase64String(imagePart.data);
            turnContent.imageMimeType = encodingToMime(imagePart.encoding);
            turnContent.imageWidthPx = imagePart.widthPx;
            turnContent.imageHeightPx = imagePart.heightPx;
          }

          let blockCount = 0;
          for await (const block of adapter.generateTurn(
            sessionId,
            turnContent,
            controller.signal,
          )) {
            blockCount++;
            if (block.type === "reasoning") {
              const thinkFrame: AgentFrame = buildFrame(
                sessionId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  agentProfileName: effectiveProfileName,
                  content: {
                    parts: [{ thinking: { content: block.reasoning } }],
                  },
                },
              );
              stream.write(thinkFrame);
            } else if (block.type === "text") {
              const textFrame: AgentFrame = buildFrame(
                sessionId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  agentProfileName: effectiveProfileName,
                  content: {
                    parts: [{ text: { content: block.text } }],
                  },
                },
              );
              stream.write(textFrame);
            }
          }

          if (controller.signal.aborted) {
            info("turn aborted on desktop disconnect", { sessionId });
          } else {
            info("user content processing completed", {
              sessionId,
              blockCount,
            });
            const waitFrame: AgentFrame = buildFrame(
              sessionId,
              FrameSender.FRAME_SENDER_SYSTEM,
              {
                agentProfileName: effectiveProfileName,
                wait: {},
              },
            );
            stream.write(waitFrame);
          }
        } catch (err: unknown) {
          if (controller.signal.aborted) {
            info("turn aborted on desktop disconnect", { sessionId });
          } else {
            const message =
              err instanceof Error ? err.message : "Processing error";
            error("LLM processing failed", { sessionId, error: message });
            const warnFrame: AgentFrame = buildFrame(
              sessionId,
              FrameSender.FRAME_SENDER_SYSTEM,
              {
                warn: { message: `Processing error: ${message}` },
              },
            );
            stream.write(warnFrame);

            const waitFrame: AgentFrame = buildFrame(
              sessionId,
              FrameSender.FRAME_SENDER_SYSTEM,
              { agentProfileName: effectiveProfileName, wait: {} },
            );
            stream.write(waitFrame);
          }
        } finally {
          activeTurns.delete(sessionId);
          this.releaseMutex(sessionId);
        }
      }
    });

    stream.on("error", (err: Error) => {
      error("connect stream error", { error: err.message });
      abortAllTurns();
      cleanupSinks();
      try {
        stream.end();
      } catch {
        // Already closed.
      }
    });

    stream.on("end", () => {
      info("connect stream ended");
      abortAllTurns();
      cleanupSinks();
    });
  };

  // -----------------------------------------------------------------------
  // ListMessages
  // -----------------------------------------------------------------------

  // TODO: implement true pagination — currently returns all messages with
  // empty nextPageToken.
  ListMessages: grpc.handleUnaryCall<
    { parent?: string; pageSize?: number; pageToken?: string },
    { messages?: MessageProto[]; nextPageToken?: string }
  > = async (call, callback) => {
    const parent = call.request.parent ?? "";

    const sessionId = extractSessionId(parent);

    try {
      const sessionAgent = this.sessionAgentStore.get(sessionId);
      const adapter = sessionAgent?.getAdapter();

      if (!adapter) {
        callback(null, { messages: [], nextPageToken: "" });
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

        const sender =
          msgType === "human"
            ? FrameSender.FRAME_SENDER_USER
            : msgType === "ai"
              ? FrameSender.FRAME_SENDER_AGENT
              : FrameSender.FRAME_SENDER_SYSTEM;

        const parts: Part[] = [];

        if (msgType === "tool") {
          const toolResult = reconstructToolResult(msg.content);
          if (toolResult) {
            parts.push({ toolResult });
          }
        } else {
          if (typeof msg.content === "string") {
            if (msg.content) {
              parts.push({ text: { content: msg.content } });
            }
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

            for (const b of reasoningBlocks) {
              const reasoning = (b as any).reasoning ?? "";
              if (reasoning) parts.push({ thinking: { content: reasoning } });
            }
            const text = textBlocks
              .map((b: any) => b.text ?? "")
              .join("");
            if (text) parts.push({ text: { content: text } });

            for (const imgBlock of imageBlocks) {
              const base64Data = extractBase64FromImageBlock(imgBlock);
              if (base64Data) {
                parts.push({
                  image: {
                    encoding: "IMAGE_ENCODING_PNG",
                    data: base64Data,
                  },
                });
              }
            }
          }

          // AI tool_calls reconstruct as MouseMovePart / MouseClickPart parts
          // so operation history renders identically to live tool dispatch.
          if (msgType === "ai") {
            for (const call of extractToolCalls(msg)) {
              const part = toolCallToPart(call);
              if (part) parts.push(part);
            }
          }
        }

        if (parts.length === 0) {
          continue;
        }

        result.push({
          name: `sessions/${sessionId}/agent/messages/${msg.id}`,
          messageId: msg.id,
          sender,
          content: { parts },
          createTime,
        });
      }

      callback(null, { messages: result, nextPageToken: "" });
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

function bytesToBase64String(data: Uint8Array | string): string {
  if (typeof data === "string") return data;
  return Buffer.from(data).toString("base64");
}

function encodingToMime(encoding: unknown): string {
  if (typeof encoding === "number") {
    return encoding === 1 ? "image/png" : "image/png";
  }
  const subtype = String(encoding ?? "").replace(/^IMAGE_ENCODING_/, "").toLowerCase();
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
    const base64 = bytesLikeToBase64(raw);
    if (base64) return base64;
  }
  return "";
}

/**
 * Coerce a candidate image-data value to a base64 string. Handles the forms a
 * LangChain message content block may carry after a langgraph `MemorySaver`
 * round-trip: a data-url/base64 string, a live `Uint8Array`/`Buffer`, OR a
 * plain object/array produced by JSON-serializing a `Uint8Array` (MemorySaver's
 * default serde yields `{0:137,1:80,...}` — `instanceof Uint8Array` is false on
 * the restored object, so the typed-array branch alone misses checkpointed
 * byte images). Returns "" when the value is not a recognizable byte payload.
 */
function bytesLikeToBase64(raw: unknown): string {
  if (typeof raw === "string" && raw.length > 0) {
    return stripDataUrlPrefix(raw);
  }
  if (raw instanceof Uint8Array && raw.length > 0) {
    return Buffer.from(raw).toString("base64");
  }
  if (Buffer.isBuffer(raw) && raw.length > 0) {
    return raw.toString("base64");
  }
  // JSON-serialized Uint8Array (MemorySaver round-trip): {0:n,1:n,...} or [n,n,...]
  if (raw && typeof raw === "object") {
    const src = Array.isArray(raw) ? raw : undefined;
    if (src) {
      if (src.length > 0 && src.every((b) => typeof b === "number" && Number.isInteger(b) && b >= 0 && b <= 255)) {
        return Buffer.from(src as number[]).toString("base64");
      }
      return "";
    }
    const keys = Object.keys(raw as Record<string, unknown>);
    if (
      keys.length > 0 &&
      keys.every((k, i) => Number(k) === i)
    ) {
      const bytes = keys.map((k) => (raw as Record<string, unknown>)[k]);
      if (bytes.every((b) => typeof b === "number" && Number.isInteger(b) && b >= 0 && b <= 255)) {
        return Buffer.from(bytes as number[]).toString("base64");
      }
    }
  }
  return "";
}

// ---------------------------------------------------------------------------
// Tool history reconstruction helpers
//
// ListMessages best-effort reconstructs MouseMovePart / MouseClickPart /
// ToolResultPart from LangChain message state so operation history renders
// identically to the live tool stream.
// ---------------------------------------------------------------------------

/** Minimal shape of a LangChain tool_call carried on an AIMessage. */
interface ToolCallLike {
  name?: string;
  args?: Record<string, unknown>;
  id?: string;
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

/** Map a mouse_click click_type arg to the proto MouseClickAction string. */
function clickTypeToAction(clickType: unknown): string {
  if (typeof clickType !== "string" || !clickType) {
    return "MOUSE_CLICK_ACTION_UNSPECIFIED";
  }
  return `MOUSE_CLICK_ACTION_${clickType.toUpperCase()}`;
}

/**
 * Best-effort map a LangChain tool_call to a Part (MouseMovePart or
 * MouseClickPart). Unknown tools return null (no Part emitted for them).
 */
function toolCallToPart(call: ToolCallLike): Part | null {
  const name = call.name;
  const args = call.args ?? {};
  if (name === "mouse_move") {
    const part: MouseMovePart = {
      xPx: toInt32(args.x_px),
      yPx: toInt32(args.y_px),
    };
    return { mouseMove: part };
  }
  if (name === "mouse_click") {
    const part: MouseClickPart = {
      click: clickTypeToAction(args.click_type) as MouseClickPart["click"],
    };
    return { mouseClick: part };
  }
  return null;
}

/** Infer ToolResultStatus from the result message text. */
function inferToolResultStatus(message: string): string {
  const lower = message.toLowerCase();
  return lower.includes("ok") || lower.includes("succeeded")
    ? "TOOL_RESULT_STATUS_SUCCEEDED"
    : "TOOL_RESULT_STATUS_FAILED";
}

/**
 * Best-effort reconstruct a ToolResultPart from a ToolMessage's content
 * blocks.
 *
 * - text block (non-annotation) → message
 * - image_url block → screenshot.data (base64, data-url prefix stripped)
 * - pixel-size annotation text → screenshot widthPx / heightPx
 * - status inferred from message ("ok"/"succeeded" → SUCCEEDED, else FAILED)
 *
 * String content (when the ToolMessage carries a plain string) is used as the
 * message directly. tool_id is unknown from history, so it is left empty.
 */
function reconstructToolResult(
  content: BaseMessage["content"],
): ToolResultPart | null {
  const blocks: { type?: string; text?: string; image_url?: { url?: string } }[] =
    Array.isArray(content)
      ? (content as { type?: string; text?: string; image_url?: { url?: string } }[])
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

  const status = inferToolResultStatus(message) as ToolResultPart["status"];

  const result: ToolResultPart = {
    status,
    message,
  };
  if (screenshotData) {
    const screenshot: ImagePart = {
      encoding: "IMAGE_ENCODING_PNG",
      data: screenshotData,
      widthPx,
      heightPx,
    };
    result.screenshot = screenshot;
  }
  return result;
}
