/**
 * server.ts — grpc-js AgentService + PresetService + DesktopBridgeService
 * for the game agent_v2.
 *
 * Loads the runtime proto via proto-loader (materialized at its canonical
 * import path under the service root, the experimental/grpc_chain/mid
 * pattern) and registers all three services on the single 50051 server
 * (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2;
 * specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.7):
 * - AgentService handlers implement the session-face method semantics of
 *   specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2 — validation
 *   is fail-fast (malformed resource names and empty text are request-level
 *   INVALID_ARGUMENT failures with the stream never opened; UpdateAgent
 *   checks the preset then the model catalog before materializing), and
 *   Send has no lazy creation (unmaterialized → FAILED_PRECONDITION).
 * - PresetService handlers are the stateless configuration face (built by
 *   {@link buildPresetHandlers} over its own deps): preset CRUD delegates
 *   to the PresetStore, and the model catalog shares ctx.llm.listModels
 *   with UpdateAgent's validation (research.md D4).
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
import { AgentSessionError, AgentSessions, PROVIDER } from "./session.js";
import type { AgentView } from "./session.js";
import { PresetStoreError } from "./presets.js";
import type { PresetRecord, PresetStore } from "./presets.js";
import type { TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { AgentServiceHandlers } from "../agent_v2_types/projects/game/v2/AgentService.js";
import type { PresetServiceHandlers } from "../agent_v2_types/projects/game/v2/PresetService.js";
import type { DesktopBridgeServiceHandlers } from "../agent_v2_types/projects/game/v2/DesktopBridgeService.js";
import type { HistoryMessage } from "../agent_v2_types/projects/game/v2/HistoryMessage.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { Preset } from "../agent_v2_types/projects/game/v2/Preset.js";
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
 * The collaborators the PresetService handlers consume: the preset
 * persistence and the deployment model catalog
 * (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.5/§2.6). The
 * catalog is a single seam so UpdateAgent's validation and the ListModels
 * RPC cannot drift apart. Faces are structural so tests inject `vi.fn()`
 * doubles (style/javascript.md Mock convention).
 */
export interface PresetServiceDeps {
  presets: PresetStore;
  listModels(provider: string): Promise<ModelCatalogEntry[]>;
}

/**
 * The collaborators the gRPC handlers consume: the session materialization
 * registry plus the {@link PresetServiceDeps} faces — UpdateAgent's
 * fail-fast validation reads the preset store and the model catalog before
 * materializing (agent-api.md §2.1).
 */
export interface AgentServiceDeps extends PresetServiceDeps {
  sessions: Pick<AgentSessions, "send" | "listMessages" | "materialize" | "getAgent" | "cancel">;
}

export interface ParsedSessionResource {
  template: string;
  session: string;
}

/**
 * Validate the game session resource name
 * `templates/{template}/sessions/{session}`: both segments non-empty and the
 * template in the known set (data-model.md §2.2 validation rule).
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
 * Validate the agent parent resource name — the session resource name plus
 * the `/agent` singleton segment (AIP-156:
 * https://google.aip.dev/156) — and return the underlying session identity.
 */
export function parseAgentParent(parent: string): ParsedSessionResource | undefined {
  const match = /^(.+)\/agent$/.exec(parent);
  if (match === null) {
    return undefined;
  }
  return parseSessionResource(match[1]);
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

function presetToProto(record: PresetRecord): Preset {
  return {
    name: record.name,
    playerPrompt: record.playerPrompt,
    createTime: dateToTimestamp(record.createTime),
    updateTime: dateToTimestamp(record.updateTime),
  };
}

function agentViewToProto(view: AgentView): {
  name: string;
  preset: string;
  model: string;
  createTime: Timestamp;
  updateTime: Timestamp;
} {
  return {
    name: view.name,
    preset: view.preset,
    model: view.model,
    createTime: dateToTimestamp(view.createTime),
    updateTime: dateToTimestamp(view.updateTime),
  };
}

const SESSION_STATUS_BY_CODE: Record<AgentSessionError["code"], grpc.status> = {
  INVALID_ARGUMENT: grpc.status.INVALID_ARGUMENT,
  NOT_FOUND: grpc.status.NOT_FOUND,
  FAILED_PRECONDITION: grpc.status.FAILED_PRECONDITION,
};

const PRESET_STATUS_BY_CODE: Record<PresetStoreError["code"], grpc.status> = {
  ALREADY_EXISTS: grpc.status.ALREADY_EXISTS,
  NOT_FOUND: grpc.status.NOT_FOUND,
};

/**
 * Map a request-level failure onto its gRPC status (AIP-193 canonical codes;
 * v1 precedent: projects/game/agent/src/handler.ts propagates numeric
 * status-carrying errors unchanged). Non-domain errors fall back to
 * INTERNAL.
 */
function toServiceError(err: unknown): grpc.ServiceError {
  const message = err instanceof Error ? err.message : String(err);
  if (err instanceof AgentSessionError) {
    return { code: SESSION_STATUS_BY_CODE[err.code], message } as grpc.ServiceError;
  }
  if (err instanceof PresetStoreError) {
    return { code: PRESET_STATUS_BY_CODE[err.code], message } as grpc.ServiceError;
  }
  if (err instanceof Error && typeof (err as grpc.ServiceError).code === "number") {
    return { code: (err as grpc.ServiceError).code, message } as grpc.ServiceError;
  }
  return { code: grpc.status.INTERNAL, message } as grpc.ServiceError;
}

/**
 * Reject a server-streaming request before any frame is written. grpc-js
 * delivers a streaming call's final status from an 'error' event on the
 * stream (ServerWritableStreamImpl sets the pending status and ends —
 * @grpc/grpc-js server-call.js), matching the game agent handler convention
 * (dominion/projects/game/agent/src/handler.ts).
 */
function rejectStream(
  call: grpc.ServerWritableStream<unknown, unknown>,
  code: grpc.status,
  message: string,
): void {
  call.emit("error", { code, details: message } as grpc.ServiceError);
}

/**
 * Guard one streaming write: a peer that disconnected mid-turn makes
 * `call.write` throw (or the call is already destroyed) — the failure is
 * logged and swallowed so a late frame from the collector/queue can never
 * escape the async write path as an unhandled rejection and kill the
 * multi-session process (v1 precedent: projects/game/agent/src/handler.ts
 * safeWrite).
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
 * Build the AgentService handlers over the session-face collaborators
 * (Send/UpdateAgent/GetAgent/ListAgentMessages/Cancel — the owner-affinity
 * surface, agent-api.md §2.1–§2.4 and specs/054-agent-v2-bugfixes/
 * contracts/agent-api-changes.md §3). Exported for unit tests so the gRPC
 * status mapping is asserted without binding a port.
 */
export function buildAgentHandlers(deps: AgentServiceDeps): AgentServiceHandlers {
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
      // Long-lived stream write guards (the v1 agent handler precedent,
      // projects/game/agent/src/handler.ts safeWrite): a peer disconnect
      // surfaces as an 'error' event / write failure on the call. Without
      // the listener an async write failure becomes an unhandled
      // 'error' event and can take the multi-session process down; with it,
      // late writes from the collector/queue are silently dropped.
      call.on("error", (err: Error) => {
        info("Send stream error (peer disconnected?)", {
          session: name,
          error: err.message,
        });
      });
      const stream: TurnStream = {
        write: (event) => safeWrite(call, event, name),
        end: () => {
          try {
            call.end();
          } catch (err) {
            info("Send stream end failed (already closed)", {
              session: name,
              error: err instanceof Error ? err.message : String(err),
            });
          }
        },
      };
      try {
        // No lazy materialization (specs/051-agent-v2-dsh-migration/spec.md
        // FR-007): an unmaterialized session throws
        // FAILED_PRECONDITION here and the stream never opens
        // (agent-api.md §2.4).
        deps.sessions.send(name, text, stream);
      } catch (err) {
        const serviceError = toServiceError(err);
        info("Send rejected", { session: name, code: serviceError.code });
        rejectStream(call, serviceError.code, serviceError.message);
      }
    },

    UpdateAgent: (call, callback) => {
      const agent = call.request.agent ?? undefined;
      const name = agent?.name ?? "";
      const session = parseAgentParent(name);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `agent.name must be an agent resource name ("templates/{template}/sessions/{session}/agent"), got "${name}"`,
        });
        return;
      }
      // preset is REQUIRED on this singleton and no default preset resource
      // is provisioned — use requires creating one first
      // (specs/051-agent-v2-dsh-migration/spec.md Clarifications, Q&A
      // "agent 经 Update 显式物化时，preset 引用是必填还是可选？", session
      // 2026-08-31) — empty means INVALID_ARGUMENT.
      const presetName = agent?.preset ?? "";
      if (presetName === "") {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: "agent.preset is required; create a preset and reference it",
        });
        return;
      }
      const preset = parsePresetResource(presetName);
      if (preset === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `agent.preset must be a preset resource name ("templates/{template}/presets/{preset}"), got "${presetName}"`,
        });
        return;
      }
      if (preset.template !== session.template) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `preset template ${preset.template} does not match agent template ${session.template}`,
        });
        return;
      }
      // An explicit mask may only name the singleton's mutable fields
      // (AIP-134: https://google.aip.dev/134); the mutable fields are always
      // taken from the request body.
      const maskPaths = call.request.updateMask?.paths ?? [];
      if (maskPaths.some((path) => path !== "preset" && path !== "model")) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `update_mask paths must be "preset" or "model", got [${maskPaths.join(", ")}]`,
        });
        return;
      }
      const model = agent?.model ?? "";
      const sessionName = `templates/${session.template}/sessions/${session.session}`;
      void (async () => {
        try {
          // Fail-fast validation before any teardown (data-model.md §2.2
          // step 1 — no half-materialized state): preset must exist, then a
          // non-empty model must be in the catalog (US2 场景 7).
          const presetRecord = await deps.presets.get(presetName);
          if (model !== "") {
            const catalog = await deps.listModels(PROVIDER);
            if (!catalog.some((entry) => entry.id === model)) {
              callback({
                code: grpc.status.INVALID_ARGUMENT,
                message: `unknown model "${model}"; see ListModels for the available catalog`,
              });
              return;
            }
          }
          const view = await deps.sessions.materialize(sessionName, {
            preset: presetName,
            ...(model === "" ? {} : { model }),
            persona: presetRecord.playerPrompt,
          });
          callback(null, agentViewToProto(view));
        } catch (err) {
          const serviceError = toServiceError(err);
          info("UpdateAgent failed", {
            session: sessionName,
            code: serviceError.code,
            error: serviceError.message,
          });
          callback(serviceError);
        }
      })();
    },

    GetAgent: (call, callback) => {
      const name = call.request.name ?? "";
      const session = parseAgentParent(name);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be an agent resource name ("templates/{template}/sessions/{session}/agent"), got "${name}"`,
        });
        return;
      }
      const sessionName = `templates/${session.template}/sessions/${session.session}`;
      try {
        callback(null, agentViewToProto(deps.sessions.getAgent(sessionName)));
      } catch (err) {
        callback(toServiceError(err));
      }
    },

    ListAgentMessages: (call, callback) => {
      const parent = call.request.parent ?? "";
      const parsed = parseAgentParent(parent);
      if (parsed === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `parent must be an agent resource name ("templates/{template}/sessions/{session}/agent"), got "${parent}"`,
        });
        return;
      }
      // Pagination fields are protocol compliance only: the history is
      // in-memory and returned whole, nextPageToken is always empty
      // (agent-api.md §2.3).
      const sessionName = `templates/${parsed.template}/sessions/${parsed.session}`;
      void deps.sessions.listMessages(sessionName).then(
        (messages: HistoryMessage[]) => {
          callback(null, { messages, nextPageToken: "" });
        },
        (err: unknown) => {
          const serviceError = toServiceError(err);
          info("ListAgentMessages: failed", {
            parent,
            code: serviceError.code,
            error: serviceError.message,
          });
          callback(serviceError);
        },
      );
    },

    // Same shape as GetAgent: the request carries only the agent resource
    // name. Path and preconditions follow Send's rejection family — a
    // malformed name is INVALID_ARGUMENT and an unmaterialized session is
    // FAILED_PRECONDITION (specs/054-agent-v2-bugfixes/contracts/
    // agent-api-changes.md §3); the cancel semantics themselves (in-flight
    // turn termination, queue landing, idempotent no-op) live in
    // AgentSessions.cancel (specs/054-agent-v2-bugfixes/data-model.md §1.3).
    Cancel: (call, callback) => {
      const name = call.request.name ?? "";
      const session = parseAgentParent(name);
      if (session === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be an agent resource name ("templates/{template}/sessions/{session}/agent"), got "${name}"`,
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

/**
 * Validate the preset resource name `templates/{template}/presets/{preset}`
 * (AIP-122) against the known template set.
 */
export function parsePresetResource(name: string): ParsedSessionResource | undefined {
  const match = /^templates\/([^/]+)\/presets\/([^/]+)$/.exec(name);
  if (match === null) {
    return undefined;
  }
  const template = match[1];
  if (!KNOWN_TEMPLATES.has(template)) {
    return undefined;
  }
  return { template, session: match[2] };
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
 * Build the PresetService handlers over the stateless configuration
 * collaborators (preset CRUD + ListModels — served by the same process as
 * the AgentService but routed by the gateway without proxy owner affinity,
 * specs/051-agent-v2-dsh-migration/contracts/agent-api.md §1/§4 and
 * specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.7).
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
      // create_time/update_time are server-maintained (AIP-133); a caller
      // value in the body is ignored.
      const now = new Date();
      const record: PresetRecord = {
        name: `${parent}/presets/${presetId}`,
        playerPrompt: call.request.preset?.playerPrompt ?? "",
        createTime: now,
        updateTime: now,
      };
      void deps.presets.create(record).then(
        () => callback(null, presetToProto(record)),
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
      void deps.presets
        .list(parent, call.request.pageSize ?? 0, call.request.pageToken ?? "")
        .then(
          (page) => {
            callback(null, {
              presets: page.presets.map(presetToProto),
              nextPageToken: page.nextPageToken,
            });
          },
          (err: unknown) => callback(toServiceError(err)),
        );
    },

    GetPreset: (call, callback) => {
      const name = call.request.name ?? "";
      if (parsePresetResource(name) === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a preset resource name ("templates/{template}/presets/{preset}"), got "${name}"`,
        });
        return;
      }
      void deps.presets.get(name).then(
        (record) => callback(null, presetToProto(record)),
        (err: unknown) => callback(toServiceError(err)),
      );
    },

    UpdatePreset: (call, callback) => {
      const preset = call.request.preset ?? undefined;
      const name = preset?.name ?? "";
      if (parsePresetResource(name) === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `preset.name must be a preset resource name ("templates/{template}/presets/{preset}"), got "${name}"`,
        });
        return;
      }
      // player_prompt is the only mutable field (data-model.md §2.1 — presets
      // MUST NOT carry model fields); an explicit mask may only name it.
      const maskPaths = call.request.updateMask?.paths;
      if (maskPaths !== undefined && maskPaths.length === 0) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: "update_mask must be omitted or carry the \"player_prompt\" path",
        });
        return;
      }
      if (maskPaths !== undefined && maskPaths.some((path) => path !== "player_prompt")) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `update_mask paths must be "player_prompt", got [${maskPaths.join(", ")}]`,
        });
        return;
      }
      void (async () => {
        try {
          // create_time is preserved from the stored row (AIP-134); a
          // missing preset is the store's NOT_FOUND.
          const current = await deps.presets.get(name);
          const updated: PresetRecord = {
            name,
            playerPrompt: preset?.playerPrompt ?? "",
            createTime: current.createTime,
            updateTime: new Date(),
          };
          await deps.presets.update(updated);
          callback(null, presetToProto(updated));
        } catch (err) {
          callback(toServiceError(err));
        }
      })();
    },

    DeletePreset: (call, callback) => {
      const name = call.request.name ?? "";
      if (parsePresetResource(name) === undefined) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a preset resource name ("templates/{template}/presets/{preset}"), got "${name}"`,
        });
        return;
      }
      // No fan-out: an already-materialized agent keeps its materialization-
      // time persona snapshot (data-model.md §2.1).
      void deps.presets.delete(name).then(
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
 * ListModels RPC and UpdateAgent's validation share one source.
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
 * the unbound server, its credentials, and the session registry the
 * composition stop drains.
 */
export interface BuiltAgentServer {
  server: grpc.Server;
  credentials: grpc.ServerCredentials;
  sessions: AgentSessions;
}

/**
 * Construct the session registry, the handler deps, and register the three
 * services — without binding. Binding, serving, and the graceful stop
 * (tryShutdown racing the shutdown budget → forceShutdown) are owned by the
 * bootstrap's gRPC server component
 * (specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §6).
 */
export function buildServer(options: {
  ctx: DshContext;
  presetStore: PresetStore;
}): BuiltAgentServer {
  const sessions = new AgentSessions(options.ctx);
  const deps: AgentServiceDeps = {
    sessions,
    presets: options.presetStore,
    listModels: (provider) => listModelCatalog(options.ctx, provider),
  };
  const proto = loadProto();
  const server = new grpc.Server();
  // Dedicated per-service handler factories over one deps object: the
  // AgentService registration binds the session face and the PresetService
  // registration the configuration face (agent_v2 serves both on this
  // process; the gateway routes them differently — agent-api.md §4, and
  // specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.7).
  server.addService(
    (proto.projects.game.v2.AgentService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildAgentHandlers(deps),
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
