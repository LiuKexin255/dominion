import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import type { GreeterHandlers } from "../greeter_types/experimental/ts/grpc_hello_world/Greeter";
import type { ProtoGrpcType } from "../greeter_types/greeter";

// Service root: the parent directory of the src/ directory.
// In the deployed package, src/server.js is at service/src/server.js,
// so __dirname points to service/src/, and ".." gives us service/.
const protoRoot = path.join(__dirname, "..");

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

	const handlers: GreeterHandlers = {
		SayHello: (call, callback) => {
			info("SayHello", {
				"rpc.service": "Greeter",
				"rpc.method": "SayHello",
				name: call.request.name,
			});
			callback(null, { message: `Hello ${call.request.name}` });
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
