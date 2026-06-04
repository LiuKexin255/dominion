import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import type { GreeterHandlers } from "../greeter_types/experimental/ts/grpc_hello_world/Greeter";
import type { ProtoGrpcType } from "../greeter_types/greeter";

const protoPath = path.join(__dirname, "..", "greeter.proto");
const protoIncludeDirs = [path.join(__dirname, "..")];

for (const workspaceName of ["googleapis+", "googleapis"]) {
	const includeDir = path.join(process.env.RUNFILES_DIR || "", workspaceName);
	if (fs.existsSync(path.join(includeDir, "google/api/annotations.proto"))) {
		protoIncludeDirs.push(includeDir);
	}
}

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

const proto = grpc.loadPackageDefinition(
	packageDefinition,
) as unknown as ProtoGrpcType;

// Implement handlers
const handlers: GreeterHandlers = {
	SayHello: (call, callback) => {
		callback(null, { message: `Hello ${call.request.name}` });
	},
};

const tlsCert = "/etc/tls/tls.crt";
const tlsKey = "/etc/tls/tls.key";
const useTLS = fs.existsSync(tlsCert) && fs.existsSync(tlsKey);

let credentials: grpc.ServerCredentials;
if (useTLS) {
	credentials = grpc.ServerCredentials.createSsl(
		null,
		[{ cert_chain: fs.readFileSync(tlsCert), private_key: fs.readFileSync(tlsKey) }],
		false,
	);
} else {
	credentials = grpc.ServerCredentials.createInsecure();
}

// Start server
const server = new grpc.Server();
server.addService(
	proto.experimental.ts.grpc_hello_world.Greeter.service,
	handlers,
);
server.bindAsync(
	"0.0.0.0:50051",
	credentials,
	(err, port) => {
		if (err) {
			console.error("Failed to bind:", err);
			process.exit(1);
		}
		server.start();
		console.log(
			`gRPC server listening on port ${port} (${useTLS ? "TLS" : "insecure"})`,
		);
	},
);
