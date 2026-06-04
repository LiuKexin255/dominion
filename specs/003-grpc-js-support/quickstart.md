# Quickstart: gRPC-JS Build Support

**Branch**: `003-grpc-js-support` | **Date**: 2026-06-03

## Overview

This feature adds TypeScript gRPC type generation to the Bazel build system. You can generate type-safe definitions from `.proto` files and implement gRPC services in TypeScript that dynamically load proto definitions at runtime with `@grpc/proto-loader`. Static JavaScript or TypeScript protobuf/gRPC stubs are not generated.

## Prerequisites

- Bazel (already configured in the repository)
- Node.js toolchain (v24.14.0, configured via MODULE.bazel)

## Step 1: Define a Proto File

Create a `.proto` file with your service definition:

```protobuf
// my_service.proto
syntax = "proto3";
package myapp;

service MyService {
  rpc GetItem (GetItemRequest) returns (Item) {}
}

message GetItemRequest {
  string id = 1;
}

message Item {
  string id = 1;
  string name = 2;
}
```

## Step 2: Create a Bazel Build Target

```python
# BUILD.bazel
load("@rules_proto//proto:defs.bzl", "proto_library")
load("//tools/dev/js:ts_proto_library.bzl", "ts_proto_library")
load("@aspect_rules_ts//ts:defs.bzl", "ts_project")
load("@aspect_rules_swc//swc:defs.bzl", "swc")
load("@aspect_rules_js//js:defs.bzl", "js_binary")

proto_library(
    name = "my_service_proto",
    srcs = ["my_service.proto"],
)

ts_proto_library(
    name = "my_service_types",
    proto = ":my_service_proto",
)
```

`ts_proto_library` uses the repository-global `//tools/dev/js:proto_loader_gen_types` executable by default, so ordinary projects do not need to define their own `proto_loader.proto_loader_gen_types_binary` target.

> **TypeScript config note**: Projects consuming `ts_proto_library` generated sources must include both `src/**/*.ts` and `generated/**/*.ts` in `tsconfig.json`, and must not set `rootDir` to `src`. This differs from `experimental/ts/hello_world` because generated proto type files are compiled with the handwritten server source.

## Step 3: Implement the Service

```typescript
// src/server.ts
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import type { ProtoGrpcType } from "../generated/ProtoGrpcType";
import type { MyServiceHandlers } from "../generated/myapp/MyService";

const packageDefinition = protoLoader.loadSync("my_service.proto", {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
});

const proto = grpc.loadPackageDefinition(packageDefinition) as unknown as ProtoGrpcType;

const handlers: MyServiceHandlers = {
    GetItem: (call, callback) => {
        callback(null, { id: call.request.id, name: "Example Item" });
    },
};

const server = new grpc.Server();
server.addService(proto.myapp.MyService.service, handlers);
server.bindAsync("0.0.0.0:50051", grpc.ServerCredentials.createInsecure(), (err, port) => {
    if (err) {
        console.error("Server failed to start:", err);
        process.exit(1);
    }
    console.log(`Server listening on port ${port}`);
});
```

## Step 4: Build and Run

```bash
# Generate TypeScript types from proto
bazel build //path/to:my_service_types

# Build the complete server
bazel build //path/to:server

# Run the server
bazel run //path/to:run

# Run the service acceptance testplan
guitar run path/to/testplan/interface_test.yaml
```

## What Gets Generated

`ts_proto_library` produces TypeScript type files that provide:

- **Message interfaces** — input and output types for each protobuf message
- **Service handler interfaces** — typed handler signatures for implementing the server
- **Service client interfaces** — typed client method signatures for calling the service
- **ProtoGrpcType** — master type for the loaded package definition

It does not produce static protobuf message implementations or gRPC stub implementations. Runtime service descriptors and serialization come from dynamically loading the `.proto` file with `@grpc/proto-loader`.

## Acceptance Testing

The TypeScript gRPC demo is verified through the repository testplan workflow. Because deploy exposes HTTP and not raw gRPC, the testplan launches both the grpc-js service and a Go grpc-gateway adapter from `testplan/gateway/`. The large-test client resolves the adapter HTTP endpoint, calls `GET /experimental/ts/grpc-hello-world/say-hello?name=World` through the `apitest.liukexin.com` route prefix, the adapter forwards to the internal grpc-js `SayHello` RPC, and the test verifies `Hello World` within the configured timeout. Do not validate acceptance by starting the service process inside a unit test.

## Dependency Management

All gRPC dependencies use the centralized catalog in `pnpm-workspace.yaml`:

```json
{
    "dependencies": {
        "@grpc/grpc-js": "catalog:",
        "@grpc/proto-loader": "catalog:"
    }
}
```

## Reference Implementation

See `experimental/ts/grpc_hello_world/` for a complete working example.
