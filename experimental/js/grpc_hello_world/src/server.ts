import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { readConfig } from "@dominion/common-js-config";
import { info } from "@dominion/common-js-logs";
import type { GreeterHandlers } from "../greeter_types/experimental/ts/grpc_hello_world/Greeter.js";
import type { ProtoGrpcType } from "../greeter_types/greeter.js";

/** Greeting data shape declared in the service.yaml `service_config` config block. */
interface Greeting {
	message: string;
	times: number;
}

// Fallback merged with the config entry content: keys absent from the config
// keep their defaults (deep merge semantics, contracts/sdk-js.md §1).
const defaultGreeting: Greeting = { message: "hello", times: 1 };

// User env appended to the greeting when set. Config parameters and env
// parameters coexist without interference (FR-016/SC-006, specs/045-deploy-config/spec.md).
const greetingSuffix = process.env.GREETING_SUFFIX ?? "";
// Leading space when the env is set, empty otherwise — avoids a trailing
// space in the greeting when GREETING_SUFFIX is unset.
const greetingSuffixSegment = greetingSuffix === "" ? "" : ` ${greetingSuffix}`;

// Service root: the parent directory of the src/ directory.
// In the deployed package, src/server.js is at service/src/server.js,
// so import.meta.dirname points to service/src/, and ".." gives us service/.
const protoRoot = path.join(import.meta.dirname, "..");

// Proto files are placed at their canonical import paths under the service root.
// For example: service/experimental/ts/grpc_hello_world/greeter.proto
const protoPath = path.join(protoRoot, "experimental/ts/grpc_hello_world/greeter.proto");

// All proto dependencies (e.g. google/api/annotations.proto) are also under
// the service root, so a single includeDir covers all imports.
const protoIncludeDirs = [protoRoot];

function loadProto(): ProtoGrpcType {
	if (!fs.existsSync(protoPath)) {
		throw new Error(`greeter.proto not found at ${protoPath}`);
	}

	// Load proto definition.
	// Options MUST match the ts_proto_library generation options:
	//   longs=String, enums=String, defaults=true, oneofs=true, keep_case=False
	// keep_case=False means do NOT pass keepCase (defaults to false in loadSync).
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

/**
 * Creates and starts the gRPC server.
 *
 * Loads the proto definition, registers the Greeter service handlers,
 * and binds to port 50051 on all interfaces.
 *
 * @returns A promise that resolves to the started gRPC Server instance.
 */
export async function startServer(): Promise<grpc.Server> {
	const proto = loadProto();
	const credentials = buildCredentials();

	// Read the greeting config entry once at service startup (config is not on
	// the hot path; contracts/sdk-js.md §2 "同步读取"). Throws when
	// DOMINION_CONFIG_DIR is unset or the entry is missing — the service fails
	// fast instead of silently serving defaults (runtime-contract.md §3).
	const greeting = readConfig<Greeting>("service_config", "greeting", defaultGreeting);

	const handlers: GreeterHandlers = {
		SayHello: (call, callback) => {
			info("SayHello", {
				"rpc.service": "Greeter",
				"rpc.method": "SayHello",
				name: call.request.name,
			});
			// The response proves both the config override (message/times) and
			// the GREETING_SUFFIX user env; either field alone would not prove
			// FR-015 deep merge or SC-006 config/env coexistence.
			callback(null, {
				message: `${greeting.message} ${call.request.name} x${greeting.times}${greetingSuffixSegment}`,
			});
		},
	};

	const server = new grpc.Server();
	server.addService(
		proto.experimental.ts.grpc_hello_world.Greeter.service,
		handlers,
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
				info("gRPC server listening", { port, tls: true });
				resolve(server);
			},
		);
	});
}
