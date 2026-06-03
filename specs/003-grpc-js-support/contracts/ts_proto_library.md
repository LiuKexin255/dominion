# Contract: ts_proto_library Bazel Rule

**Branch**: `003-grpc-js-support` | **Date**: 2026-06-03

## Rule API

### `ts_proto_library`

Generates TypeScript type files from `.proto` files using the officially recommended `proto-loader-gen-types` tool. The generated files provide compile-time types for services that load `.proto` files dynamically with `@grpc/proto-loader`; this rule does not generate static JavaScript or TypeScript protobuf/gRPC stubs.

```python
load("//tools/proto:ts_proto_library.bzl", "ts_proto_library")
```

#### Attributes

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `name` | `string` | yes | — | Target name |
| `proto` | `label` | yes | — | `proto_library` target to generate types from |
| `grpc_lib` | `string` | no | `"@grpc/grpc-js"` | Import path for the gRPC library in generated types |
| `longs` | `string` | no | `"String"` | How to represent int64 fields |
| `enums` | `string` | no | `"String"` | How to represent enum values |
| `defaults` | `bool` | no | `True` | Whether to set default values on output types |
| `oneofs` | `bool` | no | `True` | Whether to include oneof fields |
| `keep_case` | `bool` | no | `False` | Whether to preserve proto field names; maps to `--keepCase` and runtime `keepCase` |

#### Providers

| Provider | Fields | Description |
|----------|--------|-------------|
| `DefaultInfo` | `files` | Generated `.ts` type files consumable by `ts_project` |

#### Usage Example

```python
load("@rules_proto//proto:defs.bzl", "proto_library")
load("//tools/proto:ts_proto_library.bzl", "ts_proto_library")

proto_library(
    name = "greeter_proto",
    srcs = ["greeter.proto"],
)

ts_proto_library(
    name = "greeter_types",
    proto = ":greeter_proto",
)
```

#### Generated Output Structure

For a proto with package `example.greeter` containing `Greeter` service and `HelloRequest`/`HelloReply` messages:

```
bazel-out/.../generated/
  example/
    greeter/
      Greeter.ts              # GreeterClient + GreeterHandlers types
      HelloRequest.ts         # HelloRequest input interface
      HelloRequest__Output.ts # HelloRequest output interface
      HelloReply.ts           # HelloReply input interface
      HelloReply__Output.ts   # HelloReply output interface
  ProtoGrpcType.ts            # Master type for loaded package
```

#### Constraints

- Input MUST be a `proto_library` target (provides `ProtoInfo`)
- Generated files MUST NOT be committed to source control
- Rule MUST handle transitive proto dependencies (for Google API annotation support)
- Output `.ts` files are consumable as `srcs` by `ts_project`
- Static protobuf/gRPC JavaScript or TypeScript stubs are not produced by this rule

#### ts_project Integration

`ts_proto_library` invokes `proto-loader-gen-types` with `--outDir` pointing at a declared Bazel output directory named `generated`. Downstream TypeScript projects that consume the generated files MUST include that generated layout in `ts_project` sources and TypeScript configuration:

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "strict": true,
    "declaration": true
  },
  "include": ["src/**/*.ts", "generated/**/*.ts"]
}
```

Do not set `rootDir` to `src` for a target that compiles `:greeter_types`, because generated files are outside `src/`. From `src/server.ts`, generated imports use `../generated/...`.

---

## Contract: Example gRPC Service

### Proto Definition

The example service defines a Greeter service with a single unary RPC:

```protobuf
syntax = "proto3";
package experimental.ts.grpc_hello_world;

service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply) {}
}

message HelloRequest {
  string name = 1;
}

message HelloReply {
  string message = 1;
}
```

### Server Interface

The server dynamically loads the `.proto` file at runtime and implements typed handlers using generated types:

```typescript
import type { ProtoGrpcType } from "../generated/ProtoGrpcType";
import type {
  GreeterHandlers,
} from "../generated/experimental/ts/grpc_hello_world/Greeter";

const handlers: GreeterHandlers = {
  SayHello: (call, callback) => {
    callback(null, { message: "Hello " + call.request.name });
  },
};
```

The runtime `protoLoader.loadSync()` options MUST match the generation options used by `ts_proto_library` (`longs`, `enums`, `defaults`, `oneofs`, and `keep_case`/`keepCase`). The default is `keep_case = False`, so runtime loading omits `keepCase` unless the rule sets `keep_case = True`. The `.proto` file and any transitive imported protos must be available in the server binary runfiles/data.

### BUILD.bazel Contract

```python
load("@rules_proto//proto:defs.bzl", "proto_library")
load("@aspect_rules_ts//ts:defs.bzl", "ts_project")
load("@aspect_rules_swc//swc:defs.bzl", "swc")
load("@aspect_rules_js//js:defs.bzl", "js_binary")
load("//tools/proto:ts_proto_library.bzl", "ts_proto_library")

proto_library(
    name = "greeter_proto",
    srcs = ["greeter.proto"],
)

ts_proto_library(
    name = "greeter_types",
    proto = ":greeter_proto",
)

ts_project(
    name = "server",
    srcs = [
        "src/server.ts",
        ":greeter_types",
    ],
    declaration = True,
    transpiler = swc,
    tsconfig = ":tsconfig.json",
)

js_binary(
    name = "run",
    data = [
        ":server",
        "greeter.proto",
    ],
    entry_point = "src/server.js",
)
```

### Testplan Contract

The example service acceptance test is testplan-based. It launches the compiled TypeScript server plus a Go HTTP wrapper from the testplan suite as the system under test. Deploy exposes the testplan-owned wrapper over HTTP; the wrapper calls the TypeScript server over gRPC. Unit tests MUST NOT start the service process in-process for this acceptance path.

Required acceptance behavior:

1. Launch the TypeScript gRPC server artifact through the repository testplan/deploy workflow.
2. Resolve the wrapper HTTP endpoint from the testplan-provided environment using `common/gopkg/testtool`.
3. Send HTTP request `GET /say-hello?name=World` to the wrapper.
4. Wrapper calls grpc-js `SayHello` over gRPC and returns `Hello World` within a 5-second client deadline.
5. Let the testplan workflow clean up both the grpc-js service and the testplan-owned wrapper service.
