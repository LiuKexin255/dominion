/**
 * handler.ts — TeamServiceServer gRPC handler implementations.
 *
 * Implements UpdateTeam, GetTeam, Connect, ListMessages and RefreshTeam for
 * the TeamService defined in game.proto
 * (specs/040-team-singleton-conformance/contracts/api-contract.md §2 —
 * replaces the former CreateTeam handler).
 *
 * - **UpdateTeam** (AIP-134 create-or-update
 *   https://google.aip.dev/134#create-or-update + AIP-156 singleton
 *   https://google.aip.dev/156): the session Team's ONLY materialization
 *   point — `allow_missing=true` materializes it on the first call from a
 *   caller-supplied TeamProfile (full resource name, AIP-122); repeated
 *   calls with the same profile are idempotent; a different profile
 *   rebuilds the team graph against the existing checkpointer (US3 FR-005,
 *   rejected FAILED_PRECONDITION while a turn is in-flight, FR-006).
 *   There is no lazy creation anymore: every other RPC requires the team to
 *   already exist (Agent 移除懒加载模式, design decision — the former
 *   implicit creation with a fixed default profile is removed).
 * - **GetTeam**: returns the Team resource (agents = the template schema's
 *   `SAOLEI_TEAM_AGENTS` description, D3 — typed, not hard-coded; profile =
 *   the profile the team was materialized with, FR-004). Requires
 *   the team to have been provisioned; otherwise NOT_FOUND.
 * - **Connect**: bidirectional stream. The client sends UserFrame (user
 *   input, operation results, connectivity probes); the server sends
 *   TeamFrame (agent display content, control signals, operation requests).
 *   User-input frames route to the team agent that `accepts_user_input`
 *   (FR-032 — saolei: player only; planner is an observation view). Frames
 *   carry the producing agent's name (`TeamFrame.agent`, D12). Operation
 *   results from the desktop (flowParts/flow_result) route to the session's
 *   `OperationBridge`.
 *   Frames for a session whose team was not provisioned are rejected with
 *   NOT_FOUND (delivered over the stream's error channel — see Connect).
 * - **ListMessages**: reconstructs one agent's message partition from the
 *   checkpoint state's per-agent channel (`playerMessages`/`plannerMessages`,
 *   FR-005 / A3); `Message.agent` carries the owning agent. Requires the
 *   team to exist; otherwise NOT_FOUND.
 * - **RefreshTeam**: clears the session's short-term memory (FR-018 via
 *   `SessionTeam.refreshTeam`); rejected while a turn is in flight.
 *   Requires the team to exist; otherwise NOT_FOUND.
 *
 * Frame contract (specs/035-proto-contract-refine/contracts/frame-split.md):
 * inbound frames are UserFrame (message_parts = user turns; flow_parts =
 * operation results / status probes); outbound frames are TeamFrame
 * (message_parts = agent display content; flow_parts = control signals /
 * operation requests). Outbound envelopes are built via `buildTeamFrame`
 * (FR-013 — every TeamFrame sets session_id/template_id/frame_id/create_time).
 */

import * as grpc from "@grpc/grpc-js";
import { info, warn, error } from "@dominion/common-js-logs";

import type { BaseMessage } from "@langchain/core/messages";

import type { TeamServiceHandlers } from "../game_types/projects/game/TeamService";
import type { Team } from "../game_types/projects/game/Team";
import type { TeamAgent } from "../game_types/projects/game/TeamAgent";
import type { UpdateTeamRequest } from "../game_types/projects/game/UpdateTeamRequest";
import type { UserFrame } from "../game_types/projects/game/UserFrame";
import type { TeamFrame } from "../game_types/projects/game/TeamFrame";
import type { MessagePart } from "../game_types/projects/game/MessagePart";
import type { FlowPart } from "../game_types/projects/game/FlowPart";
import type { FlowResultPart } from "../game_types/projects/game/FlowResultPart";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";
import type { Message as MessageProto } from "../game_types/projects/game/Message";
import type { TeamStateValue } from "./team/state";

import { buildTeamFrame } from "./turn-loop";
import { PRIMARY_AGENT_NAME } from "./session-team";
import type { SessionTeamStore } from "./session-team";
import type { TurnContent } from "./llm";
import { extractToolCalls, readToolResultStatus } from "./llm";
import type { SinkHandle } from "./operation-bridge";
import { parseToolResultFields } from "./tools/shared/result-blocks";
import { deriveStatusSignal } from "./status-signal";
import { SAOLEI_TEAM_AGENTS } from "./team/graph";
import type { TeamAgent as TeamAgentSchema } from "./team/graph";

/**
 * The per-agent message channel map (D5 / FR-005).
 */
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
  // UpdateTeam (AIP-134 create-or-update — the singleton's ONLY
  // materialization point)
  // -----------------------------------------------------------------------

  UpdateTeam: grpc.handleUnaryCall<UpdateTeamRequest, Team> = async (
    call,
    callback,
  ) => {
    const teamReq = call.request.team ?? null;
    const allowMissing = call.request.allowMissing ?? false;
    const teamName = teamReq?.name ?? "";
    const profile = teamReq?.profile ?? "";

    // AIP-134: `team.name` identifies the Team singleton
    // "templates/{template}/sessions/{session}/team"; `team.profile` is the
    // TeamProfile full resource name "templates/{template}/profiles/{profile}"
    // (AIP-122, https://google.aip.dev/122). The profile's template segment
    // MUST match the name's —
    // validated explicitly, no implicit rules (FR-008,
    // specs/040-team-singleton-conformance/spec.md).
    const parsedTeamName = parseTeamName(teamName);
    const profileName = parseProfileName(profile);
    if (!parsedTeamName.template || !parsedTeamName.sessionId || !profileName) {
      callback({
        code: grpc.status.INVALID_ARGUMENT,
        details:
          "team.name must be templates/{template}/sessions/{session}/team and profile must be templates/{template}/profiles/{profile}",
      } as grpc.ServiceError);
      return;
    }
    if (profileName.template !== parsedTeamName.template) {
      callback({
        code: grpc.status.INVALID_ARGUMENT,
        details: `profile template ${profileName.template} does not match team name template ${parsedTeamName.template}`,
      } as grpc.ServiceError);
      return;
    }

    try {
      // Dispatch on allow_missing + missing/existing (AIP-134
      // create-or-update, specs/040-team-singleton-conformance/contracts/
      // api-contract.md §2.3): missing + allow_missing →
      // materialize; missing + !allow_missing → NOT_FOUND (standard Update
      // semantics); existing + same profile → idempotent; existing +
      // different profile → team-graph rebuild (FR-005, rejected
      // FAILED_PRECONDITION by the store while a turn is in-flight, FR-006).
      // The ALREADY_EXISTS deviation is removed
      // (FR-007, specs/040-team-singleton-conformance/research.md §R6).
      await this.sessionTeamStore.update(
        parsedTeamName.sessionId,
        parsedTeamName.template,
        profileName.profileId,
        allowMissing,
      );
      info("team provisioned", { sessionId: parsedTeamName.sessionId });
      callback(null, buildTeamResource(teamName, profile));
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to update team";
      error("update team failed", {
        sessionId: parsedTeamName.sessionId,
        error: message,
      });
      // Propagate a gRPC status unchanged (the store's NOT_FOUND for a
      // missing team with allow_missing=false, its FAILED_PRECONDITION for a
      // profile change while a turn is in-flight (FR-006), or a downstream
      // error such as the TeamProfile's NOT_FOUND from the prompt service);
      // fall back to INTERNAL for non-status errors.
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
    // The team must have been provisioned via UpdateTeam first: a missing
    // team is NOT_FOUND (the desktop uses this as its provisioning probe).
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
        details: `team not provisioned for session ${sessionId}; provision via UpdateTeam`,
      } as grpc.ServiceError);
      return;
    }

    // FR-004: the Team body carries its current profile — the TeamProfile
    // FULL resource name the team is based on, read back from the store
    // (which records the bare profile id) and re-expanded under the team's
    // template.
    const profileId = this.sessionTeamStore.getProfileName(sessionId) ?? "";
    const profile = profileId
      ? `templates/${template}/profiles/${profileId}`
      : "";
    const team: Team = buildTeamResource(name, profile);

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

    // The team must have been provisioned via UpdateTeam first (no lazy
    // creation). A session without a team has no short-term memory to clear
    // — NOT_FOUND, consistent with GetTeam/ListMessages (api-contract §2).
    const team = this.sessionTeamStore.get(sessionId);
    if (!team) {
      callback({
        code: grpc.status.NOT_FOUND,
        details: `team not provisioned for session ${sessionId}; provision via UpdateTeam`,
      } as grpc.ServiceError);
      return;
    }

    // Reject Refresh while work is in flight: the per-session TurnLoop is
    // the single-flight owner; `isBusy()` covers "turn in flight OR
    // draining queued work" AND the one-shot async initInstruction turn
    // (039 US3 — a refresh during the init could clear a freshly written
    // instruction in `playerMessages`, contract §7; specs/030-queued-chat-input/
    // contracts/turn-loop-contract.md). Deliberately NOT `isRunning()` —
    // that one feeds the Connect status probe, which must exclude the init
    // turn (it emits no `wait`, so ACTIVE would stick the desktop's typing
    // indicator on).
    if (team.isBusy()) {
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

  Connect: grpc.handleBidiStreamingCall<UserFrame, TeamFrame> = (stream) => {
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
      // The gateway injects the BARE session_id (and template_id) from the
      // connect URL path into every inbound frame (specs/031-team-template-mode/
      // contracts/api-contract.md §2.2), so the normal path always yields a
      // bare session id. extractSessionId additionally tolerates the full
      // resource-name form defensively (legacy/direct-proxy callers), and
      // frame.templateId is read verbatim (bare by construction) with an
      // empty-string fallback for the same defensive path.
      const sessionId = extractSessionId(frame.sessionId ?? "");
      const templateId = frame.templateId ?? "";
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
          // (UpdateTeam is the only materialization point). A stray result
          // for an unprovisioned session is dropped — the desktop cannot
          // have dispatched an operation without a live team, so this is a
          // protocol anomaly.
          const team = this.sessionTeamStore.get(sessionId);
          if (!team) {
            warn("flow_result ignored: team not provisioned", { sessionId });
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
          // The probe reports ACTIVE only for REAL turns (`team.isRunning()`
          // excludes the one-shot async initInstruction turn — 039 US3
          // deploy bugfix: the init runs outside the TurnLoop and emits no
          // `wait`, so ACTIVE would stick the desktop's typing indicator on
          // with no way to clear it; the init is still gated for
          // RefreshTeam/rebuild via `isBusy()`, see session-team.ts).
          const statusFrame: TeamFrame = buildTeamFrame(
            sessionId,
            templateId,
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

        // Inbound frames are naturally user-sent (the direction split removed
        // the sender gate — specs/035-proto-contract-refine/research.md R2):
        // every messageParts UserFrame drives a turn.

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
          const warnFrame: TeamFrame = buildTeamFrame(
            sessionId,
            templateId,
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
          const waitFrame: TeamFrame = buildTeamFrame(
            sessionId,
            templateId,
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

        // The team must exist before a turn can run — UpdateTeam is the only
        // materialization point (no lazy creation). grpc-js delivers a bidi
        // stream's final status from an 'error' event on the stream
        // (ServerDuplexStreamImpl: this.on('error', ...) sets the pending
        // status and ends — @grpc/grpc-js server-call.js), so emit the
        // NOT_FOUND service error directly.
        const team = this.sessionTeamStore.get(sessionId);
        if (!team) {
          warn("connect frame rejected: team not provisioned", { sessionId });
          stream.emit("error", {
            code: grpc.status.NOT_FOUND,
            details: `team not provisioned for session ${sessionId}; provision via UpdateTeam`,
          } as grpc.ServiceError);
          return;
        }

        // Register the operation-channel sink on the bridge so flow_result
        // routing continues to work (spec 025 FR-023/FR-025).
        const handle = team.getBridge().registerSink(
          (contentEnvelope: TeamFrame) => {
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
        team.submit(turnContent, (frame: TeamFrame) => {
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
      // The team must have been provisioned via UpdateTeam first (no lazy
      // creation): a session without a team has no checkpoint — NOT_FOUND.
      const team = this.sessionTeamStore.get(sessionId);
      if (!team) {
        callback({
          code: grpc.status.NOT_FOUND,
          details: `team not provisioned for session ${sessionId}; provision via UpdateTeam`,
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

        // MessageRole derivation (FR-020): human → USER; ai + tool → AGENT
        // (tool messages are unified as AGENT, aligned with the live stream's
        // tool_result frames — the former FRAME_SENDER_SYSTEM had no live
        // counterpart; specs/035-proto-contract-refine/contracts/frame-split.md §4.2).
        const role =
          msgType === "human"
            ? "MESSAGE_ROLE_USER"
            : "MESSAGE_ROLE_AGENT";

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
          role,
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
  stream: grpc.ServerDuplexStream<UserFrame, TeamFrame>,
  frame: TeamFrame,
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
 * Build the Team resource response (UpdateTeam/GetTeam, AIP-131/134): the
 * agents come from the template's graph schema (D3, typed — not hard-coded
 * by clients); `profile` is the TeamProfile full name the team is based on
 * (FR-004).
 */
function buildTeamResource(name: string, profile: string): Team {
  return {
    name,
    profile,
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
 * Extract the session id from a frame's `session_id`. The gateway injects
 * the BARE session id into every inbound frame (api-contract.md §2.2), so
 * the normal path returns it unchanged; the full resource-name form
 * "templates/{template}/sessions/{session}" is tolerated defensively
 * (legacy/direct-proxy callers) and reduced to the bare id.
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
