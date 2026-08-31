/**
 * server.ts — grpc-js AgentService + DesktopBridgeService for the game
 * agent_v2.
 *
 * Loads the runtime proto via proto-loader (materialized at its canonical
 * import path under the service root, the experimental/grpc_chain/mid
 * pattern) and registers both services on the single 50051 server
 * (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2 D8):
 * AgentService handlers map onto AgentSessions
 * (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2) — Send
 * streams ChatEvent frames until the turn ends, malformed resource names
 * and empty text are request-level INVALID_ARGUMENT failures (the stream
 * never opens). UpdateAgent/GetAgent and the preset/model catalog RPCs are
 * UNIMPLEMENTED placeholders until the host wiring task replaces them with
 * the real agent/materialization logic.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import { AgentSessions } from "./session.js";
import type { TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { AgentServiceHandlers } from "../agent_v2_types/projects/game/v2/AgentService.js";
import type { DesktopBridgeServiceHandlers } from "../agent_v2_types/projects/game/v2/DesktopBridgeService.js";
import type { HistoryMessage } from "../agent_v2_types/projects/game/v2/HistoryMessage.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
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

const GRPC_PORT = "0.0.0.0:50051";

/**
 * Known template path segments — the TS-side mirror of the game domain's
 * fixed template set (dominion/projects/game/pkg/gameconst/const.go
 * knownTemplateIDs; the game session resource shape is
 * templates/{template}/sessions/{session}, AIP-122).
 */
const KNOWN_TEMPLATES = new Set(["saolei"]);

/**
 * The agent surface consumed by the gRPC handlers: the session-scoped
 * operations of AgentService
 * (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2). Preset and
 * model catalog handlers take separate collaborators once implemented.
 */
export interface AgentSink {
  send(session: string, text: string, stream: TurnStream): void;
  listMessages(session: string): Promise<HistoryMessage[]>;
}

/** What {@link startServer} hands back to the bootstrap for shutdown. */
export interface StartedAgentServer {
  server: grpc.Server;
  sessions: AgentSessions;
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

/**
 * Reject a server-streaming request before any frame is written. grpc-js
 * delivers a streaming call's final status from an 'error' event on the
 * stream (ServerWritableStreamImpl sets the pending status and ends —
 * @grpc/grpc-js server-call.js), matching the game agent handler convention
 * (dominion/projects/game/agent/src/handler.ts).
 */
function rejectStream(call: grpc.ServerWritableStream<unknown, unknown>, message: string): void {
  call.emit("error", {
    code: grpc.status.INVALID_ARGUMENT,
    details: message,
  } as grpc.ServiceError);
}

/**
 * The Phase-2 placeholder error for handlers whose host wiring lands with
 * the US1/US2 implementation tasks (specs/051-agent-v2-dsh-migration/
 * tasks.md T014). Fails loudly at the RPC boundary rather than silently
 * answering empty data.
 */
function unimplemented(method: string): grpc.ServiceError {
  return {
    code: grpc.status.UNIMPLEMENTED,
    details: `${method} is not implemented yet`,
  } as grpc.ServiceError;
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
 * Build the AgentService handlers over a session sink. Exported for unit
 * tests so the gRPC status mapping is asserted without binding a port.
 */
export function buildAgentHandlers(sink: AgentSink): AgentServiceHandlers {
  return {
    Send: (call) => {
      const name = call.request.session ?? "";
      if (parseSessionResource(name) === undefined) {
        rejectStream(
          call,
          `session must be a game session resource name ("templates/{template}/sessions/{session}"), got "${name}"`,
        );
        return;
      }
      const text = call.request.text ?? "";
      if (!text) {
        rejectStream(call, "text must be non-empty");
        return;
      }
      info("Send: dispatching to agent session", { session: name });
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
      sink.send(name, text, {
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
      });
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
      const name = `templates/${parsed.template}/sessions/${parsed.session}`;
      sink.listMessages(name).then(
        (messages) => {
          callback(null, { messages });
        },
        (err: unknown) => {
          info("ListAgentMessages: failed", {
            session: name,
            error: err instanceof Error ? err.message : String(err),
          });
          callback({
            code: grpc.status.INTERNAL,
            message: `history lookup failed: ${err instanceof Error ? err.message : String(err)}`,
          });
        },
      );
    },

    UpdateAgent: (call, callback) => callback(unimplemented("UpdateAgent")),
    GetAgent: (call, callback) => callback(unimplemented("GetAgent")),
    CreatePreset: (call, callback) => callback(unimplemented("CreatePreset")),
    ListPresets: (call, callback) => callback(unimplemented("ListPresets")),
    GetPreset: (call, callback) => callback(unimplemented("GetPreset")),
    UpdatePreset: (call, callback) => callback(unimplemented("UpdatePreset")),
    DeletePreset: (call, callback) => callback(unimplemented("DeletePreset")),
    ListModels: (call, callback) => callback(unimplemented("ListModels")),
  };
}

/**
 * Build the DesktopBridgeService handlers. Connect is a Phase-2 placeholder
 * — the flow stream wires up with the desktop-bridge plugin's handler face
 * (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2) and is
 * replaced by the real registration then.
 */
export function buildDesktopBridgeHandlers(): DesktopBridgeServiceHandlers {
  return {
    Connect: (call) => {
      call.emit("error", {
        code: grpc.status.UNIMPLEMENTED,
        details: "Connect is not implemented yet",
      } as grpc.ServiceError);
    },
  };
}

/** Create, bind, and start the agent_v2 gRPC server on 0.0.0.0:50051. */
export async function startServer(options: { ctx: DshContext }): Promise<StartedAgentServer> {
  const sessions = new AgentSessions(options.ctx);
  const proto = loadProto();
  const server = new grpc.Server();
  server.addService(
    (proto.projects.game.v2.AgentService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildAgentHandlers(sessions),
  );
  server.addService(
    (proto.projects.game.v2.DesktopBridgeService as unknown as {
      service: grpc.ServiceDefinition<grpc.UntypedServiceImplementation>;
    }).service,
    buildDesktopBridgeHandlers(),
  );

  return new Promise((resolve, reject) => {
    server.bindAsync(GRPC_PORT, buildServerCredentials(), (err, port) => {
      if (err) {
        info("startServer: bind failed", { error: err.message });
        reject(err);
        return;
      }
      server.start();
      info("game agent_v2 server listening", { port, tls: hasTlsFiles() });
      resolve({ server, sessions });
    });
  });
}
