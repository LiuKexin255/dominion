/**
 * server.ts — Game agent gRPC server (TeamService).
 *
 * Loads game.proto, wires the team-service dependencies and registers the
 * TeamService (which replaced AgentService, specs/031-team-template-mode/
 * contracts/api-contract.md §2.2):
 *
 * - **PromptClient** → `getTeamProfile` (the saolei TeamProfile's
 *   player/planner model specs, §2.3).
 * - **ModelProviderCache** → per-model `ChatModel` singletons for the
 *   player/planner agents.
 * - **Mongo client** → `MongoStrategyStore` (`strategies` collection; the
 *   strategy is persisted by the agent service itself, NOT via the prompt
 *   service — strategy-store-contract.md §2). The client resolves the
 *   current `game/mongo` instance via the dominion resolver and derives the
 *   mongo credentials deterministically (same scheme as the Go
 *   `common/gopkg/mongo` client — `dominion/common/gopkg/mongo/client.go`).
 * - **SessionTeamStore** → per-session compiled saolei team graph (buildTeamGraph)
 *   with the saolei MCP tools wired as the player's tools (FR-010).
 * - **MCP host** → per-session saolei McpServer with the team sink injected
 *   (specs/031-team-template-mode/contracts/saolei-sink-contract.md §6;
 *   T009 extension point).
 */

import * as fs from "node:fs";
import * as path from "node:path";
import { createHmac } from "node:crypto";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info, warn } from "@dominion/common-js-logs";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import { createResolver } from "@dominion/common-js-resolver";
import { MongoClient } from "mongodb";
import type { EndpointResolver } from "@dominion/common-js-resolver";
import type { ProtoGrpcType } from "../game_types/game";

import { readSecret } from "./secrets";
import { PromptClient } from "./prompt-client";
import { ModelProviderCache } from "./model-provider";
import type { ChatModel } from "./model-provider";
import { MongoStrategyStore, STRATEGIES_COLLECTION } from "./strategy-store";
import { SessionTeamStore } from "./session-team";
import { SessionTeam } from "./session-team";
import { Handler } from "./handler";
import { startMcpHost, DEFAULT_MCP_PORT } from "./mcp-host";
import { buildSaoleiMcpTools, defaultMcpClientFactory } from "./llm";
import { createEphemeralGameBuffer } from "./team/team-sink";
import { buildTeamGraph } from "./team/graph";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** The mongo database shared with the prompt service (same instance). */
const MONGO_DB_NAME = "game_prompt";

/** The dominion mongo target (app/service — deploy.yaml `infra mongodb`). */
const MONGO_TARGET = { app: "game", service: "mongo", port: { kind: "number", port: 27017 } as const };

// ---------------------------------------------------------------------------
// Mongo client (deterministic credentials — mirrors common/gopkg/mongo)
// ---------------------------------------------------------------------------

const MONGO_PASSWORD_HMAC_KEY = "dominion-mongo-stable-password";
const MONGO_PASSWORD_ALPHABET =
  "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const MONGO_PASSWORD_MIN_LEN = 24;
const MONGO_USERNAME = "admin";
const MONGO_AUTH_DB = "admin";

/**
 * Deterministically derive the mongo admin password — the TS port of
 * `generateStablePassword` (`common/gopkg/mongo/credentials.go`): HMAC-SHA256
 * over the NUL-joined inputs with the fixed domain key, mapped onto the
 * alphabet. Kept byte-identical so the agent connects to the same mongo
 * instance the Go services use.
 */
function generateStablePassword(...inputs: string[]): string {
  const mac = createHmac("sha256", MONGO_PASSWORD_HMAC_KEY);
  mac.update(inputs.map((i) => i.trim()).join("\u0000"));
  const sum = mac.digest();

  let encoded = "";
  for (const b of sum) {
    encoded += MONGO_PASSWORD_ALPHABET[b % MONGO_PASSWORD_ALPHABET.length];
  }
  if (encoded.length >= MONGO_PASSWORD_MIN_LEN) {
    return encoded;
  }
  while (encoded.length < MONGO_PASSWORD_MIN_LEN) {
    for (const b of sum) {
      encoded += MONGO_PASSWORD_ALPHABET[b % MONGO_PASSWORD_ALPHABET.length];
      if (encoded.length >= MONGO_PASSWORD_MIN_LEN) break;
    }
  }
  return encoded;
}

/**
 * Create and connect the mongo client for the current `game/mongo` instance
 * (strategy-store-contract.md §2 — connection config mirrors the prompt
 * service's approach: resolve the endpoint, derive the credentials).
 */
async function createMongoClient(
  resolver: EndpointResolver,
): Promise<MongoClient> {
  const endpoints = await resolver.resolve(MONGO_TARGET);
  if (endpoints.length === 0) {
    throw new Error("resolve mongo endpoint for game/mongo: no ready endpoints found");
  }
  const address = endpoints[0];
  const envName = (process.env.DOMINION_ENVIRONMENT ?? "").trim() || "default";
  const password = generateStablePassword("game", envName, "mongo");
  const uri = `mongodb://${MONGO_USERNAME}:${password}@${address}/${MONGO_AUTH_DB}?authSource=${MONGO_AUTH_DB}`;
  info("mongo client initializing", { address, db: MONGO_DB_NAME });
  const client = new MongoClient(uri);
  await client.connect();
  return client;
}

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

export interface StartServerOverrides {
  /**
   * Model lookup override (DI seam — the test artifact
   * `bootstrap-test.ts` swaps the provider cache for the resolver-aware
   * fake-llm ChatModel; `style/javascript.md` §测试).
   */
  getProvider?: (modelSpec: string) => Promise<ChatModel>;
}

export async function startServer(
  overrides: StartServerOverrides = {},
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
  const getProvider = overrides.getProvider ?? ((spec: string) => providerCache.getProvider(spec));

  // Strategy long-term memory: the agent persists it itself (D4 revision #5);
  // the graph is injected with the mongo-backed store.
  const resolver = createResolver();
  const mongoClient = await createMongoClient(resolver);
  const strategyStore = new MongoStrategyStore(
    mongoClient.db(MONGO_DB_NAME).collection(STRATEGIES_COLLECTION) as never,
  );
  await strategyStore.ensureIndexes();
  info("strategy store ready", { collection: STRATEGIES_COLLECTION });

  // Per-session team: resolve the requested TeamProfile's models (the
  // template + profile name come from the CreateTeam request — no fixed
  // default profile), wire the saolei MCP tools as the player's tools
  // (FR-010/FR-028), compile the team graph.
  const sessionTeamStore = new SessionTeamStore(
    async (sessionId, template, profileName) => {
      const profile = await promptClient.getTeamProfile(
        template,
        profileName,
      );
      const playerModel = await getProvider(profile.playerModel);
      const plannerModel = await getProvider(profile.plannerModel);
      const buffer = createEphemeralGameBuffer();
      const playerTools = await buildSaoleiMcpTools(
        sessionId,
        DEFAULT_MCP_PORT,
        defaultMcpClientFactory,
      );
      const handle = buildTeamGraph({
        playerModel,
        plannerModel,
        strategyStore,
        buffer,
        sessionId,
        playerTools,
      });
      return new SessionTeam(handle, buffer, sessionId);
    },
  );

  const handler = new Handler(sessionTeamStore);

  // FR-001 + saolei-sink-contract.md §6: the localhost MCP HTTP host
  // resolves each session to its bridge AND the team sink (T009 extension
  // point) so the saolei MCP events land in the session's ephemeral buffer.
  startMcpHost((sessionId: string) => {
    const team = sessionTeamStore.get(sessionId);
    return team
      ? { bridge: team.getBridge(), sink: team.getSink() }
      : undefined;
  });

  const proto = loadProto();
  const credentials = buildCredentials();
  const tlsEnabled = fs.existsSync("/etc/tls/tls.crt") && fs.existsSync("/etc/tls/tls.key");

  const server = new grpc.Server({
    "grpc.max_receive_message_length": 8 * 1024 * 1024,
    "grpc.max_send_message_length": 8 * 1024 * 1024,
  });
  server.addService(
    proto.projects.game.TeamService.service,
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
