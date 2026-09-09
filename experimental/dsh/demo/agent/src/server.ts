/**
 * server.ts — grpc-js Chat service for the dsh demo agent.
 *
 * Loads the runtime proto via proto-loader (materialized at its canonical
 * import path under the service root, the experimental/grpc_chain/mid
 * pattern), maps `SendMessage` onto `AgentSessions.send` (the resource name
 * `conversations/{id}`, AIP-122/136 custom-method pattern,
 * specs/047-dsh-chat-demo/contracts/chat-api.md §2) and `CreateConversation`
 * onto `AgentSessions.create` (explicit preset binding,
 * specs/058-dsh-preset-roster-demo/contracts/chat-api.md §1.1). Malformed or
 * empty fields map to INVALID_ARGUMENT, the not-created domain error maps to
 * FAILED_PRECONDITION (FR-002), preset-authoring rejections map onto their
 * error codes one-to-one (preset-authoring-plugin.md §6), and agent failures
 * map to INTERNAL without taking the process down.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import { info } from "@dominion/common-js-logs";
import { AgentSessions, ConversationNotCreatedError } from "./session.js";
import type { ConversationView } from "./session.js";
import type { DshContext } from "./dsh.js";
import type { ChatHandlers } from "../chat_types/experimental/dsh/demo/Chat.js";
import type { ProtoGrpcType } from "../chat_types/chat.js";

// Service root: parent of the compiled src/ directory.
const serviceRoot = path.resolve(import.meta.dirname, "..");

// Proto path at canonical import location under the service root.
const protoPath = path.join(serviceRoot, "experimental/dsh/demo/chat.proto");

const CONVERSATION_PREFIX = "conversations/";

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

/** Extract the conversation id from a `conversations/{id}` resource name. */
export function conversationIdOf(name: string): string {
  return name.startsWith(CONVERSATION_PREFIX)
    ? name.slice(CONVERSATION_PREFIX.length)
    : "";
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
            createTime: {
              seconds: Math.floor(view.createTime.getTime() / 1000),
              nanos: (view.createTime.getTime() % 1000) * 1e6,
            },
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

/** Create, bind, and start the Chat server on 0.0.0.0:50051. */
export async function startServer(options: { ctx: DshContext }): Promise<StartedChatServer> {
  const sessions = new AgentSessions(options.ctx);
  const proto = loadProto();
  const server = new grpc.Server();
  server.addService(
    (proto.experimental.dsh.demo.Chat as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildChatHandlers(sessions),
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
