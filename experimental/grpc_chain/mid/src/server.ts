/**
 * server.ts — grpc-chain mid service (grpc-js).
 *
 * Phase 1 (diagnostic): responds directly without forwarding to backend.
 * This isolates whether the Go→JS server connection works independently of
 * the JS→Go client connection.
 *
 * Set CHAIN_MODE=forward to enable full chain forwarding to backend.
 */
import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import type { EchoHandlers } from "../echo_types/experimental/grpc_chain/Echo.js";
import type { ProtoGrpcType } from "../echo_types/echo.js";

// Service root: parent of src/ directory.
const protoRoot = path.join(import.meta.dirname, "..");

const protoIncludeDirs = [protoRoot];

// Proto path at canonical import location under service root.
const protoPath = path.join(protoRoot, "experimental/grpc_chain/echo.proto");

// Dominion resolver target for the Go backend service.
const BACKEND_TARGET = "dominion:///grpc-chain/backend:50051";

const TLS_CA_CERT = "/etc/tls/ca.crt";

function loadProto(): ProtoGrpcType {
	if (!fs.existsSync(protoPath)) {
		throw new Error(`echo.proto not found at ${protoPath}`);
	}

	info("loadProto: loading proto", { protoPath, protoRoot });

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

function buildServerCredentials(): grpc.ServerCredentials {
	const tlsCert = "/etc/tls/tls.crt";
	const tlsKey = "/etc/tls/tls.key";
	const useTLS = fs.existsSync(tlsCert) && fs.existsSync(tlsKey);

	info("buildServerCredentials", { useTLS, tlsCert, tlsKey });

	if (useTLS) {
		return grpc.ServerCredentials.createSsl(
			null,
			[{ cert_chain: fs.readFileSync(tlsCert), private_key: fs.readFileSync(tlsKey) }],
			false,
		);
	}
	return grpc.ServerCredentials.createInsecure();
}

function buildClientCredentials(): grpc.ChannelCredentials {
	const hasCA = fs.existsSync(TLS_CA_CERT);
	info("buildClientCredentials", { hasCA, TLS_CA_CERT });

	if (!hasCA) {
		return grpc.credentials.createInsecure();
	}
	const rootCert = fs.readFileSync(TLS_CA_CERT);
	return grpc.credentials.createSsl(rootCert);
}

function buildChannelOptions(): Record<string, unknown> {
	const serverName = process.env.TLS_SERVER_NAME;
	info("buildChannelOptions", { serverName, hasCA: fs.existsSync(TLS_CA_CERT) });
	if (serverName && fs.existsSync(TLS_CA_CERT)) {
		return { "grpc.ssl_target_name_override": serverName };
	}
	return {};
}

/**
 * Creates and starts the gRPC server.
 *
 * In standalone mode (default): responds directly without forwarding.
 * In forward mode (CHAIN_MODE=forward): forwards to backend via dominion resolver.
 */
export async function startServer(): Promise<grpc.Server> {
	const chainMode = process.env.CHAIN_MODE || "standalone";
	console.error("[server] startServer: chainMode=%s backendTarget=%s", chainMode, BACKEND_TARGET);
	info("startServer: initializing", { chainMode, BACKEND_TARGET });

	registerDominionResolver();
	console.error("[server] dominion resolver registered");

	const proto = loadProto();
	const credentials = buildServerCredentials();
	console.error("[server] proto loaded, credentials built");

	let backendClient: any = null;
	if (chainMode === "forward") {
		console.error("[server] creating forward backend client");
		backendClient = new (proto.experimental.grpc_chain.Echo as any)(
			BACKEND_TARGET,
			buildClientCredentials(),
			buildChannelOptions(),
		);
		info("startServer: backend client created", { target: BACKEND_TARGET });
		console.error("[server] forward backend client created");
	}

	if (!backendClient) {
		console.error("[server] creating diagnostic backend client");
		backendClient = new (proto.experimental.grpc_chain.Echo as any)(
			BACKEND_TARGET,
			buildClientCredentials(),
			buildChannelOptions(),
		);
		info("startServer: unused backend client created", { target: BACKEND_TARGET });
		console.error("[server] diagnostic backend client created");
	}

	const handlers: EchoHandlers = {
		Say: (call, callback) => {
			const reqMsg = call.request.message;
			info("Say: request received", { message: reqMsg, chainMode });

			if (chainMode === "forward" && backendClient) {
				info("Say: forwarding to backend", { message: reqMsg });

				// Forward to backend gRPC server (Go).
				backendClient.say(
					{ message: reqMsg },
					(err: grpc.ServiceError | null, response: any) => {
						if (err) {
							info("Say: backend error", { error: err.message });
							callback(err);
							return;
						}
						info("Say: backend responded", {
							message: response.message,
							chain: response.chain,
						});
						callback(null, {
							message: response.message,
							chain: "mid→" + response.chain,
						});
					},
				);
			} else {
				// Standalone mode: respond directly (no backend forwarding).
				info("Say: responding directly (standalone mode)", { message: reqMsg });
				callback(null, {
					message: "mid:" + reqMsg,
					chain: "mid",
				});
			}
		},
	};

	const server = new grpc.Server();
	server.addService(
		(proto.experimental.grpc_chain.Echo as any).service,
		handlers,
	);

	return new Promise((resolve, reject) => {
		server.bindAsync(
			"0.0.0.0:50051",
			credentials,
			(err, port) => {
				if (err) {
					console.error("[server] bind FAILED: %s", err.message);
					info("startServer: bind failed", { error: err.message });
					reject(err);
					return;
				}
				server.start();
				console.error("[server] gRPC server listening on 0.0.0.0:%d chainMode=%s", port, chainMode);
				info("gRPC mid server listening on 0.0.0.0:50051", { port, tls: true, chainMode });
				resolve(server);
			},
		);
	});
}
