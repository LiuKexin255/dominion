/**
 * server.ts — grpc-js Chat service for the dsh demo agent.
 *
 * Loads the runtime proto via proto-loader (materialized at its canonical
 * import path under the service root, the experimental/grpc_chain/mid
 * pattern), maps `SendMessage` onto `AgentSessions.send`: the resource name
 * `conversations/{id}` (AIP-122/136 custom-method pattern,
 * specs/047-dsh-chat-demo/contracts/chat-api.md §2) supplies the conversation
 * id, malformed or empty fields map to INVALID_ARGUMENT, and agent failures
 * map to INTERNAL without taking the process down.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import { AgentSessions } from "./session.js";
import type { DshContext } from "./dsh.js";
import type { ChatHandlers } from "../chat_types/experimental/dsh/demo/Chat.js";
import type { ProtoGrpcType } from "../chat_types/chat.js";

// Service root: parent of the compiled src/ directory.
const serviceRoot = path.resolve(import.meta.dirname, "..");

// Proto path at canonical import location under the service root.
const protoPath = path.join(serviceRoot, "experimental/dsh/demo/chat.proto");

const CONVERSATION_PREFIX = "conversations/";

/** The session service surface consumed by the gRPC handlers. */
export interface ChatSessionSink {
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
 * Build the Chat handlers over a session sink. Exported for unit tests so
 * the gRPC status mapping is asserted without binding a port.
 */
export function buildChatHandlers(sink: ChatSessionSink): ChatHandlers {
  return {
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
          // Agent/round failures are per-request errors (specs/047-dsh-chat-demo/contracts/chat-api.md §1:
          // INTERNAL / HTTP 500, process stays alive).
          info("SendMessage: agent round failed", {
            conversationId,
            error: err instanceof Error ? err.message : String(err),
          });
          callback({
            code: grpc.status.INTERNAL,
            message: `agent round failed: ${err instanceof Error ? err.message : String(err)}`,
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
