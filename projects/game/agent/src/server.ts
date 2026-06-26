/**
 * server.ts — Game agent gRPC server.
 *
 * Loads game.proto, wires service dependencies (secret, prompt client,
 * ModelProviderCache, SessionAgentStore, shared MemorySaver, compiled
 * StateGraph, handler), registers AgentService on a gRPC server, and
 * starts listening.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info, warn } from "@dominion/common-js-logs";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import {
  MemorySaver,
} from "@langchain/langgraph";
import type { ProtoGrpcType } from "../game_types/game";

import { readSecret } from "./secrets";
import { PromptClient } from "./prompt-client";
import { ModelProviderCache } from "./model-provider";
import { AgentAdapterImpl } from "./llm";
import type { AdapterFactory } from "./llm";
import { SessionAgentStore } from "./session-agent";
import { Handler } from "./handler";

// ---------------------------------------------------------------------------
// Proto loading
// ---------------------------------------------------------------------------

const protoRoot = path.join(__dirname, "..");
const protoPath = path.join(protoRoot, "projects", "game", "game.proto");
const protoIncludeDirs = [protoRoot];

function loadProto(): ProtoGrpcType {
  if (!fs.existsSync(protoPath)) {
    throw new Error(`game.proto not found at ${protoPath}`);
  }

  const packageDefinition = protoLoader.loadSync(protoPath, {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
    includeDirs: protoIncludeDirs,
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

export async function startServer(
  adapterFactoryOverride?: AdapterFactory,
): Promise<grpc.Server> {
  registerDominionResolver();

  const providerSecret = readSecret(
    path.join(process.env.DOMINION_SECRET_DIR || "/etc/secrets", "provider"),
  );

  const promptClient = new PromptClient();
  const promptReady = await promptClient.warmup();
  if (promptReady) {
    info("prompt service connection pre-warmed");
  } else {
    warn("prompt service not ready after warmup; deferring to first RPC");
  }

  const checkpointer = new MemorySaver();

  const openaiBaseUrl =
    process.env.OPENCODE_OPENAI_BASE_URL ||
    process.env.OPENCODE_BASE_URL ||
    "https://opencode.ai/zen/go/v1";
  const anthropicBaseUrl =
    process.env.OPENCODE_ANTHROPIC_BASE_URL ||
    "https://opencode.ai/zen/go";
  const providerCache = new ModelProviderCache(
    openaiBaseUrl,
    anthropicBaseUrl,
    providerSecret,
  );

  const adapterFactory: AdapterFactory =
    adapterFactoryOverride ??
    (async (getProvider, systemPrompt, toolNames, bridge, cp) => {
      const chatModel = await getProvider();
      return new AgentAdapterImpl(chatModel, systemPrompt, toolNames, bridge, cp);
    });

  const sessionAgentStore = new SessionAgentStore(
    (modelSpec: string) => providerCache.getProvider(modelSpec),
    adapterFactory,
    checkpointer,
  );

  const handler = new Handler(
    promptClient,
    sessionAgentStore,
  );

  const proto = loadProto();
  const credentials = buildCredentials();
  const tlsEnabled = fs.existsSync("/etc/tls/tls.crt") && fs.existsSync("/etc/tls/tls.key");

  const server = new grpc.Server({
    "grpc.max_receive_message_length": 8 * 1024 * 1024,
    "grpc.max_send_message_length": 8 * 1024 * 1024,
  });
  server.addService(
    proto.projects.game.AgentService.service,
    handler as any,
  );

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
