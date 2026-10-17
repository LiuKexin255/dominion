/**
 * server.ts — redis-demo js service (grpc-js).
 *
 * Implements the same KvService as the Go service over one shared redis
 * instance. The resource name is used as the redis key verbatim, which is the
 * mapping the Go service uses too, so a value written by one language is
 * readable by the other
 * (specs/067-redis-infra-support/contracts/redis-demo.md §2).
 */
import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { error, info } from "@dominion/common-js-logs";
import { createRedisClient } from "@dominion/common-js-redis";
import type { RedisClient } from "@dominion/common-js-redis";
import type { KvServiceHandlers } from "../redis_demo_types/experimental/redis_demo/KvService.js";
import type { ProtoGrpcType } from "../redis_demo_types/redis_demo.js";

// Target of the redis instance declared in the testplan deploy.yaml
// (specs/067-redis-infra-support/contracts/redis-demo.md §1).
const redisTarget = "redis-demo/redis";

const grpcAddress = "0.0.0.0:50051";

// Service root: parent of the src/ directory. artifact_pkg_js places the proto
// at its canonical import path below that root.
const protoRoot = path.join(import.meta.dirname, "..");
const protoPath = path.join(protoRoot, "experimental/redis_demo/redis_demo.proto");

const tlsCertPath = "/etc/tls/tls.crt";
const tlsKeyPath = "/etc/tls/tls.key";

function loadProto(): ProtoGrpcType {
	if (!fs.existsSync(protoPath)) {
		throw new Error(`redis_demo.proto not found at ${protoPath}`);
	}

	const packageDefinition = protoLoader.loadSync(protoPath, {
		longs: String,
		enums: String,
		defaults: true,
		oneofs: true,
		includeDirs: [protoRoot],
	});

	return grpc.loadPackageDefinition(packageDefinition) as unknown as ProtoGrpcType;
}

// Uses the TLS material the deployment injects; the insecure fallback keeps
// local runs (no /etc/tls) usable, mirroring experimental/grpc_chain/mid.
function buildServerCredentials(): grpc.ServerCredentials {
	if (fs.existsSync(tlsCertPath) && fs.existsSync(tlsKeyPath)) {
		return grpc.ServerCredentials.createSsl(
			null,
			[{ cert_chain: fs.readFileSync(tlsCertPath), private_key: fs.readFileSync(tlsKeyPath) }],
			false,
		);
	}
	return grpc.ServerCredentials.createInsecure();
}

/** Handle over the started service; close releases the server and the redis client. */
export interface RedisDemoService {
	server: grpc.Server;
	close(): Promise<void>;
}

function notFoundError(name: string): { code: grpc.status; message: string } {
	return { code: grpc.status.NOT_FOUND, message: `kv entry ${name} not found` };
}

function internalError(operation: string, err: unknown): { code: grpc.status; message: string } {
	const detail = err instanceof Error ? err.message : String(err);
	return { code: grpc.status.INTERNAL, message: `${operation}: ${detail}` };
}

function buildHandlers(client: RedisClient): KvServiceHandlers {
	return {
		GetKvEntry: (call, callback) => {
			const name = call.request.name;
			info("GetKvEntry: reading redis key", { name });

			client.get(name).then(
				(value) => {
					if (value === null) {
						callback(notFoundError(name));
						return;
					}
					callback(null, { name, value });
				},
				(err: unknown) => callback(internalError(`get kv entry ${name}`, err)),
			);
		},

		SetKvEntry: (call, callback) => {
			const name = call.request.name;
			const value = call.request.value ?? "";
			info("SetKvEntry: writing redis key", { name });

			client.set(name, value).then(
				() => callback(null, { name, value }),
				(err: unknown) => callback(internalError(`set kv entry ${name}`, err)),
			);
		},

		DeleteKvEntry: (call, callback) => {
			const name = call.request.name;
			info("DeleteKvEntry: deleting redis key", { name });

			client.del(name).then(
				(deleted) => {
					if (deleted === 0) {
						callback(notFoundError(name));
						return;
					}
					callback(null, {});
				},
				(err: unknown) => callback(internalError(`delete kv entry ${name}`, err)),
			);
		},
	};
}

function bindServer(server: grpc.Server, credentials: grpc.ServerCredentials): Promise<void> {
	return new Promise((resolve, reject) => {
		server.bindAsync(grpcAddress, credentials, (err) => {
			if (err) {
				reject(err);
				return;
			}
			server.start();
			resolve();
		});
	});
}

/**
 * Connects redis, loads the proto and starts the gRPC server. The returned
 * handle owns both resources so the bootstrap shutdown path can release them
 * (specs/067-redis-infra-support/research.md D10).
 */
export async function startServer(): Promise<RedisDemoService> {
	const proto = loadProto();
	const client = await createRedisClient(redisTarget);
	// node-redis surfaces socket errors (including reconnection failures) as
	// 'error' events on the client. An EventEmitter 'error' event with no
	// listener is rethrown as an uncaught exception, so without this listener a
	// redis blip would crash the process instead of failing the in-flight
	// command (https://github.com/redis/node-redis — client 'error' event).
	// Log only: recovery is the driver's reconnect logic, not process exit.
	client.on("error", (err: Error) => {
		error("redis client error", { target: redisTarget, err });
	});
	info("redis client connected", { target: redisTarget });

	const server = new grpc.Server();
	server.addService(
		(proto.experimental.redis_demo.KvService as any).service,
		buildHandlers(client),
	);

	try {
		await bindServer(server, buildServerCredentials());
	} catch (err) {
		await client.disconnect();
		throw err;
	}
	info("gRPC server listening", { address: grpcAddress });

	return {
		server,
		async close(): Promise<void> {
			server.forceShutdown();
			await client.disconnect();
		},
	};
}
