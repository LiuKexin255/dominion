/**
 * handler.ts — TeamServiceServer gRPC handler implementations.
 *
 * Implements CreateTeam, GetTeam, Connect, ListMessages and RefreshTeam for
 * the TeamService defined in game.proto (specs/031-team-template-mode/
 * contracts/api-contract.md §2.2 — replaces the former AgentService
 * handlers).
 *
 * - **CreateTeam** (AIP-133): the ONLY Team creation point — explicitly
 *   creates the session's team from a caller-supplied TeamProfile
 *   (full resource name, AIP-122). There is no lazy creation anymore: every
 *   other RPC requires the team to already exist (Agent 移除懒加载模式,
 *   design decision — the former implicit creation with a fixed default
 *   profile is removed).
 * - **GetTeam**: returns the Team resource (agents = the template schema's
 *   `SAOLEI_TEAM_AGENTS` description, D3 — typed, not hard-coded). Requires
 *   the team to have been created; otherwise NOT_FOUND.
 * - **Connect**: bidirectional stream. User-input frames route to the team
 *   agent that `accepts_user_input` (FR-032 — saolei: player only; planner
 *   is an observation view). Frames carry the producing agent's name
 *   (`AgentFrame.agent`, D12). Operation results from the desktop
 *   (flowParts/flow_result) route to the session's `OperationBridge`.
 *   Frames for a session whose team was not created are rejected with
 *   NOT_FOUND (delivered over the stream's error channel — see Connect).
 * - **ListMessages**: reconstructs one agent's message partition from the
 *   checkpoint state's per-agent channel (`playerMessages`/`plannerMessages`,
 *   FR-005 / A3); `Message.agent` carries the owning agent. Requires the
 *   team to exist; otherwise NOT_FOUND.
 * - **RefreshTeam**: clears the session's short-term memory (FR-018 via
 *   `SessionTeam.refreshTeam`); rejected while a turn is in flight.
 *   Requires the team to exist; otherwise NOT_FOUND.
 *
 * Frame contract (Part model, unchanged from spec 023/025/030): every
 * AgentFrame carries exactly one payload — a MessageParts batch (display) OR
 * a FlowParts batch (control). User turns and agent output are both display
 * frames distinguished by `sender`; operation results from the desktop are
 * control frames carrying a FlowResultPart.
 */

import * as grpc from "@grpc/grpc-js";
import { randomUUID } from "node:crypto";
import { info, warn, error } from "@dominion/common-js-logs";

import type { BaseMessage } from "@langchain/core/messages";

import type { TeamServiceHandlers } from "../game_types/projects/game/TeamService";
import type { Team } from "../game_types/projects/game/Team";
import type { TeamAgent } from "../game_types/projects/game/TeamAgent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";
import type { MessagePart } from "../game_types/projects/game/MessagePart";
import type { FlowPart } from "../game_types/projects/game/FlowPart";
import type { FlowResultPart } from "../game_types/projects/game/FlowResultPart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type { Message as MessageProto } from "../game_types/projects/game/Message";
import type { TeamStateValue } from "./team/state";

import { PRIMARY_AGENT_NAME, TeamAlreadyExistsError } from "./session-team";
import type { SessionTeamStore } from "./session-team";
import type { TurnContent } from "./llm";
import { extractToolCalls, readToolResultStatus } from "./llm";
import type { SinkHandle } from "./operation-bridge";
import { parseToolResultFields } from "./tools/shared/result-blocks";
import { deriveStatusSignal } from "./status-signal";
import { SAOLEI_TEAM_AGENTS } from "./team/graph";
import type { TeamAgent as TeamAgentSchema } from "./team/graph";

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

/** The per-agent message channel map (D5 / FR-005). */
const AGENT_CHANNELS: Record<string, "playerMessages" | "plannerMessages"> = {
  player: "playerMessages",
  planner: "plannerMessages",
};

export class Handler implements TeamServiceHandlers {
  [name: string]: any;

  private sessionTeamStore: SessionTeamStore;

  constructor(sessionTeamStore: SessionTeamStore) {
    this.sessionTeamStore = sessionTeamStore;
  }

  // -----------------------------------------------------------------------
  // CreateTeam (AIP-133 — the only Team creation point)
  // -----------------------------------------------------------------------

  CreateTeam: grpc.handleUnaryCall<{ parent?: string; profile?: string }, Team> =
    async (call, callback) => {
      const parent = call.request.parent ?? "";
      const profile = call.request.profile ?? "";

      // AIP-133: parent = "templates/{template}/sessions/{session}". The
      // profile is the TeamProfile full resource name
      // "templates/{template}/profiles/{profile}" (AIP-122): its template
      // segment MUST match the parent's — validated explicitly, no implicit
      // rules (spec 031-team-template-mode directive 2).
      const sessionParent = parseSessionParent(parent);
      const profileName = parseProfileName(profile);
      if (!sessionParent || !profileName) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          details: `parent must be templates/{template}/sessions/{session} and profile must be templates/{template}/profiles/{profile}`,
        } as grpc.ServiceError);
        return;
      }
      if (profileName.template !== sessionParent.template) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          details: `profile template ${profileName.template} does not match parent template ${sessionParent.template}`,
        } as grpc.ServiceError);
        return;
      }

      try {
        // Re-entry is profile-conditional (user refinement — api-contract.md
        // §2.2): same profile → idempotent, returns the existing team; a
        // different profile rejects with TeamAlreadyExistsError below.
        await this.sessionTeamStore.create(
          sessionParent.sessionId,
          sessionParent.template,
          profileName.profileId,
        );
        info("team created", { sessionId: sessionParent.sessionId });
        callback(null, buildTeamResource(`${parent}/team`));
      } catch (err: unknown) {
        // A re-entry with a different profile is a configuration mismatch,
        // not an idempotent retry: ALREADY_EXISTS with the existing profile
        // in the details (user refinement — api-contract.md §2.2).
        if (err instanceof TeamAlreadyExistsError) {
          callback({
            code: grpc.status.ALREADY_EXISTS,
            details: err.message,
          } as grpc.ServiceError);
          return;
        }
        const message =
          err instanceof Error ? err.message : "Failed to create team";
        error("create team failed", {
          sessionId: sessionParent.sessionId,
          error: message,
        });
        // Propagate a downstream gRPC status (e.g. the TeamProfile's
        // NOT_FOUND from the prompt service) unchanged; fall back to
        // INTERNAL for non-status errors.
        const code =
          err instanceof Error &&
          typeof (err as grpc.ServiceError).code === "number"
            ? (err as grpc.ServiceError).code
            : grpc.status.INTERNAL;
        callback({ code, details: message } as grpc.ServiceError);
      }
    };

  // -----------------------------------------------------------------------
  // GetTeam
  // -----------------------------------------------------------------------

  GetTeam: grpc.handleUnaryCall<{ name?: string }, Team> = (
    call,
    callback,
  ) => {
    const name = call.request.name ?? "";
    // AIP-131: the name identifies the Team resource
    // "templates/{template}/sessions/{session}/team". The agents come from
    // the template's graph schema (D3) — a pure resource-description read.
    // The team must have been created via CreateTeam first: a missing team
    // is NOT_FOUND (the desktop uses this as its create-if-missing probe).
    const { template, sessionId } = parseTeamName(name);
    if (!template || !sessionId) {
      callback({
        code: grpc.status.INVALID_ARGUMENT,
        details: `invalid team name: ${name}`,
      } as grpc.ServiceError);
      return;
    }

    if (!this.sessionTeamStore.get(sessionId)) {
      callback({
        code: grpc.status.NOT_FOUND,
        details: `team not created for session ${sessionId}; call CreateTeam first`,
      } as grpc.ServiceError);
      return;
    }

    const team: Team = buildTeamResource(name);

    callback(null, team);
  };

  // -----------------------------------------------------------------------
  // RefreshTeam
  // -----------------------------------------------------------------------

  RefreshTeam: grpc.handleUnaryCall<{ name?: string }, {}> = async (
    call,
    callback,
  ) => {
    const name = call.request.name ?? "";
    const { template, sessionId } = parseTeamName(name);
    if (!template || !sessionId) {
      callback({
        code: grpc.status.INVALID_ARGUMENT,
        details: `invalid team name: ${name}`,
      } as grpc.ServiceError);
      return;
    }
    info("refresh team requested", { sessionId });

    // The team must have been created via CreateTeam first (no lazy
    // creation). A session without a team has no short-term memory to clear
    // — NOT_FOUND, consistent with GetTeam/ListMessages (api-contract §2.2).
    const team = this.sessionTeamStore.get(sessionId);
    if (!team) {
      callback({
        code: grpc.status.NOT_FOUND,
        details: `team not created for session ${sessionId}; call CreateTeam first`,
      } as grpc.ServiceError);
      return;
    }

    // Reject Refresh while a turn is in flight: the per-session TurnLoop is
    // the single-flight owner; `isRunning()` covers "turn in flight OR
    // draining queued work" (specs/030-queued-chat-input/contracts/
    // turn-loop-contract.md).
    if (team.isRunning()) {
      warn("refresh team rejected: turn in-flight", { sessionId });
      callback({
        code: grpc.status.FAILED_PRECONDITION,
        details: "cannot refresh team while a turn is in-flight",
      } as grpc.ServiceError);
      return;
    }

    try {
      await team.refreshTeam();
      info("refresh team completed", { sessionId });
      callback(null, {});
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to refresh team";
      error("refresh team failed", { sessionId, error: message });
      callback({
        code: grpc.status.INTERNAL,
        details: message,
      } as grpc.ServiceError);
    }
  };

  // -----------------------------------------------------------------------
  // Connect (bidirectional streaming)
  // -----------------------------------------------------------------------

  Connect: grpc.handleBidiStreamingCall<AgentFrame, AgentFrame> = (stream) => {
    // Oneof case names for AgentFrame.payload (game.proto). proto-loader only
    // populates the `payload` discriminator during (de)serialization; outbound
    // raw frame objects built here must carry it explicitly so the frame is
    // self-describing.
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
    // THIS stream's sink is cleared (compare-and-delete)
    // (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §1).
    const sessionSinkHandles = new Map<string, SinkHandle>();
    const cleanupSinks = () => {
      for (const [sid, handle] of sessionSinkHandles) {
        try {
          const team = this.sessionTeamStore.get(sid);
          team?.getBridge().unregisterSink(handle);
        } catch (err) {
          warn("cleanupSinks: failed to unregister sink", {
            sessionId: sid,
            error: String(err),
          });
        }
      }
      sessionSinkHandles.clear();
    };

    // Sessions whose per-session TurnLoop emit sink is THIS stream. On stream
    // end/error the loop must stop emitting to the dead peer: abortLoops calls
    // team.abort() for each (clears the queue + emits a final `wait`).
    const activeLoopSessions = new Set<string>();
    const abortLoops = () => {
      for (const sid of [...activeLoopSessions.values()]) {
        try {
          this.sessionTeamStore.get(sid)?.abort();
        } catch (err) {
          warn("abortLoops: failed to abort session loop", {
            sessionId: sid,
            error: String(err),
          });
        }
      }
      activeLoopSessions.clear();
    };

    stream.on("data", async (frame) => {
      // The frame's session_id may be the full session resource name
      // ("templates/{template}/sessions/{session}" — proxy-side Connect
      // forwards the first frame verbatim, see projects/game/proxy/handler/
      // handler.go) or a bare session id.
      const sessionId = extractSessionId(frame.sessionId ?? "");
      if (!sessionId) {
        warn("connect frame with no session id", {});
        return;
      }

      // Control-only FlowParts (operation result / wait / warn / status). A
      // flow_result FlowPart is the desktop's operation-execution outcome on
      // the control channel — route it to the bridge (spec 025 FR-023/FR-025).
      // A status FlowPart is the desktop's connectivity probe — respond with
      // the session's working-state signal.
      if (frame.payload === "flowParts") {
        const parts = frame.flowParts?.parts ?? [];

        const flowResults = parts.filter((p: FlowPart) => p.flowResult);
        if (flowResults.length > 0) {
          // A flow_result can only be routed once the session's team exists
          // (CreateTeam is the only creation point). A stray result for an
          // uncreated session is dropped — the desktop cannot have dispatched
          // an operation without a live team, so this is a protocol anomaly.
          const team = this.sessionTeamStore.get(sessionId);
          if (!team) {
            warn("flow_result ignored: team not created", { sessionId });
            return;
          }
          for (const p of flowResults) {
            team.getBridge().handleResult(p.flowResult as FlowResultPart);
          }
          return;
        }

        const statusPart = parts.find((p: FlowPart) => p.status);
        if (statusPart) {
          const team = this.sessionTeamStore.get(sessionId);
          const statusFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              agent: PRIMARY_AGENT_NAME,
              flowParts: {
                parts: [
                  {
                    status: {
                      status: deriveStatusSignal(
                        team?.isRunning() ?? false,
                        team !== undefined,
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

        // Only user-sent content drives a turn (operation results arrive as
        // flowParts/flow_result on the control channel, not here).
        if (frame.sender !== FrameSender.FRAME_SENDER_USER) {
          return;
        }

        // User-input routing (FR-032): route to the team agent that accepts
        // user input. The frame's `agent` names the target; empty falls back
        // to the (first) accepts-user-input agent — saolei: player. A target
        // that does not accept user input (planner) or is unknown to the
        // template schema is rejected with warn + wait (the desktop's
        // planner tab is input-blocked, observation-only).
        const targetAgent = resolveUserInputAgent(frame.agent ?? "");
        if (!targetAgent) {
          const received = frame.agent ?? "";
          warn("user input rejected: agent does not accept user input", {
            sessionId,
            agent: received,
          });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              agent: PRIMARY_AGENT_NAME,
              flowParts: {
                parts: [
                  {
                    warn: {
                      message: `agent '${received}' does not accept user input (observation view); route input to '${PRIMARY_AGENT_NAME}'`,
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
              agent: PRIMARY_AGENT_NAME,
              flowParts: { parts: [{ wait: {} }] },
            },
          );
          safeWrite(stream, waitFrame, sessionId);
          return;
        }

        const userText = parts
          .map((p: MessagePart) => p.text?.content ?? "")
          .join("");
        const imagePart = parts.map((p: MessagePart) => p.image).find(Boolean);

        // The team must exist before a turn can run — CreateTeam is the only
        // creation point (no lazy creation). grpc-js delivers a bidi stream's
        // final status from an 'error' event on the stream
        // (ServerDuplexStreamImpl: this.on('error', ...) sets the pending
        // status and ends — @grpc/grpc-js server-call.js), so emit the
        // NOT_FOUND service error directly.
        const team = this.sessionTeamStore.get(sessionId);
        if (!team) {
          warn("connect frame rejected: team not created", { sessionId });
          stream.emit("error", {
            code: grpc.status.NOT_FOUND,
            details: `team not created for session ${sessionId}; call CreateTeam first`,
          } as grpc.ServiceError);
          return;
        }

        // Register the operation-channel sink on the bridge so flow_result
        // routing continues to work (spec 025 FR-023/FR-025).
        const handle = team.getBridge().registerSink(
          (contentEnvelope: AgentFrame) => {
            safeWrite(stream, contentEnvelope, sessionId);
          },
        );
        sessionSinkHandles.set(sessionId, handle);

        const turnContent: TurnContent = { text: userText };
        if (imagePart?.data) {
          turnContent.imageData = bytesToBase64String(imagePart.data);
          turnContent.imageMimeType = encodingToMime(imagePart.encoding);
          turnContent.imageWidthPx = imagePart.widthPx;
          turnContent.imageHeightPx = imagePart.heightPx;
        }

        // Route the user content to the per-session TurnLoop (single-flight
        // owner). `submit` is non-blocking: a frame arriving while a turn is
        // in flight is buffered (FR-002) and becomes the next turn on the
        // same thread_id.
        activeLoopSessions.add(sessionId);
        team.submit(turnContent, (frame: AgentFrame) => {
          safeWrite(stream, frame, sessionId);
        });
        return;
      }
    });

    stream.on("error", (err: Error) => {
      error("connect stream error", { error: err.message });
      abortLoops();
      cleanupSinks();
      try {
        stream.end();
      } catch {
        // Already closed.
      }
    });

    stream.on("end", () => {
      info("connect stream ended");
      abortLoops();
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
    // parent = "templates/{template}/sessions/{session}/team/agents/{agent}"
    // (FR-005). The {agent} names the message partition.
    const parsed = parseMessagesParent(parent);
    if (!parsed) {
      callback({
        code: grpc.status.INVALID_ARGUMENT,
        details: `invalid messages parent: ${parent}`,
      } as grpc.ServiceError);
      return;
    }
    const { template, sessionId, agent } = parsed;

    const channel = AGENT_CHANNELS[agent];
    if (!channel) {
      callback({
        code: grpc.status.INVALID_ARGUMENT,
        details: `unknown team agent: ${agent}`,
      } as grpc.ServiceError);
      return;
    }

    try {
      // The team must have been created via CreateTeam first (no lazy
      // creation): a session without a team has no checkpoint — NOT_FOUND.
      const team = this.sessionTeamStore.get(sessionId);
      if (!team) {
        callback({
          code: grpc.status.NOT_FOUND,
          details: `team not created for session ${sessionId}; call CreateTeam first`,
        } as grpc.ServiceError);
        return;
      }

      const state = await team.getTeamState();
      const rawMessages: BaseMessage[] = state?.[channel] ?? [];
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
          // from additional_kwargs.toolResultStatus (the real outcome
          // carried by US2); absent → UNSPECIFIED (neutral, NEVER FAILED —
          // spec 023 FR-014/FR-015). message + screenshot come from the
          // content blocks via the shared parser used by the live path too.
          const statusRaw = readToolResultStatus(msg) as ToolResultPart["status"];
          const toolCallId = (msg as unknown as { tool_call_id?: string })
            .tool_call_id;
          const parsedFields = parseToolResultFields(msg.content);
          const toolResultPart: ToolResultPart = {
            toolId: toolCallId ?? "",
            status: statusRaw,
            message: parsedFields.message,
          };
          if (parsedFields.screenshot) {
            toolResultPart.screenshot = {
              encoding: "IMAGE_ENCODING_PNG",
              data: parsedFields.screenshot.data,
              widthPx: parsedFields.screenshot.widthPx,
              heightPx: parsedFields.screenshot.heightPx,
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

          // AIMessage.tool_calls reconstruct as tool_call MessageParts.
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
          name: `templates/${template}/sessions/${sessionId}/team/agents/${agent}/messages/${msg.id}`,
          messageId: msg.id,
          sender,
          agent,
          content: { parts },
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
 * Resolve the frame's target agent for user input (FR-032): an empty `agent`
 * falls back to the first accepts-user-input agent of the template schema
 * (saolei: player); a named agent is accepted iff the schema declares
 * `accepts_user_input` (planner/unknown → undefined = reject).
 */
function resolveUserInputAgent(agent: string): string | undefined {
  if (agent === "") {
    const first = SAOLEI_TEAM_AGENTS.find((a) => a.accepts_user_input);
    return first ? first.name : undefined;
  }
  const schema = SAOLEI_TEAM_AGENTS.find((a) => a.name === agent);
  return schema?.accepts_user_input ? schema.name : undefined;
}

/**
 * Write a frame to the bidi stream, swallowing any synchronous throw that
 * results from writing to a closed/destroyed stream (peer disconnected).
 *
 * The core contract is that this helper NEVER throws: it is the
 * error-containment boundary that prevents a closed-stream write error from
 * escaping an async EventEmitter listener and becoming an unhandled rejection
 * (which would terminate the multi-session agent service).
 *
 * Contract: specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §1
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

/**
 * Build the Team resource response (CreateTeam/GetTeam, AIP-131/133): the
 * agents come from the template's graph schema (D3, typed — not hard-coded
 * by clients).
 */
function buildTeamResource(name: string): Team {
  return {
    name,
    agents: SAOLEI_TEAM_AGENTS.map((a: TeamAgentSchema): TeamAgent => {
      return { name: a.name, acceptsUserInput: a.accepts_user_input };
    }),
    createTime: timestampNow(),
  };
}

/**
 * Parse a Team resource name "templates/{template}/sessions/{session}/team".
 * Returns empty strings when malformed.
 */
function parseTeamName(name: string): {
  template: string;
  sessionId: string;
} {
  const match = name.match(
    /^templates\/([^/]+)\/sessions\/([^/]+)\/team$/,
  );
  return match
    ? { template: match[1], sessionId: match[2] }
    : { template: "", sessionId: "" };
}

/**
 * Parse a CreateTeam parent "templates/{template}/sessions/{session}"
 * (AIP-133). Returns null when malformed.
 */
function parseSessionParent(parent: string): {
  template: string;
  sessionId: string;
} | null {
  const match = parent.match(/^templates\/([^/]+)\/sessions\/([^/]+)$/);
  return match
    ? { template: match[1], sessionId: match[2] }
    : null;
}

/**
 * Parse a TeamProfile full resource name "templates/{template}/profiles/
 * {profile}" (AIP-122). Returns null when malformed.
 */
function parseProfileName(profile: string): {
  template: string;
  profileId: string;
} | null {
  const match = profile.match(/^templates\/([^/]+)\/profiles\/([^/]+)$/);
  return match ? { template: match[1], profileId: match[2] } : null;
}

/**
 * Parse a ListMessages parent "templates/{template}/sessions/{session}/
 * team/agents/{agent}" (FR-005). Returns null when malformed.
 */
function parseMessagesParent(parent: string): {
  template: string;
  sessionId: string;
  agent: string;
} | null {
  const match = parent.match(
    /^templates\/([^/]+)\/sessions\/([^/]+)\/team\/agents\/([^/]+)$/,
  );
  return match
    ? { template: match[1], sessionId: match[2], agent: match[3] }
    : null;
}

/**
 * Extract the session id from a frame's `session_id`: either the full
 * session resource name "templates/{template}/sessions/{session}" (proxy-side
 * Connect forwards the first frame verbatim) or a bare session id.
 */
function extractSessionId(value: string): string {
  const match = value.match(
    /^templates\/[^/]+\/sessions\/([^/]+)$/,
  );
  return match ? match[1] : value;
}

function bytesToBase64String(data: Uint8Array | string): string {
  if (typeof data === "string") return data;
  return Buffer.from(data).toString("base64");
}

function encodingToMime(encoding: unknown): string {
  if (typeof encoding === "number") {
    // The proto ImageEncoding enum's only concrete value is PNG (1);
    // UNSPECIFIED (0) also defaults to PNG — the desktop always sends PNG
    // screenshots.
    return "image/png";
  }
  const subtype = String(encoding ?? "")
    .replace(/^IMAGE_ENCODING_/, "")
    .toLowerCase();
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
 * plain object/array produced by JSON-serializing a `Uint8Array`
 * (MemorySaver's default serde yields `{0:137,1:80,...}` — `instanceof
 * Uint8Array` is false on the restored object). Returns "" when the value is
 * not a recognizable byte payload.
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
  // JSON-serialized Uint8Array (MemorySaver round-trip): [n,n,...] form.
  if (raw && typeof raw === "object") {
    const src = Array.isArray(raw) ? raw : undefined;
    if (src) {
      if (
        src.length > 0 &&
        src.every(
          (b) =>
            typeof b === "number" &&
            Number.isInteger(b) &&
            b >= 0 &&
            b <= 255,
        )
      ) {
        return Buffer.from(src as number[]).toString("base64");
      }
      return "";
    }
  }
  return "";
}
