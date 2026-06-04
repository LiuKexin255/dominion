import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import type { ProtoGrpcType } from "../greeter_types/greeter";
import type { GreeterHandlers } from "../greeter_types/experimental/ts/grpc_hello_world/Greeter";
import * as path from "path";

// Resolve proto file path via Bazel runfiles
const protoPath = path.join(
  process.env.RUNFILES_DIR || "",
  "dominion",
  "experimental/ts/grpc_hello_world/greeter.proto"
);

// Load proto definition.
// Options MUST match the ts_proto_library generation options:
//   longs=String, enums=String, defaults=true, oneofs=true, keep_case=False
// keep_case=False means do NOT pass keepCase (defaults to false in loadSync).
const packageDefinition = protoLoader.loadSync(protoPath, {
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
  // keepCase omitted (keep_case=False in the rule)
});

const proto = grpc.loadPackageDefinition(
  packageDefinition
) as unknown as ProtoGrpcType;

// Implement handlers
const handlers: GreeterHandlers = {
  SayHello: (call, callback) => {
    callback(null, { message: "Hello " + call.request.name });
  },
};

// Start server
const server = new grpc.Server();
server.addService(proto.experimental.ts.grpc_hello_world.Greeter.service, handlers);
server.bindAsync(
  "0.0.0.0:50051",
  grpc.ServerCredentials.createInsecure(),
  (err, port) => {
    if (err) {
      console.error("Failed to bind:", err);
      process.exit(1);
    }
    server.start();
    console.log(`gRPC server listening on port ${port}`);
  }
);
