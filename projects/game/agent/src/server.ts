/**
 * server.ts — Game agent gRPC server.
 *
 * Loads game.proto, wires service dependencies (secret, prompt client,
 * LLM adapter, shared MemorySaver, compiled StateGraph, handler),
 * registers AgentService on a gRPC server, and starts listening.
 *
 * Exports startServer() invoked by bootstrap.ts after OTel init.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import {
  MemorySaver,
  StateGraph,
  MessagesAnnotation,
} from "@langchain/langgraph";
import type { ProtoGrpcType } from "../game_types/game";

import { readSecret } from "./secrets";
import { PromptClient } from "./prompt-client";
import { type LLMProvider } from "./llm";
import { Handler } from "./handler";
import { ConnectionRegistry } from "./connection-registry";
import { AdapterManager } from "./adapter-manager";

// ---------------------------------------------------------------------------
// Proto loading
// ---------------------------------------------------------------------------

// Service root: the parent directory of the src/ directory.
// In the deployed package, src/server.js is at service/src/server.js,
// so __dirname points to service/src/, and ".." gives us service/.
const protoRoot = path.join(__dirname, "..");

// Proto files are placed at their canonical import paths under the service root.
const protoPath = path.join(protoRoot, "projects/game/game.proto");

// All proto dependencies (e.g. google/api/annotations.proto) are also under
// the service root, so a single includeDir covers all imports.
const protoIncludeDirs = [protoRoot];

function loadProto(): ProtoGrpcType {
  if (!fs.existsSync(protoPath)) {
    throw new Error(`game.proto not found at ${protoPath}`);
  }

  // Load proto definition.
  // Options MUST match the ts_proto_library generation options:
  //   longs=String, enums=String, defaults=true, oneofs=true, keep_case=False
  const packageDefinition = protoLoader.loadSync(protoPath, {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
    includeDirs: protoIncludeDirs,
    // keepCase omitted (keep_case=False in the rule)
  });

  return grpc.loadPackageDefinition(
    packageDefinition,
  ) as unknown as ProtoGrpcType;
}

// ---------------------------------------------------------------------------
// TLS credentials (auto-detect)
// ---------------------------------------------------------------------------

function buildCredentials(): grpc.ServerCredentials {
  const tlsCert = "/etc/tls/tls.crt";
  const tlsKey = "/etc/tls/tls.key";
  const useTLS = fs.existsSync(tlsCert) && fs.existsSync(tlsKey);

  if (useTLS) {
    return grpc.ServerCredentials.createSsl(
      null,
      [{ cert_chain: fs.readFileSync(tlsCert), private_key: fs.readFileSync(tlsKey) }],
      false,
    );
  }
  return grpc.ServerCredentials.createInsecure();
}

// ---------------------------------------------------------------------------
// Exported startServer
// ---------------------------------------------------------------------------

/**
 * Creates and starts the gRPC server.
 *
 * Reads the provider secret, creates the prompt client, AdapterManager
 * (which creates AgentAdapterImpl instances on demand), and
 * ConnectionRegistry, plus a shared MemorySaver checkpointer and a
 * minimal StateGraph compiled with MessagesAnnotation for checkpoint
 * state reads, wires the Handler, loads the proto definition, registers
 * the AgentService, and binds to port 50051 on all interfaces.
 *
 * @param adapterManagerOverride Optional pre-configured AdapterManager
 *   for testing (defaults to production config via env vars).
 * @returns A promise that resolves to the started gRPC Server instance.
 */
export async function startServer(
  adapterManagerOverride?: AdapterManager,
): Promise<grpc.Server> {
  // Register dominion URI resolver for service discovery.
  registerDominionResolver();

  // 1. Read provider secret (empty string if file is missing).
  const providerSecret = readSecret(
    path.join(process.env.DOMINION_SECRET_DIR || "/etc/secrets", "provider"),
  );

  // 2. Create PromptClient (connects to prompt service via dominion).
  const promptClient = new PromptClient();

  // 3. Create AdapterManager (creates AgentAdapterImpl instances on demand).
  const adapterManager: AdapterManager = adapterManagerOverride ?? (() => {
    const baseUrl =
      process.env.OPENCODE_BASE_URL || "https://opencode.ai/zen/go/v1";
    const providerEnv = process.env.LLM_PROVIDER;
    const provider: LLMProvider | undefined =
      providerEnv === "openai" || providerEnv === "anthropic"
        ? providerEnv
        : undefined;
    return new AdapterManager(baseUrl, provider);
  })();

  // 4. Create ConnectionRegistry (per-session connection tracking).
  const connectionRegistry = new ConnectionRegistry();

  // 5. Create ONE shared MemorySaver for all sessions.
  const checkpointer = new MemorySaver();

  // 6. Create minimal StateGraph compiled with MessagesAnnotation for checkpoint reads.
  const graph = new StateGraph(MessagesAnnotation).compile({ checkpointer });

  // 7. Create Handler with all dependencies.
  const handler = new Handler(
    promptClient,
    adapterManager,
    connectionRegistry,
    checkpointer,
    graph,
    providerSecret,
  );

  // Load proto and build credentials.
  const proto = loadProto();
  const credentials = buildCredentials();
  const tlsEnabled = fs.existsSync("/etc/tls/tls.crt") && fs.existsSync("/etc/tls/tls.key");

  // Create gRPC server and register AgentService.
  const server = new grpc.Server();
  server.addService(
    proto.projects.game.AgentService.service,
    handler as any,
  );

  // Bind and start server.
  return new Promise((resolve, reject) => {
    server.bindAsync(
      "0.0.0.0:50051",
      credentials,
      (err, port) => {
        if (err) {
          reject(err);
          return;
        }
        server.start();
        info("gRPC server listening on 0.0.0.0:50051", { port, tls: tlsEnabled });
        resolve(server);
      },
    );
  });
}
