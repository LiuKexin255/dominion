/**
 * server.ts — grpc-js AgentService (team session face) + PresetService +
 * DesktopBridgeService for the game agent_v2.
 *
 * Loads the runtime proto via proto-loader (materialized at its canonical
 * import path under the service root, the experimental/grpc_chain/mid
 * pattern) and registers all three services on the single 50051 server
 * (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2;
 * specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.7):
 * - AgentService handlers implement the team session face of
 *   specs/059-agent-v2-team-mode/contracts/team-api.md §1–§6 — validation is
 *   fail-fast (malformed resource names, missing preset references and empty
 *   text are request-level INVALID_ARGUMENT failures), UpdateTeam validates
 *   both presets and models before materializing, Send has no lazy creation
 *   (unmaterialized → FAILED_PRECONDITION) and relays the team stream
 *   (member event frames + team_message frames) until the team quiesces, the
 *   List faces read the T015 history projections, and GetTeamMember reads the
 *   member instance's live prompt assembly (src/system-prompt.ts).
 * - PresetService handlers are the stateless configuration face (built by
 *   {@link buildPresetHandlers} over its own deps): preset CRUD delegates
 *   to the authoring plugin's `ctx.presetAuthoring` domain service (store-only
 *   role-pooled records; member compositions derive at use time —
 *   specs/060-agent-v2-team-optimize/contracts/preset-derivation.md), and the
 *   model catalog shares ctx.llm.listModels with UpdateTeam's validation.
 * - DesktopBridgeService.Connect is the desktop-bridge plugin's handler face
 *   (`ctx.desktopBridge.handlers()`).
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import type { LlmModelInfo } from "@deepseek-ai/dsh-llm";
// Type-only: the plugin's `ctx.desktopBridge` declaration merge and its
// handler-face types (the handlers are consumed at runtime through the
// composed context, not through a direct import).
import type { BidiStream, DesktopBridgeServiceHandlers as PluginBridgeHandlers } from "@dominion/dsh-desktop-bridge";
// The preset domain service face and its error surface: the PresetService
// RPC handlers translate onto `ctx.presetAuthoring` (specs/059-agent-v2-
// team-mode/contracts/preset-api.md); the error class is a runtime import
// (instanceof discrimination in toServiceError, roster-verification §4.1).
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { PresetAuthoringService, PresetView } from "@dominion/dsh-preset-authoring";
import { TeamSessionError, TeamSessions, PROVIDER } from "./session.js";
import type { TeamView } from "./session.js";
import type { TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { AgentServiceHandlers } from "../agent_v2_types/projects/game/v2/AgentService.js";
import type { PresetServiceHandlers } from "../agent_v2_types/projects/game/v2/PresetService.js";
import type { DesktopBridgeServiceHandlers } from "../agent_v2_types/projects/game/v2/DesktopBridgeService.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { Preset } from "../agent_v2_types/projects/game/v2/Preset.js";
import type { Team as TeamProto } from "../agent_v2_types/projects/game/v2/Team.js";
import type { TeamMember as TeamMemberProto } from "../agent_v2_types/projects/game/v2/TeamMember.js";
import type { TeamMessage as TeamMessageProto } from "../agent_v2_types/projects/game/v2/TeamMessage.js";
import type { Timestamp } from "../agent_v2_types/google/protobuf/Timestamp.js";
import type { ProtoGrpcType } from "../agent_v2_types/agent_v2.js";

// Service root: parent of the compiled src/ directory.
const SERVICE_ROOT = path.resolve(import.meta.dirname, "..");

/**
 * Proto path at its canonical import location under the service root:
 * runtime_protos materializes the proto files (with their transitive deps)
 * at their standard import paths, so the app-root proto lands at
 * projects/game/agent_v2.proto (tools/release/deploy/README.md §runtime_protos;
 * demo precedent: experimental/dsh/demo/agent/src/server.ts loads the
 * app-root chat.proto the same way). Exported for the path assertion test.
 */
export const PROTO_PATH = path.join(SERVICE_ROOT, "projects/game/agent_v2.proto");

/**
 * Known template path segments — the TS-side mirror of the game domain's
 * fixed template set (dominion/projects/game/pkg/gameconst/const.go
 * knownTemplateIDs; the game session resource shape is
 * templates/{template}/sessions/{session}, AIP-122).
 */
const KNOWN_TEMPLATES = new Set(["saolei"]);

/** One entry of the read-only model catalog (agent-api.md §2.6). */
export interface ModelCatalogEntry {
  id: string;
  contextWindow: number;
}

/**
 * The collaborators the PresetService handlers consume: the preset domain
 * service (the authoring plugin's `ctx.presetAuthoring` — store-only
 * role-pooled record CRUD plus use-time composition derivation) and the
 * deployment model catalog. The catalog is a single seam so UpdateTeam's
 * validation and the ListModels RPC cannot drift apart. Faces are
 * structural so tests inject `vi.fn()` doubles (style/javascript.md Mock
 * convention).
 */
export interface PresetServiceDeps {
  authoring: PresetAuthoringService;
  listModels(provider: string): Promise<ModelCatalogEntry[]>;
}

/**
 * The collaborators the team session handlers consume: the team registry
 * plus the {@link PresetServiceDeps} faces — UpdateTeam's fail-fast
 * validation and the List faces read through the registry, and GetTeam fills
 * `desktop_connected` from the desktop-bridge connection registry
 * (specs/059-agent-v2-team-mode/contracts/team-api.md §1/§6).
 */
export interface TeamHandlersDeps extends PresetServiceDeps {
  sessions: Pick<
    TeamSessions,
    | "send"
    | "listTeamMessages"
    | "listMemberMessages"
    | "materialize"
    | "getTeam"
    | "getTeamMember"
    | "cancel"
  >;
  /** The desktop-bridge connection fact read directly off the registry at GetTeam/UpdateTeam time. */
  isDesktopConnected(sessionName: string): boolean;
}

export interface ParsedSessionResource {
  template: string;
  session: string;
}

/**
 * Validate the game session resource name
 * `templates/{template}/sessions/{session}`: both segments non-empty and the
 * template in the known set.
 */
export function parseSessionResource(name: string): ParsedSessionResource | undefined {
  const match = /^templates\/([^/]+)\/sessions\/([^/]+)$/.exec(name);
  if (match === null) {
    return undefined;
  }
  const template = match[1];
  const session = match[2];
  if (!KNOWN_TEMPLATES.has(template)) {
    return undefined;
  }
  return { template, session };
}

/**
 * Validate the team singleton resource name — the session resource name plus
 * the `/team` singleton segment (AIP-156: https://google.aip.dev/156) — and
 * return the underlying session identity.
 */
export function parseTeamResource(name: string): ParsedSessionResource | undefined {
  const match = /^(.+)\/team$/.exec(name);
  if (match === null) {
    return undefined;
  }
  return parseSessionResource(match[1]);
}

/**
 * Validate a team member resource name — the team resource name plus
 * `/members/{member}` — and return the session identity plus the member id.
 */
export function parseTeamMemberResource(
  name: string,
): (ParsedSessionResource & { member: string }) | undefined {
  const match = /^(.+)\/team\/members\/([^/]+)$/.exec(name);
  if (match === null) {
    return undefined;
  }
  const session = parseSessionResource(match[1]);
  if (session === undefined) {
    return undefined;
  }
  return { ...session, member: match[2] };
}

function loadProto(): ProtoGrpcType {
  if (!fs.existsSync(PROTO_PATH)) {
    throw new Error(`agent_v2.proto not found at ${PROTO_PATH}`);
  }
  info("loadProto: loading proto", { protoPath: PROTO_PATH });
  const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
    includeDirs: [SERVICE_ROOT],
  });
  return grpc.loadPackageDefinition(packageDefinition) as unknown as ProtoGrpcType;
}

const TLS_CERT = "/etc/tls/tls.crt";
const TLS_KEY = "/etc/tls/tls.key";

/** Both halves of the mounted certificate pair must be present to serve TLS. */
function hasTlsFiles(): boolean {
  return fs.existsSync(TLS_CERT) && fs.existsSync(TLS_KEY);
}

// Opportunistic TLS: serve with the mounted certificate pair when present,
// otherwise insecure (repo grpc-js service convention).
function buildServerCredentials(): grpc.ServerCredentials {
  const useTLS = hasTlsFiles();
  info("buildServerCredentials", { useTLS, tlsCert: TLS_CERT, tlsKey: TLS_KEY });
  if (useTLS) {
    return grpc.ServerCredentials.createSsl(
      null,
      [{ cert_chain: fs.readFileSync(TLS_CERT), private_key: fs.readFileSync(TLS_KEY) }],
      false,
    );
  }
  return grpc.ServerCredentials.createInsecure();
}

function dateToTimestamp(date: Date): Timestamp {
  const ms = date.getTime();
  return { seconds: Math.floor(ms / 1000), nanos: (ms % 1000) * 1e6 };
}

/**
 * The saolei scene role vocabulary: the role decides the pool a preset is
 * materialized from and is immutable after create
 * (specs/059-agent-v2-team-mode/contracts/preset-api.md §2). The wire form is
 * a plain string (scene-agnostic proto, 2026-09-10 user ruling); this
 * service — the saolei scene host — validates the vocabulary.
 */
type PresetRole = "player" | "planner";

/** Whether a wire role string is part of the saolei scene vocabulary. */
function isSceneRole(value: string): value is PresetRole {
  return value === "player" || value === "planner";
}

/**
 * The pool template a role's presets derive their composition from
 * (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2): the
 * roster root layout pins the template directory names (cordis.yml roots —
 * presets-templates/{player,planner}).
 */
const ROLE_TEMPLATE: Record<PresetRole, string> = { player: "player", planner: "planner" };

/** The RPC resource parent all preset names live under (KNOWN_TEMPLATES). */
const SAOLEI_TEMPLATE = "saolei";

function presetToProto(view: PresetView): Preset {
  const preset: Preset = {
    name: `templates/${SAOLEI_TEMPLATE}/presets/${view.id}`,
    persona: view.persona,
    createTime: dateToTimestamp(view.createTime),
    updateTime: dateToTimestamp(view.updateTime),
  };
  // Every preset this service creates carries a role (required on create);
  // the undefined arm is the seam's role-less-consumer case and stays
  // unset on the wire (proto3 omits it).
  if (view.role !== undefined) {
    preset.role = view.role;
  }
  return preset;
}

/**
 * Project the team registry view onto the proto Team resource. The
 * output-only member states carry the configured preset/model snapshots;
 * `system_prompt` stays empty here — it is served only by GetTeamMember
 * (the proto field is OUTPUT_ONLY; contracts/team-api.md §1).
 */
function teamViewToProto(view: TeamView, desktopConnected: boolean): TeamProto {
  const members: TeamMemberProto[] = view.members.map((member) => ({
    name: member.name,
    role: member.role,
    preset: member.preset,
    model: member.model,
    systemPrompt: member.systemPrompt,
  }));
  return {
    name: view.name,
    members,
    desktopConnected,
    createTime: dateToTimestamp(view.createTime),
    updateTime: dateToTimestamp(view.updateTime),
  };
}

function teamMemberViewToProto(member: TeamView["members"][number]): TeamMemberProto {
  return {
    name: member.name,
    role: member.role,
    preset: member.preset,
    model: member.model,
    systemPrompt: member.systemPrompt,
  };
}

const SESSION_STATUS_BY_CODE: Record<TeamSessionError["code"], grpc.status> = {
  INVALID_ARGUMENT: grpc.status.INVALID_ARGUMENT,
  NOT_FOUND: grpc.status.NOT_FOUND,
  FAILED_PRECONDITION: grpc.status.FAILED_PRECONDITION,
};

const PRESET_STATUS_BY_CODE: Record<PresetAuthoringError["code"], grpc.status> = {
  INVALID_ARGUMENT: grpc.status.INVALID_ARGUMENT,
  ALREADY_EXISTS: grpc.status.ALREADY_EXISTS,
  NOT_FOUND: grpc.status.NOT_FOUND,
  FAILED_PRECONDITION: grpc.status.FAILED_PRECONDITION,
  INTERNAL: grpc.status.INTERNAL,
};

/**
 * Map a request-level failure onto its gRPC status (AIP-193 canonical codes;
 * https://google.aip.dev/193). Non-domain errors fall back to INTERNAL. The
 * original error rides along as the status object's `cause`, keeping the
 * chain inspectable in-process (the wire projection carries code/message
 * only).
 */
function toServiceError(err: unknown): grpc.ServiceError {
  const message = err instanceof Error ? err.message : String(err);
  let code: grpc.status = grpc.status.INTERNAL;
  if (err instanceof TeamSessionError) {
    code = SESSION_STATUS_BY_CODE[err.code];
  } else if (err instanceof PresetAuthoringError) {
    code = PRESET_STATUS_BY_CODE[err.code];
  } else if (err instanceof Error && typeof (err as grpc.ServiceError).code === "number") {
    code = (err as grpc.ServiceError).code;
  }
  return { code, message, cause: err } as unknown as grpc.ServiceError;
}

/**
 * Reject a server-streaming request before any frame is written. grpc-js
 * delivers a streaming call's final status from an 'error' event on the
 * stream (ServerWritableStreamImpl sets the pending status and ends —
 * @grpc/grpc-js server-call.js), matching the game agent handler convention.
 */
function rejectStream(
  call: grpc.ServerWritableStream<unknown, unknown>,
  code: grpc.status,
  message: string,
  cause?: unknown,
): void {
  call.emit("error", { code, details: message, cause } as unknown as grpc.ServiceError);
}

/**
 * Guard one streaming write: a peer that disconnected mid-stream makes
 * `call.write` throw (or the call is already destroyed) — the failure is
 * logged and swallowed so a late frame from the collectors can never escape
 * the async write path as an unhandled rejection and kill the multi-session
 * process.
 */
function safeWrite(
  call: grpc.ServerWritableStream<unknown, unknown>,
  event: ChatEvent,
  sessionName: string,
): void {
  try {
    call.write(event);
  } catch (err) {
    info("stream write failed (peer disconnected?)", {
      session: sessionName,
      error: err instanceof Error ? err.message : String(err),
    });
  }
}

/**
 * Build the AgentService handlers over the team session collaborators
 * (UpdateTeam/GetTeam/GetTeamMember/ListTeamMessages/ListMemberMessages/
 * Send/Cancel — the owner-affinity team surface,
 * specs/059-agent-v2-team-mode/contracts/team-api.md §1–§6). Exported for
 * unit tests so the gRPC status mapping is asserted without binding a port.
 */
export function buildTeamHandlers(deps: TeamHandlersDeps): AgentServiceHandlers {
  return {
    Send: (call) => {
      const name = call.request.session ?? "";
      if (parseSessionResource(name) === undefined) {
        rejectStream(
          call,
          grpc.status.INVALID_ARGUMENT,
          `session must be a game session resource name ("templates/{template}/sessions/{session}"), got "${name}"`,
        );
        return;
      }
      const text = call.request.text ?? "";
      if (!text) {
        rejectStream(call, grpc.status.INVALID_ARGUMENT, "text must be non-empty");
        return;
      }
      // Long-lived stream write guards: a peer disconnect surfaces as an
      // 'error'/'cancelled' event on the call. The disconnect only detaches
      // the stream — it MUST NOT cancel the team orchestration
      // (contracts/team-api.md §3.3). A server-issued terminal event (clean
      // end or orchestration failure) marks the stream settled first so its
      // own 'error' emission is not misread as a peer disconnect.
      let detach: (() => void) | undefined;
      let settled = false;
      const settle = (): void => {
        if (settled) {
          return;
        }
        settled = true;
        detach?.();
      };
      call.on("error", (err: Error) => {
        if (!settled) {
          info("Send stream error (peer disconnected?)", {
            session: name,
            error: err.message,
          });
        }
        settle();
      });
      call.on("cancelled", () => {
        settle();
      });
      const stream: TurnStream = {
        write: (event) => safeWrite(call, event, name),
        end: () => {
          settled = true;
          try {
            call.end();
          } catch (err) {
            info("Send stream end failed (already closed)", {
              session: name,
              error: err instanceof Error ? err.message : String(err),
            });
          }
        },
        fail: (streamError) => {
          settled = true;
          try {
            // grpc-js terminates a server stream with the emitted status
            // (ServerWritableStreamImpl sets pendingStatus on 'error' —
            // @grpc/grpc-js server-call.js): the orchestration failure
            // surfaces as INTERNAL mid-stream (AIP-193, contracts/team-api.md
            // §6) instead of a silent EOF.
            call.emit("error", {
              code: grpc.status.INTERNAL,
              details: streamError.message,
              cause: streamError,
            } as unknown as grpc.ServiceError);
          } catch (err) {
            info("Send stream error close failed (already closed)", {
              session: name,
              error: err instanceof Error ? err.message : String(err),
            });
          }
        },
      };
      try {
        // No lazy materialization: an unmaterialized session throws
        // FAILED_PRECONDITION here and the stream never opens; the returned
        // detach removes exactly this subscription.
        detach = deps.sessions.send(name, text, stream);
      } catch (err) {
        const serviceError = toServiceError(err);
        info("Send rejected", { session: name, code: serviceError.code });
        rejectStream(call, serviceError.code, serviceError.message, err);
      }
    },

    UpdateTeam: (call, callback) => {
      const team = call.request.team ?? undefined;
      const name = team?.name ?? "";
      const session = parseTeamResource(name);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `team.name must be a team resource name ("templates/{template}/sessions/{session}/team"), got "${name}"`,
        });
        return;
      }
      // Layer 1 — structure (scene-agnostic, contracts/team-api.md §2):
      // the members list is the materialization input, every member carries
      // a non-empty role and a preset resource name under the session
      // template; the model is optional.
      const wireMembers = team?.members ?? [];
      if (wireMembers.length === 0) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: "team.members must not be empty",
        });
        return;
      }
      const members = [] as Array<{ role: string; preset: string; model?: string }>;
      for (const wireMember of wireMembers) {
        const role = wireMember.role ?? "";
        if (role === "") {
          callback({
            code: grpc.status.INVALID_ARGUMENT,
            message: "every team member must carry a non-empty role",
          });
          return;
        }
        const presetName = wireMember.preset ?? "";
        const preset = parsePresetResource(presetName);
        if (preset === undefined) {
          callback({
            code: grpc.status.INVALID_ARGUMENT,
            message: `member "${role}" preset must be a preset resource name ("templates/{template}/presets/{preset}"), got "${presetName}"`,
          });
          return;
        }
        if (preset.template !== session.template) {
          callback({
            code: grpc.status.INVALID_ARGUMENT,
            message: `member "${role}" preset template ${preset.template} does not match team template ${session.template}`,
          });
          return;
        }
        const model = wireMember.model ?? "";
        members.push({
          role,
          preset: presetName,
          ...(model === "" ? {} : { model }),
        });
      }
      // An explicit mask may only name the singleton's mutable field
      // (AIP-134: https://google.aip.dev/134; members is replaced whole —
      // contracts/team-api.md §2). An explicit empty mask is rejected: the
      // same convention as UpdatePreset ("omitted" is the way to replace all
      // mutable fields).
      const maskPaths = call.request.updateMask?.paths;
      if (maskPaths !== undefined && maskPaths.length === 0) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: 'update_mask must be omitted or carry the "members" path',
        });
        return;
      }
      const badPath = (maskPaths ?? []).find((maskPath) => maskPath !== "members");
      if (badPath !== undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `update_mask paths must be "members", got "${badPath}"`,
        });
        return;
      }
      const sessionName = `templates/${session.template}/sessions/${session.session}`;
      void (async () => {
        try {
          // Layer 2 — saolei scene validation (exactly two members with the
          // {player, planner} roles, preset existence + role equality, model
          // catalog) happens inside the registry BEFORE any teardown — no
          // half-materialized state (contracts/team-api.md §2).
          const view = await deps.sessions.materialize(sessionName, { members });
          callback(null, teamViewToProto(view, deps.isDesktopConnected(sessionName)));
        } catch (err) {
          const serviceError = toServiceError(err);
          info("UpdateTeam failed", {
            session: sessionName,
            code: serviceError.code,
            error: serviceError.message,
          });
          callback(serviceError);
        }
      })();
    },

    GetTeam: (call, callback) => {
      const name = call.request.name ?? "";
      const session = parseTeamResource(name);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a team resource name ("templates/{template}/sessions/{session}/team"), got "${name}"`,
        });
        return;
      }
      const sessionName = `templates/${session.template}/sessions/${session.session}`;
      try {
        // desktop_connected is the bridge registry fact at query time, filled
        // at the handler layer — the connection state is not team storage
        // state (contracts/team-api.md §1).
        callback(null, teamViewToProto(deps.sessions.getTeam(sessionName), deps.isDesktopConnected(sessionName)));
      } catch (err) {
        callback(toServiceError(err));
      }
    },

    GetTeamMember: (call, callback) => {
      const name = call.request.name ?? "";
      const parsed = parseTeamMemberResource(name);
      if (parsed === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a team member resource name ("templates/{template}/sessions/{session}/team/members/{member}"), got "${name}"`,
        });
        return;
      }
      const sessionName = `templates/${parsed.template}/sessions/${parsed.session}`;
      void (async () => {
        try {
          // The system_prompt read assembles from the member instance's live
          // prompt surface (src/system-prompt.ts); an assembly failure maps
          // to INTERNAL with its cause chain (contracts/team-api.md §6).
          const member = await deps.sessions.getTeamMember(sessionName, parsed.member);
          callback(null, teamMemberViewToProto(member));
        } catch (err) {
          const serviceError = toServiceError(err);
          info("GetTeamMember failed", {
            name,
            code: serviceError.code,
            error: serviceError.message,
          });
          callback(serviceError);
        }
      })();
    },

    ListTeamMessages: (call, callback) => {
      const parent = call.request.parent ?? "";
      const session = parseTeamResource(parent);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `parent must be a team resource name ("templates/{template}/sessions/{session}/team"), got "${parent}"`,
        });
        return;
      }
      // Pagination fields are protocol compliance only: the merged sequence
      // is in-memory and returned whole, next_page_token stays empty
      // (contracts/team-api.md §5).
      const sessionName = `templates/${session.template}/sessions/${session.session}`;
      try {
        const messages: TeamMessageProto[] = deps.sessions
          .listTeamMessages(sessionName)
          .map((entry) => ({
            // The producer label is the scene role string itself ("user"
            // reserved for user input) — no enum mapping (2026-09-10 ruling).
            member: entry.member,
            message: entry.message,
            seq: String(entry.seq),
          }));
        callback(null, { messages, nextPageToken: "" });
      } catch (err) {
        const serviceError = toServiceError(err);
        info("ListTeamMessages: failed", {
          parent,
          code: serviceError.code,
          error: serviceError.message,
        });
        callback(serviceError);
      }
    },

    ListMemberMessages: (call, callback) => {
      const parent = call.request.parent ?? "";
      const parsed = parseTeamMemberResource(parent);
      if (parsed === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `parent must be a team member resource name ("templates/{template}/sessions/{session}/team/members/{member}"), got "${parent}"`,
        });
        return;
      }
      const sessionName = `templates/${parsed.template}/sessions/${parsed.session}`;
      try {
        const messages = deps.sessions.listMemberMessages(sessionName, parsed.member).map((entry) => ({
          message: entry.message,
          sender: entry.sender,
        }));
        callback(null, { messages, nextPageToken: "" });
      } catch (err) {
        const serviceError = toServiceError(err);
        info("ListMemberMessages: failed", {
          parent,
          code: serviceError.code,
          error: serviceError.message,
        });
        callback(serviceError);
      }
    },

    // Cancel keeps the Send rejection family: a malformed name is
    // INVALID_ARGUMENT and an unmaterialized team is FAILED_PRECONDITION.
    // The cancel semantics themselves (in-flight turn termination, queue
    // preservation, idempotent no-op) live in TeamSessions.cancel
    // (specs/059-agent-v2-team-mode/contracts/team-api.md §4).
    Cancel: (call, callback) => {
      const name = call.request.name ?? "";
      const session = parseTeamResource(name);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a team resource name ("templates/{template}/sessions/{session}/team"), got "${name}"`,
        });
        return;
      }
      const sessionName = `templates/${session.template}/sessions/${session.session}`;
      try {
        deps.sessions.cancel(sessionName);
        callback(null, {});
      } catch (err) {
        const serviceError = toServiceError(err);
        info("Cancel failed", {
          session: sessionName,
          code: serviceError.code,
          error: serviceError.message,
        });
        callback(serviceError);
      }
    },
  };
}

/** The parsed shape of `templates/{template}/presets/{preset}`. */
export interface ParsedPresetResource {
  template: string;
  preset: string;
}

/**
 * Validate the preset resource name `templates/{template}/presets/{preset}`
 * (AIP-122) against the known template set.
 */
export function parsePresetResource(name: string): ParsedPresetResource | undefined {
  const match = /^templates\/([^/]+)\/presets\/([^/]+)$/.exec(name);
  if (match === null) {
    return undefined;
  }
  const template = match[1];
  if (!KNOWN_TEMPLATES.has(template)) {
    return undefined;
  }
  return { template, preset: match[2] };
}

/**
 * Validate the template parent resource name `templates/{template}`
 * (AIP-122) against the known template set.
 */
export function parseTemplateParent(parent: string): { template: string } | undefined {
  const match = /^templates\/([^/]+)$/.exec(parent);
  if (match === null) {
    return undefined;
  }
  const template = match[1];
  if (!KNOWN_TEMPLATES.has(template)) {
    return undefined;
  }
  return { template };
}

/**
 * Default List page size (agent-api.md §2.5: personal scale defaults to 100).
 */
const DEFAULT_PAGE_SIZE = 100;

/** Maximum List page size (values above are coerced down, AIP-158). */
const MAX_PAGE_SIZE = 1000;

/**
 * Build the PresetService handlers over the stateless configuration
 * collaborators (the preset authoring service + ListModels — served by the
 * same process as the AgentService but routed by the gateway without proxy
 * owner affinity, specs/051-agent-v2-dsh-migration/contracts/agent-api.md
 * §1/§4). Creation records the preset in the store over the role's pool
 * template (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md
 * §1/§2); role is REQUIRED on create and immutable afterwards.
 * Exported for unit tests so the gRPC status mapping is asserted without
 * binding a port.
 */
export function buildPresetHandlers(deps: PresetServiceDeps): PresetServiceHandlers {
  return {
    CreatePreset: (call, callback) => {
      const parent = call.request.parent ?? "";
      if (parseTemplateParent(parent) === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `parent must be a template resource name ("templates/{template}"), got "${parent}"`,
        });
        return;
      }
      const presetId = call.request.presetId ?? "";
      if (presetId === "" || presetId.includes("/")) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `preset_id must be non-empty and free of "/" (AIP-133 caller-supplied id), got "${presetId}"`,
        });
        return;
      }
      // role is REQUIRED on create and decides the pool template the preset
      // derives its composition over
      // (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2).
      // The wire value is a plain string; this scene host validates the
      // vocabulary.
      const role = call.request.role ?? "";
      if (!isSceneRole(role)) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `role is required and must be a known saolei scene role ("player" or "planner"), got "${role}"`,
        });
        return;
      }
      // create_time/update_time are server-maintained (AIP-133); a caller
      // value in the body is ignored. An empty persona is stored as-is, so
      // the derived composition keeps the pool template's persona row (the
      // role default base).
      void deps.authoring
        .create({
          id: presetId,
          template: ROLE_TEMPLATE[role],
          role,
          persona: call.request.preset?.persona ?? "",
        })
        .then(
          (view) => callback(null, presetToProto(view)),
          (err: unknown) => callback(toServiceError(err)),
        );
    },

    ListPresets: (call, callback) => {
      const parent = call.request.parent ?? "";
      if (parseTemplateParent(parent) === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `parent must be a template resource name ("templates/{template}"), got "${parent}"`,
        });
        return;
      }
      // Empty = no role filtering; a non-empty filter must be scene
      // vocabulary (preset-api.md §1).
      const roleValue = call.request.role ?? "";
      if (roleValue !== "" && !isSceneRole(roleValue)) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `role filter must be a known saolei scene role ("player" or "planner"), got "${roleValue}"`,
        });
        return;
      }
      const roleFilter = roleValue === "" ? undefined : roleValue;
      // Coerce per AIP-158: unspecified/0 → default, above max → max.
      const size =
        (call.request.pageSize ?? 0) <= 0
          ? DEFAULT_PAGE_SIZE
          : Math.min(call.request.pageSize ?? 0, MAX_PAGE_SIZE);
      void deps.authoring.list(roleFilter).then(
        (views) => {
          // In-memory keyset pagination over the id sort: the authored
          // preset scale is personal, so the whole filtered set is cheap to
          // project; one extra slice detects a next page without a second
          // read. The page token is the last returned resource name; the
          // keyset compares the id segment.
          const sorted = [...views].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
          const tokenId = call.request.pageToken?.split("/").pop() ?? "";
          let startIdx = 0;
          if (tokenId !== "") {
            const found = sorted.findIndex((view) => view.id > tokenId);
            // A token beyond every id (stale cursor after tail deletions, or
            // a fabricated token) yields an empty page — never a wrap-around
            // to the first page, which would replay already-consumed items.
            startIdx = found === -1 ? sorted.length : found;
          }
          const page = sorted.slice(startIdx);
          const hasMore = page.length > size;
          const presets = (hasMore ? page.slice(0, size) : page).map(presetToProto);
          callback(null, {
            presets,
            nextPageToken: hasMore ? presets[presets.length - 1].name : "",
          });
        },
        (err: unknown) => callback(toServiceError(err)),
      );
    },

    GetPreset: (call, callback) => {
      const name = call.request.name ?? "";
      const parsed = parsePresetResource(name);
      if (parsed === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a preset resource name ("templates/{template}/presets/{preset}"), got "${name}"`,
        });
        return;
      }
      void deps.authoring.get(parsed.preset).then(
        (view) => callback(null, presetToProto(view)),
        (err: unknown) => callback(toServiceError(err)),
      );
    },

    UpdatePreset: (call, callback) => {
      const preset = call.request.preset ?? undefined;
      const name = preset?.name ?? "";
      const parsed = parsePresetResource(name);
      if (parsed === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `preset.name must be a preset resource name ("templates/{template}/presets/{preset}"), got "${name}"`,
        });
        return;
      }
      // persona is the only mutable field (data-model.md §2.1 — presets
      // MUST NOT carry model fields; role is immutable, preset-api.md §2); an
      // explicit mask may only name it.
      const maskPaths = call.request.updateMask?.paths;
      if (maskPaths !== undefined && maskPaths.length === 0) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: "update_mask must be omitted or carry the \"persona\" path",
        });
        return;
      }
      if (maskPaths !== undefined && maskPaths.some((path) => path !== "persona")) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `update_mask paths must be "persona", got [${maskPaths.join(", ")}]`,
        });
        return;
      }
      void deps.authoring
        .update(parsed.preset, { persona: preset?.persona ?? "" })
        .then(
          (view) => callback(null, presetToProto(view)),
          (err: unknown) => callback(toServiceError(err)),
        );
    },

    DeletePreset: (call, callback) => {
      const name = call.request.name ?? "";
      const parsed = parsePresetResource(name);
      if (parsed === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a preset resource name ("templates/{template}/presets/{preset}"), got "${name}"`,
        });
        return;
      }
      // No fan-out: an already-materialized team keeps its derived
      // composition — each session's mount is owned by its agent and
      // outlives the store record's deletion
      // (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2).
      void deps.authoring.remove(parsed.preset).then(
        () => callback(null, {}),
        (err: unknown) => callback(toServiceError(err)),
      );
    },

    ListModels: (call, callback) => {
      void deps.listModels(PROVIDER).then(
        (models) => callback(null, { models }),
        (err: unknown) => callback(toServiceError(err)),
      );
    },
  };
}

/**
 * Build the DesktopBridgeService handlers from the desktop-bridge plugin's
 * handler face (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md
 * §2): the plugin's structural BidiStream is the grpc-js duplex stream the
 * generated Connect handler receives.
 */
export function buildDesktopBridgeHandlers(bridge: PluginBridgeHandlers): DesktopBridgeServiceHandlers {
  return {
    Connect: (call) => {
      bridge.Connect(call as unknown as BidiStream);
    },
  };
}

/**
 * The deployment model catalog: the ids come from `ctx.llm.listModels`
 * (the llm-glm plugin's static config.models — research.md D4) and the
 * context windows from the same adapter's exact-model resolution, so the
 * ListModels RPC and UpdateTeam's validation share one source.
 */
async function listModelCatalog(ctx: DshContext, provider: string): Promise<ModelCatalogEntry[]> {
  const models: ReadonlyArray<LlmModelInfo> = await ctx.llm.listModels(provider);
  return Promise.all(
    models.map(async (model) => {
      const resolved = await ctx.llm.resolveModelInfo(provider, model.id).catch(() => undefined);
      return { id: model.id, contextWindow: resolved?.context?.contextWindow ?? 0 };
    }),
  );
}

/**
 * What {@link buildServer} hands to the bootstrap's gRPC server component:
 * the unbound server, its credentials, and the team registry the
 * composition stop drains.
 */
export interface BuiltAgentServer {
  server: grpc.Server;
  credentials: grpc.ServerCredentials;
  sessions: TeamSessions;
}

/**
 * Construct the team registry, the handler deps, and register the three
 * services — without binding. Binding, serving, and the graceful stop
 * (tryShutdown racing the shutdown budget → forceShutdown) are owned by the
 * bootstrap's gRPC server component
 * (specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §6).
 * The preset domain face comes from the composed context: the
 * preset-authoring plugin row provides `ctx.presetAuthoring` (storage wired
 * by the row's config), so no store handle crosses the bootstrap boundary.
 */
export function buildServer(options: { ctx: DshContext }): BuiltAgentServer {
  const sessions = new TeamSessions(options.ctx, {
    authoring: options.ctx.presetAuthoring,
    listModels: (provider) => listModelCatalog(options.ctx, provider),
  });
  const deps: TeamHandlersDeps = {
    sessions,
    authoring: options.ctx.presetAuthoring,
    listModels: (provider) => listModelCatalog(options.ctx, provider),
    // The composed context mounts the bridge plugin service (the
    // declaration merge in @dominion/dsh-desktop-bridge); GetTeam reads
    // the registry through this face.
    isDesktopConnected: (sessionName) => options.ctx.desktopBridge.isDesktopConnected(sessionName),
  };
  const proto = loadProto();
  const server = new grpc.Server();
  // Dedicated per-service handler factories over one deps object: the
  // AgentService registration binds the team session face and the
  // PresetService registration the configuration face (agent_v2 serves both
  // on this process; the gateway routes them differently — agent-api.md §4,
  // and specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
  // §3.7).
  server.addService(
    (proto.projects.game.v2.AgentService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildTeamHandlers(deps),
  );
  server.addService(
    (proto.projects.game.v2.PresetService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildPresetHandlers(deps),
  );
  server.addService(
    (proto.projects.game.v2.DesktopBridgeService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildDesktopBridgeHandlers(options.ctx.desktopBridge.handlers()),
  );

  return { server, credentials: buildServerCredentials(), sessions };
}
