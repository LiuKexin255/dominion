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
 * a MessageParts batch (display) OR a FlowParts batch (control). User turns and
 * agent output are both display frames distinguished only by `sender`;
 * operation results from the desktop are control frames carrying a
 * FlowResultPart (spec 025 FR-023/FR-024); the agent emits display tool_result
 * MessageParts from each tool's LLM result.
 */

import * as grpc from "@grpc/grpc-js";
import { randomUUID } from "node:crypto";
import { info, warn, error } from "@dominion/common-js-logs";

import type { BaseMessage } from "@langchain/core/messages";

import type { AgentServiceHandlers } from "../game_types/projects/game/AgentService";
import type { Agent as AgentMessage } from "../game_types/projects/game/Agent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { MessagePart } from "../game_types/projects/game/MessagePart";
import type { FlowPart } from "../game_types/projects/game/FlowPart";
import type { FlowResultPart } from "../game_types/projects/game/FlowResultPart";
import type { ImagePart } from "../game_types/projects/game/ImagePart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type { Message as MessageProto } from "../game_types/projects/game/Message";

import type { PromptClient } from "./prompt-client";
import type { SessionAgentStore } from "./session-agent";
import type { TurnContent } from "./llm";
import type { SinkHandle } from "./operation-bridge";
import { parseToolResultFields } from "./tools/shared/result-blocks";
import { deriveStatusSignal } from "./status-signal";
import { shouldRejectProfile } from "./profile-guard";

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
    // when reading inbound frames (`frame.payload === "messageParts"` etc.).
    const PAYLOAD_ONEOF_KEYS = ["messageParts", "flowParts"] as const;

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

    // Track the sink handle each session installed on this stream, keyed by
    // session id. cleanupSinks passes the handle to unregisterSink so only
    // THIS stream's sink is cleared (compare-and-delete); a stale close from
    // a superseded stream becomes a no-op and cannot clobber a fresh
    // registration
    // (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §1;
    // specs/021-agent-session-resync/research.md D3).
    const sessionSinkHandles = new Map<string, SinkHandle>();
    const cleanupSinks = () => {
      for (const [sid, handle] of sessionSinkHandles) {
        try {
          const sa = this.sessionAgentStore.getOrCreate(sid);
          sa.getBridge().unregisterSink(handle);
        } catch (err) {
          // Logged rather than swallowed so a failing unregister stays
          // observable for tracing/log-based diagnosis (AGENTS.md).
          warn("cleanupSinks: failed to unregister sink", {
            sessionId: sid,
            error: String(err),
          });
        }
      }
      sessionSinkHandles.clear();
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

      // Control-only FlowParts (operation result / wait / warn / status). A
      // flow_result FlowPart is the desktop's operation-execution outcome on
      // the control channel — route it to the bridge (spec 025 FR-023/FR-025).
      // The agent never initiates on inbound wait/warn (no-op, logged); a
      // status FlowPart is the desktop's connectivity probe — respond with the
      // agent's lifecycle status (data-model.md §9; research.md D9). Operation
      // request FlowParts (mouse/keyboard) never arrive inbound (the desktop
      // executes those, it does not send them to the agent).
      if (frame.payload === "flowParts") {
        const parts = frame.flowParts?.parts ?? [];

        // Operation results from the desktop arrive as flowParts carrying
        // flow_result FlowPart(s) (spec 025 FR-023/FR-025 — control channel);
        // route them to the bridge before any control-signal handling.
        const flowResults = parts.filter((p: FlowPart) => p.flowResult);
        if (flowResults.length > 0) {
          const sa = this.sessionAgentStore.getOrCreate(sessionId);
          for (const p of flowResults) {
            sa.getBridge().handleResult(p.flowResult as FlowResultPart);
          }
          return;
        }

        const statusPart = parts.find((p: FlowPart) => p.status);
        if (statusPart) {
          const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
          const state = sessionAgent.getAdapterState();
          const statusFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              // ACTIVE when a turn is in-flight (shared per-session mutex),
              // else IDLE when an adapter is bound, else UNSPECIFIED
              // (data-model.md §1; status-signal.ts).
              flowParts: {
                parts: [
                  {
                    status: {
                      status: deriveStatusSignal(
                        this.isMutexHeld(sessionId),
                        state.isBound,
                      ),
                    },
                  },
                ],
              },
            },
          );
          safeWrite(stream, statusFrame, sessionId);
          return;
        }
        for (const p of parts) {
          if (p.wait) {
            info("wait signal received from peer", { sessionId });
          } else if (p.warn) {
            const message = p.warn.message ?? "";
            warn("warn signal received from peer", { sessionId, message });
          }
        }
        return;
      }

      if (frame.payload === "messageParts") {
        const parts = frame.messageParts?.parts ?? [];

        // Only user-sent content drives a turn. (Operation results from the
        // desktop now arrive as flowParts/flow_result on the control channel,
        // not as messageParts/toolResult — spec 025 FR-023/FR-024. The display
        // tool_result MessagePart is emitted by this agent from each tool's
        // LLM result, never received inbound from the desktop.)
        if (frame.sender !== FrameSender.FRAME_SENDER_USER) {
          return;
        }

        const userText = parts
          .map((p: MessagePart) => p.text?.content ?? "")
          .join("");
        const imagePart = parts.map((p: MessagePart) => p.image).find(Boolean);

        // Adapter state is read once and reused by both the empty⇒bound
        // fallback below and the profile-name guard.
        const sessionAgent = this.sessionAgentStore.getOrCreate(sessionId);
        const state = sessionAgent.getAdapterState();

        let effectiveProfileName = frame.agentProfileName ?? "";
        if (!effectiveProfileName) {
          if (state.isBound && state.activeProfileName) {
            effectiveProfileName = state.activeProfileName;
          }
        }

        // Profile-name guard: reject a mismatched turn before it acquires the
        // mutex or invokes the adapter. Non-fatal — it only reads state and
        // writes frames, so it cannot panic the session agent or block a later
        // turn. The WarnSignal names the bound vs received profile and the
        // WaitSignal returns the desktop to ready (clears its typing
        // indicator)
        // (specs/021-agent-session-resync/data-model.md §5;
        // specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §3;
        // specs/021-agent-session-resync/research.md D7).
        if (
          shouldRejectProfile(
            state.activeProfileName,
            state.isBound,
            effectiveProfileName,
          )
        ) {
          const boundProfile = state.activeProfileName ?? "";
          warn("profile mismatch: rejecting turn before mutex", {
            sessionId,
            boundProfile,
            receivedProfile: effectiveProfileName,
          });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              flowParts: {
                parts: [
                  {
                    warn: {
                      message: `profile mismatch: session bound to '${boundProfile}' but turn targets '${effectiveProfileName}'; call Refresh to switch profiles`,
                    },
                  },
                ],
              },
            },
          );
          safeWrite(stream, warnFrame, sessionId);
          const waitFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              agentProfileName: effectiveProfileName,
              flowParts: { parts: [{ wait: {} }] },
            },
          );
          safeWrite(stream, waitFrame, sessionId);
          return;
        }

        if (!effectiveProfileName) {
          warn("no agent profile name for user content frame", { sessionId });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              flowParts: {
                parts: [{ warn: { message: "agent_profile_name required" } }],
              },
            },
          );
          safeWrite(stream, warnFrame, sessionId);
          // Return the desktop to ready: a WarnSignal alone would leave the
          // typing indicator stuck after this rejection. The WaitSignal clears
          // it so the operator can immediately retry
          // (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §3;
          // specs/021-agent-session-resync/tasks.md — note on the latent gap).
          const waitFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              agentProfileName: effectiveProfileName,
              flowParts: { parts: [{ wait: {} }] },
            },
          );
          safeWrite(stream, waitFrame, sessionId);
          return;
        }

        await this.acquireMutex(sessionId);
        const controller = new AbortController();
        activeTurns.set(sessionId, controller);
        try {
          const handle = sessionAgent.getBridge().registerSink((contentEnvelope: AgentFrame) => {
            safeWrite(stream, contentEnvelope, sessionId);
          });
          sessionSinkHandles.set(sessionId, handle);

          const adapter = await sessionAgent.getOrCreateAdapter(
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
            // Display blocks (text/thinking/tool_call/tool_result) are emitted
            // as messageParts frames — the conversation channel. Control
            // signals (wait/warn) are emitted as flowParts frames below.
            // (data-model.md §4; contracts/content-model-contract.md §4.)
            if (block.type === "reasoning") {
              const thinkFrame: AgentFrame = buildFrame(
                sessionId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  agentProfileName: effectiveProfileName,
                  messageParts: {
                    parts: [{ thinking: { content: block.reasoning } }],
                  },
                },
              );
              safeWrite(stream, thinkFrame, sessionId);
            } else if (block.type === "text") {
              const textFrame: AgentFrame = buildFrame(
                sessionId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  agentProfileName: effectiveProfileName,
                  messageParts: {
                    parts: [{ text: { content: block.text } }],
                  },
                },
              );
              safeWrite(stream, textFrame, sessionId);
            } else if (block.type === "tool_call") {
              const toolCallFrame: AgentFrame = buildFrame(
                sessionId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  agentProfileName: effectiveProfileName,
                  messageParts: {
                    parts: [
                      {
                        toolCall: {
                          toolId: block.toolCallId,
                          name: block.name,
                          argsJson: JSON.stringify(block.args ?? {}),
                        },
                      },
                    ],
                  },
                },
              );
              safeWrite(stream, toolCallFrame, sessionId);
            } else if (block.type === "tool_result") {
              const toolResultPart: ToolResultPart = {
                toolId: block.toolCallId,
                status: block.status as ToolResultPart["status"],
                message: block.message,
              };
              if (block.screenshot) {
                const screenshot: ImagePart = {
                  encoding: "IMAGE_ENCODING_PNG",
                  data: block.screenshot.data,
                  widthPx: block.screenshot.widthPx,
                  heightPx: block.screenshot.heightPx,
                };
                toolResultPart.screenshot = screenshot;
              }
              const toolResultFrame: AgentFrame = buildFrame(
                sessionId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  agentProfileName: effectiveProfileName,
                  messageParts: { parts: [{ toolResult: toolResultPart }] },
                },
              );
              safeWrite(stream, toolResultFrame, sessionId);
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
                flowParts: { parts: [{ wait: {} }] },
              },
            );
            safeWrite(stream, waitFrame, sessionId);
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
                flowParts: {
                  parts: [{ warn: { message: `Processing error: ${message}` } }],
                },
              },
            );
            safeWrite(stream, warnFrame, sessionId);

            const waitFrame: AgentFrame = buildFrame(
              sessionId,
              FrameSender.FRAME_SENDER_SYSTEM,
              {
                agentProfileName: effectiveProfileName,
                flowParts: { parts: [{ wait: {} }] },
              },
            );
            safeWrite(stream, waitFrame, sessionId);
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

        const parts: MessagePart[] = [];

        if (msgType === "tool") {
          // ToolMessage → tool_result MessagePart. Status is read verbatim
          // from additional_kwargs.toolResultStatus (the real outcome carried
          // by US2); absent → UNSPECIFIED (neutral, NEVER FAILED — spec 023
          // FR-014/FR-015). message + screenshot come from the content blocks
          // via the shared parser used by the live path too (FR-009).
          const statusRaw = readToolResultStatus(msg);
          const toolCallId = (msg as unknown as { tool_call_id?: string })
            .tool_call_id;
          const parsed = parseToolResultFields(msg.content);
          const toolResultPart: ToolResultPart = {
            toolId: toolCallId ?? "",
            status: statusRaw,
            message: parsed.message,
          };
          if (parsed.screenshot) {
            toolResultPart.screenshot = {
              encoding: "IMAGE_ENCODING_PNG",
              data: parsed.screenshot.data,
              widthPx: parsed.screenshot.widthPx,
              heightPx: parsed.screenshot.heightPx,
            };
          }
          parts.push({ toolResult: toolResultPart });
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

          // AIMessage.tool_calls reconstruct as tool_call MessageParts carrying
          // the semantic tool name + args (+ tool_id), so history shows the
          // same tool calls the live stream emitted (spec 023 FR-009 / C4).
          if (msgType === "ai") {
            for (const call of extractToolCalls(msg)) {
              parts.push({
                toolCall: {
                  toolId: call.id ?? "",
                  name: call.name ?? "",
                  argsJson: JSON.stringify(call.args ?? {}),
                },
              });
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

/**
 * Write a frame to the bidi stream, swallowing any synchronous throw that
 * results from writing to a closed/destroyed stream (peer disconnected).
 *
 * A write failure here is an expected operational event on a disconnecting
 * stream, not an anomaly — it is logged at `warn` and the frame is dropped
 * (the peer is gone; per 017 FR-004 no frames should be emitted to a dead
 * peer anyway). The core contract is that this helper NEVER throws: it is
 * the error-containment boundary that prevents a closed-stream write error
 * from escaping an async EventEmitter listener and becoming an unhandled
 * rejection (which would terminate the multi-session agent service).
 *
 * Contract: specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §1
 * Behavior: specs/026-agent-abort-crash-fix/data-model.md §1
 * Crash vector: specs/026-agent-abort-crash-fix/research.md §D
 *
 * NOTE: the stream parameter is typed as `ServerDuplexStream` (the actual
 * type of a bidi-stream handler's stream), not `ServerWritableStream` as
 * written in the contract. The two grpc-js types differ in that
 * `ServerDuplexStream` omits the `request` field; both expose the `write`
 * method this helper depends on. See
 * specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §1.
 */
function safeWrite(
  stream: grpc.ServerDuplexStream<AgentFrame, AgentFrame>,
  frame: AgentFrame,
  sessionId: string,
): void {
  try {
    stream.write(frame);
  } catch (err: unknown) {
    warn("stream write failed (peer disconnected?)", {
      sessionId,
      error: String(err),
    });
  }
}

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
// ListMessages reconstructs MessageParts from LangChain BaseMessage state so
// history renders identically to the live tool stream (spec 023 FR-009).
// tool_call parts come from AIMessage.tool_calls; tool_result parts come from
// a ToolMessage's additional_kwargs.toolResultStatus (the real status) plus
// its content blocks (message + screenshot), parsed by the shared
// parseToolResultFields used by the live path too.
// ---------------------------------------------------------------------------

/** Minimal shape of a LangChain tool_call carried on an AIMessage. */
interface ToolCallLike {
  name?: string;
  args?: Record<string, unknown>;
  id?: string;
}

/** Extract tool_calls from a BaseMessage (AIMessage carries them directly). */
function extractToolCalls(msg: BaseMessage): ToolCallLike[] {
  const calls = (msg as unknown as { tool_calls?: unknown }).tool_calls;
  return Array.isArray(calls) ? (calls as ToolCallLike[]) : [];
}

/** Neutral status for a tool result whose real status is unavailable. */
const STATUS_UNSPECIFIED = "TOOL_RESULT_STATUS_UNSPECIFIED";

/**
 * Read the real ToolResultStatus from a ToolMessage's additional_kwargs. The
 * status is carried there by US2 (buildToolResultMessage) so history reflects
 * the actual outcome (spec 023 FR-012..FR-015). Absent → UNSPECIFIED (neutral,
 * NEVER FAILED — no text inference). US1 leaves it UNSPECIFIED until the
 * mouse tools carry the real status (T018/T019/T020); live and history agree.
 */
function readToolResultStatus(msg: BaseMessage): ToolResultPart["status"] {
  const status = (
    msg as unknown as { additional_kwargs?: { toolResultStatus?: unknown } }
  ).additional_kwargs?.toolResultStatus;
  return typeof status === "string" && status.length > 0
    ? (status as ToolResultPart["status"])
    : (STATUS_UNSPECIFIED as ToolResultPart["status"]);
}
