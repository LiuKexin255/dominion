/**
 * server.ts — Game agent gRPC server.
 *
 * Loads game.proto, wires service dependencies (secret, prompt client,
 * LLM adapter, runtime instances, handler), registers AgentService on
 * a gRPC server, and starts a periodic cleanup loop for idle runtimes.
 *
 * Exports startServer() invoked by bootstrap.ts after OTel init.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import type { ProtoGrpcType } from "../game_types/game";

import { readSecret } from "./secrets";
import { PromptClient } from "./prompt-client";
import { RealLLMAdapter, type LLMAdapter, type LLMProvider } from "./llm";
import { DialogRuntime } from "./runtime";
import { Handler } from "./handler";

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
 * Reads the provider secret, creates the prompt client and LLM adapter,
 * wires the Handler with a shared runtime instances map, loads the proto
 * definition, registers the AgentService, and binds to port 50051 on all
 * interfaces. A 1-minute cleanup interval periodically prunes idle
 * runtime instances (inactivity > 15 minutes).
 *
 * @returns A promise that resolves to the started gRPC Server instance.
 */
export async function startServer(llmAdapterOverride?: LLMAdapter): Promise<grpc.Server> {
	// Register dominion URI resolver for service discovery.
	registerDominionResolver();

	// 1. Read provider secret (empty string if file is missing).
	const providerSecret = readSecret(
		path.join(process.env.DOMINION_SECRET_DIR || "/etc/secrets", "provider"),
	);

	// 2. Create PromptClient (connects to prompt service via dominion).
	const promptClient = new PromptClient();

	// 3. Create LLM adapter (fake for test, real for production).
	const llmAdapter: LLMAdapter = llmAdapterOverride ?? (() => {
		const modelName = process.env.MODEL_NAME || "deepseek-v4-pro";
		const baseUrl = process.env.OPENCODE_BASE_URL || "https://opencode.ai/zen/go/v1";
		const providerEnv = process.env.LLM_PROVIDER;
		const provider: LLMProvider | undefined =
			providerEnv === "openai" || providerEnv === "anthropic" ? providerEnv : undefined;
		return new RealLLMAdapter(modelName, baseUrl, provider);
	})();

	// 4. Create shared runtime instances map.
	const instances = new Map<string, DialogRuntime>();

	// 5. Create Handler with all dependencies.
	const handler = new Handler(instances, promptClient, llmAdapter, providerSecret);

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

	// 6. Start 1-minute cleanup interval for idle instances (>15 min).
	const cleanupInterval = setInterval(() => {
		for (const [sessionId, instance] of instances) {
			if (instance.cleanup(15 * 60 * 1000)) {
				instances.delete(sessionId);
				info("cleaned idle runtime instance", { sessionId });
			}
		}
	}, 60000);

	// Bind and start server.
	return new Promise((resolve, reject) => {
		server.bindAsync(
			"0.0.0.0:50051",
			credentials,
			(err, port) => {
				if (err) {
					clearInterval(cleanupInterval);
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
