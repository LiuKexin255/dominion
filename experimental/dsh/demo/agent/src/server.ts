/**
 * server.ts — grpc-js Chat and PresetService for the dsh demo agent.
 *
 * Loads the runtime proto via proto-loader (materialized at its canonical
 * import path under the service root, the experimental/grpc_chain/mid
 * pattern), maps `SendMessage` onto `AgentSessions.send` (the resource name
 * `conversations/{id}`, AIP-122/136 custom-method pattern,
 * specs/047-dsh-chat-demo/contracts/chat-api.md §2), `CreateConversation`
 * onto `AgentSessions.create` (explicit preset binding,
 * specs/058-dsh-preset-roster-demo/contracts/chat-api.md §1.1) and the
 * PresetService CRUD onto `ctx.presetAuthoring` (chat-api.md §2 — the
 * handlers validate the API-edge field constraints and map every
 * preset-authoring rejection onto its gRPC status one-to-one). Malformed or
 * empty fields map to INVALID_ARGUMENT, the not-created domain error maps to
 * FAILED_PRECONDITION (FR-002), preset-authoring rejections map onto their
 * error codes one-to-one (preset-authoring-plugin.md §6), and agent failures
 * map to INTERNAL without taking the process down. The service layer holds
 * zero roster API and zero filesystem references (FR-005): preset domain
 * operations flow exclusively through the preset-authoring service face.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { PresetAuthoringService, PresetView } from "@dominion/dsh-preset-authoring";
import { info } from "@dominion/common-js-logs";
import { AgentSessions, ConversationNotCreatedError } from "./session.js";
import type { ConversationView } from "./session.js";
import type { DshContext } from "./dsh.js";
import type { ChatHandlers } from "../chat_types/experimental/dsh/demo/Chat.js";
import type { PresetServiceHandlers } from "../chat_types/experimental/dsh/demo/PresetService.js";
import type { Preset } from "../chat_types/experimental/dsh/demo/Preset.js";
import type { ProtoGrpcType } from "../chat_types/chat.js";

// Service root: parent of the compiled src/ directory.
const serviceRoot = path.resolve(import.meta.dirname, "..");

// Proto path at canonical import location under the service root.
const protoPath = path.join(serviceRoot, "experimental/dsh/demo/chat.proto");

const CONVERSATION_PREFIX = "conversations/";
const PRESETS_PREFIX = "presets/";

/**
 * The preset id grammar (chat-api.md §2 / data-model.md §1: the id becomes
 * the copy's directory name under the writable root, so this check is a
 * containment boundary, not a style rule). The same grammar is pinned in the
 * plugin (preset-authoring materialize.ts PRESET_ID); the handler repeats it
 * at the API edge so malformed requests fail INVALID_ARGUMENT before any
 * roster call.
 */
const PRESET_ID = /^[a-z0-9][a-z0-9-]*$/;

/**
 * The update_mask paths UpdatePreset accepts (chat-api.md §2: the mask is
 * limited to the dynamic fields; FieldMask paths use the proto field names).
 */
const UPDATABLE_PRESET_FIELDS = new Set(["persona", "display_name"]);

/**
 * The conversation id grammar: the id becomes a dsh SessionId and a resource
 * name segment (AIP-122 resource ID guidance — RFC-1034 characters, lower
 * case), so this check is a containment boundary, not a style rule
 * (chat-api.md §1.1: 非法字符 → INVALID_ARGUMENT).
 */
const CONVERSATION_ID = /^[a-z0-9][a-z0-9-]{0,62}$/;

/** The session service surface consumed by the gRPC handlers. */
export interface ChatSessionSink {
  create(conversationId: string, presetId?: string): Promise<ConversationView>;
  send(conversationId: string, text: string): Promise<string>;
}

/** What {@link startServer} hands back to the bootstrap for shutdown. */
export interface StartedChatServer {
  server: grpc.Server;
  sessions: AgentSessions;
}

function loadProto(): ProtoGrpcType {
  if (!fs.existsSync(protoPath)) {
    throw new Error(`chat.proto not found at ${protoPath}`);
  }
  info("loadProto: loading proto", { protoPath });
  const packageDefinition = protoLoader.loadSync(protoPath, {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
    includeDirs: [serviceRoot],
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

/**
 * Extract the conversation id from a `conversations/{id}` resource name, or
 * "" when the name is not a well-formed resource name — the id must satisfy
 * the {@link CONVERSATION_ID} grammar (AIP-122: a malformed resource name
 * fails INVALID_ARGUMENT at the handler, never a NOT_FOUND lookup).
 */
export function conversationIdOf(name: string): string {
  if (!name.startsWith(CONVERSATION_PREFIX)) {
    return "";
  }
  const id = name.slice(CONVERSATION_PREFIX.length);
  return CONVERSATION_ID.test(id) ? id : "";
}

/**
 * Extract the preset id from a `presets/{id}` resource name, or "" when the
 * name is not a well-formed resource name — the id must satisfy the
 * {@link PRESET_ID} grammar (AIP-122: a malformed resource name fails
 * INVALID_ARGUMENT at the handler, never a NOT_FOUND lookup).
 */
export function presetIdOf(name: string): string {
  if (!name.startsWith(PRESETS_PREFIX)) {
    return "";
  }
  const id = name.slice(PRESETS_PREFIX.length);
  return PRESET_ID.test(id) ? id : "";
}

/** The google.protobuf.Timestamp projection of a Date. */
function timestampOf(date: Date): { seconds: number; nanos: number } {
  return {
    seconds: Math.floor(date.getTime() / 1000),
    nanos: (date.getTime() % 1000) * 1e6,
  };
}

/** The Preset resource projection of an authored-preset view (chat-api.md §2). */
function presetViewOf(view: PresetView): Preset {
  return {
    name: `${PRESETS_PREFIX}${view.id}`,
    template: view.template,
    persona: view.persona,
    // An absent display name falls back to the id at the roster surface; the
    // wire carries the empty string for the absent case (proto3 has no
    // optional string here).
    displayName: view.displayName ?? "",
    createTime: timestampOf(view.createTime),
    updateTime: timestampOf(view.updateTime),
  };
}

/**
 * Map a session/preset domain rejection onto its gRPC status. Preset
 * authoring error codes map one-to-one
 * (specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md §6);
 * the not-created domain error is FAILED_PRECONDITION (FR-002). Any other
 * failure is an unexpected INTERNAL carrying the raw error message.
 */
function domainStatusOf(err: unknown): { code: grpc.status; message: string } {
  if (err instanceof ConversationNotCreatedError) {
    return { code: grpc.status.FAILED_PRECONDITION, message: err.message };
  }
  if (err instanceof PresetAuthoringError) {
    return { code: grpc.status[err.code], message: err.message };
  }
  return {
    code: grpc.status.INTERNAL,
    message: err instanceof Error ? err.message : String(err),
  };
}

/**
 * Build the Chat handlers over a session sink. Exported for unit tests so
 * the gRPC status mapping is asserted without binding a port.
 */
export function buildChatHandlers(sink: ChatSessionSink): ChatHandlers {
  return {
    CreateConversation: (call, callback) => {
      const conversationId = call.request.conversationId ?? "";
      if (!CONVERSATION_ID.test(conversationId)) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `conversation_id must match ${CONVERSATION_ID.source}, got "${conversationId}"`,
        });
        return;
      }
      // An absent preset names the roster default (chat-api.md §1.1; the
      // wire default is the empty string).
      const preset = call.request.preset || undefined;
      info("CreateConversation: dispatching to agent session", { conversationId, preset });

      sink.create(conversationId, preset).then(
        (view) => {
          callback(null, {
            name: view.name,
            preset: view.preset,
            createTime: timestampOf(view.createTime),
          });
        },
        (err: unknown) => {
          const mapped = domainStatusOf(err);
          info("CreateConversation: rejected", {
            conversationId,
            code: mapped.code,
            error: mapped.message,
          });
          callback(mapped);
        },
      );
    },

    SendMessage: (call, callback) => {
      const rawName = call.request.name ?? "";
      const conversationId = conversationIdOf(rawName);
      if (!conversationId) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: `name must be a conversation resource name ("conversations/{id}"), got "${rawName}"`,
        });
        return;
      }
      const message = call.request.message ?? "";
      if (!message) {
        callback({
          code: grpc.status.INVALID_ARGUMENT,
          message: "message must be non-empty",
        });
        return;
      }
      info("SendMessage: dispatching to agent session", { conversationId });

      sink.send(conversationId, message).then(
        (reply) => {
          callback(null, { name: rawName, reply });
        },
        (err: unknown) => {
          // Not-created is a domain status (FAILED_PRECONDITION, FR-002);
          // other round failures are per-request INTERNAL errors
          // (specs/047-dsh-chat-demo/contracts/chat-api.md §1, process stays
          // alive).
          const mapped = domainStatusOf(err);
          info("SendMessage: agent round failed", {
            conversationId,
            code: mapped.code,
            error: mapped.message,
          });
          callback({
            code: mapped.code,
            message: mapped.code === grpc.status.INTERNAL
              ? `agent round failed: ${mapped.message}`
              : mapped.message,
          });
        },
      );
    },
  };
}

/** Fail one unary call with the given gRPC status and message. */
function rejectUnary<T>(
  callback: grpc.sendUnaryData<T>,
  code: grpc.status,
  message: string,
): void {
  callback({ code, message });
}

/**
 * Build the PresetService handlers over the preset-authoring service face
 * (R10: the ONLY preset domain face the service layer consumes). Exported for
 * unit tests so the gRPC status mapping is asserted without binding a port.
 *
 * Field constraints enforced here are the API edge of chat-api.md §2:
 * CreatePreset requires a grammar-valid preset_id, a template, and non-empty
 * persona prose (display_name is optional); UpdatePreset requires a
 * non-empty update_mask limited to persona/display_name, and a masked
 * persona stays non-empty; every handler rejects a non-`presets/{id}`
 * resource name. Template resolvability is NOT re-checked here — it is the
 * plugin's roster resolve, whose rejection surfaces verbatim (unknown
 * template → INVALID_ARGUMENT carrying the available ids, contract §6).
 */
export function buildPresetHandlers(authoring: PresetAuthoringService): PresetServiceHandlers {
  return {
    CreatePreset: (call, callback) => {
      const presetId = call.request.presetId ?? "";
      if (!PRESET_ID.test(presetId)) {
        rejectUnary(
          callback,
          grpc.status.INVALID_ARGUMENT,
          `preset_id must match ${PRESET_ID.source}, got "${presetId}"`,
        );
        return;
      }
      const template = call.request.template ?? "";
      if (!template) {
        rejectUnary(callback, grpc.status.INVALID_ARGUMENT, "template must be non-empty");
        return;
      }
      const persona = call.request.persona ?? "";
      if (!persona) {
        rejectUnary(callback, grpc.status.INVALID_ARGUMENT, "persona must be non-empty");
        return;
      }
      const displayName = call.request.displayName || undefined;
      info("CreatePreset: dispatching to preset authoring", { presetId, template });

      authoring.create({ id: presetId, template, persona, displayName }).then(
        (view) => callback(null, presetViewOf(view)),
        (err: unknown) => {
          const mapped = domainStatusOf(err);
          info("CreatePreset: rejected", { presetId, code: mapped.code, error: mapped.message });
          callback(mapped);
        },
      );
    },

    GetPreset: (call, callback) => {
      const presetId = presetIdOf(call.request.name ?? "");
      if (!presetId) {
        rejectUnary(
          callback,
          grpc.status.INVALID_ARGUMENT,
          `name must be a preset resource name ("presets/{id}"), got "${call.request.name ?? ""}"`,
        );
        return;
      }
      authoring.get(presetId).then(
        (view) => callback(null, presetViewOf(view)),
        (err: unknown) => {
          const mapped = domainStatusOf(err);
          info("GetPreset: rejected", { presetId, code: mapped.code, error: mapped.message });
          callback(mapped);
        },
      );
    },

    ListPresets: (call, callback) => {
      authoring.list().then(
        (views) => callback(null, { presets: views.map(presetViewOf) }),
        (err: unknown) => {
          const mapped = domainStatusOf(err);
          info("ListPresets: rejected", { code: mapped.code, error: mapped.message });
          callback(mapped);
        },
      );
    },

    UpdatePreset: (call, callback) => {
      const presetId = presetIdOf(call.request.name ?? "");
      if (!presetId) {
        rejectUnary(
          callback,
          grpc.status.INVALID_ARGUMENT,
          `name must be a preset resource name ("presets/{id}"), got "${call.request.name ?? ""}"`,
        );
        return;
      }
      const mask = call.request.updateMask?.paths ?? [];
      if (mask.length === 0) {
        rejectUnary(
          callback,
          grpc.status.INVALID_ARGUMENT,
          "update_mask must name at least one of persona, display_name",
        );
        return;
      }
      const unknownPath = mask.find((path) => !UPDATABLE_PRESET_FIELDS.has(path));
      if (unknownPath !== undefined) {
        rejectUnary(
          callback,
          grpc.status.INVALID_ARGUMENT,
          `update_mask path "${unknownPath}" is not updatable (persona, display_name)`,
        );
        return;
      }
      const patch: { persona?: string; displayName?: string } = {};
      if (mask.includes("persona")) {
        const persona = call.request.persona ?? "";
        if (!persona) {
          rejectUnary(callback, grpc.status.INVALID_ARGUMENT, "persona must be non-empty");
          return;
        }
        patch.persona = persona;
      }
      if (mask.includes("display_name")) {
        patch.displayName = call.request.displayName;
      }
      info("UpdatePreset: dispatching to preset authoring", { presetId, mask: mask.join(",") });

      authoring.update(presetId, patch).then(
        (view) => callback(null, presetViewOf(view)),
        (err: unknown) => {
          const mapped = domainStatusOf(err);
          info("UpdatePreset: rejected", { presetId, code: mapped.code, error: mapped.message });
          callback(mapped);
        },
      );
    },

    DeletePreset: (call, callback) => {
      const presetId = presetIdOf(call.request.name ?? "");
      if (!presetId) {
        rejectUnary(
          callback,
          grpc.status.INVALID_ARGUMENT,
          `name must be a preset resource name ("presets/{id}"), got "${call.request.name ?? ""}"`,
        );
        return;
      }
      info("DeletePreset: dispatching to preset authoring", { presetId });

      authoring.remove(presetId).then(
        () => callback(null, {}),
        (err: unknown) => {
          // A template id surfaces the roster's system-trust refusal as
          // FAILED_PRECONDITION through the one-to-one mapping
          // (chat-api.md §2 DeletePreset row); an unknown id is NOT_FOUND.
          const mapped = domainStatusOf(err);
          info("DeletePreset: rejected", { presetId, code: mapped.code, error: mapped.message });
          callback(mapped);
        },
      );
    },
  };
}

/**
 * Create, bind, and start the Chat and PresetService servers on
 * 0.0.0.0:50051.
 */
export async function startServer(options: { ctx: DshContext }): Promise<StartedChatServer> {
  const sessions = new AgentSessions(options.ctx);
  const authoring = options.ctx.get("presetAuthoring") as PresetAuthoringService;
  const proto = loadProto();
  const server = new grpc.Server();
  server.addService(
    (proto.experimental.dsh.demo.Chat as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildChatHandlers(sessions),
  );
  server.addService(
    (proto.experimental.dsh.demo.PresetService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildPresetHandlers(authoring),
  );

  return new Promise((resolve, reject) => {
    server.bindAsync("0.0.0.0:50051", buildServerCredentials(), (err, port) => {
      if (err) {
        info("startServer: bind failed", { error: err.message });
        reject(err);
        return;
      }
      server.start();
      info("dsh chat agent server listening", { port, tls: hasTlsFiles() });
      resolve({ server, sessions });
    });
  });
}
