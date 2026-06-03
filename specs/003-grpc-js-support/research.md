# Research: gRPC-JS Build Support

**Branch**: `003-grpc-js-support` | **Date**: 2026-06-03

## R1: Official grpc-node Tooling

### Decision: Use @grpc/grpc-js + @grpc/proto-loader + proto-loader-gen-types

**Rationale**: The official grpc/grpc-node repository explicitly recommends:
- `@grpc/grpc-js` (v1.14.4) — pure JavaScript gRPC runtime, replacing the deprecated native `grpc` package
- `@grpc/proto-loader` (v0.8.1) — proto file loading + TypeScript type generation
- `proto-loader-gen-types` — CLI tool shipped with `@grpc/proto-loader` for generating TypeScript type definitions from `.proto` files (formalized in gRPC proposal L70)

The officially recommended TypeScript pattern is **dynamic proto loading with build-time type generation**:
1. **Build time**: `proto-loader-gen-types` generates `.ts` type files (interfaces, handlers, clients)
2. **Runtime**: `@grpc/proto-loader` loads `.proto` files dynamically for message serialization
3. **Type safety**: Generated `.ts` types provide compile-time checking

**Alternatives considered**:
- `grpc-tools` (static JS codegen) — generates `.js` only, no TypeScript support; requires third-party `grpc_tools_node_protoc_ts` for `.d.ts`
- `ts-proto` (third-party) — excellent idiomatic TS output but not an official grpc-node tool
- `@bufbuild/protoc-gen-es` (Buf ecosystem) — modern and well-maintained but not part of the grpc-node project
- `protobuf-ts` — mature but not official

---

## R2: Code Generation Architecture

### Decision: Dynamic loading only — proto-loader-gen-types for TS types

**Rationale**: The feature scope is dynamic gRPC loading with compile-time TypeScript safety. Static JavaScript or TypeScript protobuf/gRPC stubs are not required. The official dynamic loading approach is therefore sufficient and preferred.

The plan combines:
1. **proto-loader-gen-types** → `.ts` type files for messages, service handlers, clients, and `ProtoGrpcType`
2. **@grpc/proto-loader** → runtime `.proto` loading for serialization and service descriptors
3. **@grpc/grpc-js** → runtime gRPC server/client implementation

`ts_project` compiles the generated `.ts` type files as part of normal TypeScript compilation, but any emitted `.js` for type-only files is not a generated protobuf/gRPC stub and is not part of the public codegen contract.

**Alternatives considered**:
- Static JS codegen with `grpc-tools` — rejected because static stubs are outside the clarified feature scope and would add a second generated artifact family
- `ts-proto` or `protobuf-ts` — rejected because they are not the official grpc-node dynamic-loading path requested for this feature
- Runtime dynamic loading without generated types — rejected because it loses compile-time handler/client shape checking

---

## R3: Custom Bazel Rule Design

### Decision: Create `ts_proto_library` custom rule in `tools/proto/`

**Rationale**: The user explicitly mentioned "必要时可以提供类似 js_proto_library/ts_proto_library 等自定义 rules 进行封装". Since no existing Bazel rule exactly matches the proto-loader-gen-types workflow, a custom rule is the right approach.

**Rule design**:
```python
ts_proto_library(
    name = "greeter_types",
    proto = ":greeter_proto",           # proto_library target
    grpc_lib = "@grpc/grpc-js",         # generated type import path
    longs = "String",
    enums = "String",
    defaults = True,
    oneofs = True,
    keep_case = False,                  # maps to runtime keepCase; false by default
)
# Produces .ts type files consumable by ts_project
```

**Implementation strategy**:
1. Accept `proto_library` target via `ProtoInfo` provider
2. Extract `transitive_sources` and `transitive_proto_path` for dependency resolution
3. Run `proto-loader-gen-types` via a Bazel executable action, passing `--outDir` as the declared generated output directory
4. Declare generated `.ts` files under a `generated/` output layout
5. Make outputs available to `ts_project` as `srcs`

**Include path handling** (for FR-003 Google API annotations):
- Proto files that import `google/api/annotations.proto` need these proto files resolvable
- The custom rule stages transitive proto sources in a directory structure matching import paths
- `proto-loader-gen-types` is invoked with the staged directory as the base

**Alternatives considered**:
- `aspect_rules_js` `js_proto_toolchain` — experimental, doesn't support pure-TS plugins like proto-loader-gen-types
- `rules_proto_grpc` `js_grpc_library` — mature but uses different tooling (protoc-gen-js + ts-protoc-gen)
- Manual genrule per proto — no abstraction, error-prone
- Custom rule provides the best fit: official tools + repo conventions

---

## R4: Proto File Strategy

### Decision: Example uses a new simple proto; rule supports complex protos with Google API annotations

**Rationale**:
- The spec (FR-005) says "define a .proto file" — the example should be self-contained
- The spec (FR-003) says "support proto files that import Google API annotations" — the rule must handle these
- Separating the two concerns keeps the example clean while ensuring the build system is general

**Example proto** (`experimental/ts/grpc_hello_world/greeter.proto`):
- Simple Greeter service with one unary RPC `SayHello`
- No Google API annotations (keeps the example focused)
- Package: `experimental.ts.grpc_hello_world`

**Build system validation for FR-003**:
- The custom rule accepts `proto_library` deps that include `@googleapis//google/api:annotations_proto`
- Include path staging ensures `google/api/annotations.proto` resolves correctly
- Validated by building a proto target with Google API imports (can reuse existing Go proto targets)

**Alternatives considered**:
- Reuse existing `experimental/golang/grpc_hello_world/hello_world.proto` — couples TS to Go directory structure, adds Google API annotation complexity to the example
- Create proto in a shared location — adds indirection, not self-contained

---

## R5: Project Structure & Conventions

### Decision: Follow hello_world project conventions exactly

**Rationale**: The spec (FR-006) explicitly requires following the `experimental/ts/hello_world` conventions.

**Convention checklist**:
| Aspect | Convention | Source |
|--------|-----------|--------|
| Package naming | `@dominion/grpc_hello_world` | hello_world uses `@dominion/hello_world` |
| Visibility | `"private": true` | hello_world pattern |
| Dependencies | `"catalog:"` protocol | hello_world pattern, constitution |
| Module system | CommonJS | hello_world tsconfig |
| Target | ES2020 | hello_world tsconfig |
| Compilation | SWC (via ts_project transpiler) | hello_world BUILD.bazel |
| Strict mode | Yes | hello_world tsconfig |
| Declarations | Yes | hello_world tsconfig |
| BUILD files | Manual (Gazelle doesn't generate TS) | Root BUILD.bazel Gazelle config |

**New catalog entries needed**:
- `@grpc/grpc-js` — gRPC runtime
- `@grpc/proto-loader` — proto loading + type generation CLI

**Alternatives considered**: None — conventions are fixed by spec requirement.

---

## R6: Dependency Management

### Decision: Add @grpc/grpc-js and @grpc/proto-loader to pnpm-workspace.yaml catalog

**Rationale**: Constitution requires "TypeScript/JavaScript dependency versions MUST be centralized in the root `pnpm-workspace.yaml` catalog". No documented special exceptions exist for gRPC packages.

**Catalog additions**:
```yaml
catalog:
  "@grpc/grpc-js": "^1.14.0"
  "@grpc/proto-loader": "^0.8.0"
```

**Package manifest** (`experimental/ts/grpc_hello_world/package.json`):
```json
{
    "name": "@dominion/grpc_hello_world",
    "private": true,
    "dependencies": {
        "@grpc/grpc-js": "catalog:",
        "@grpc/proto-loader": "catalog:"
    },
    "devDependencies": {
        "typescript": "catalog:"
    }
}
```

**Alternatives considered**: Inline versions — rejected per constitution.

---

## R7: Testing Strategy

### Decision: Testplan-based acceptance test for the example service

**Rationale**:
- `style/large_test.md` says gRPC services need large-test/testplan coverage
- The user explicitly requires a testplan for the TypeScript demo and disallows starting the service process inside a unit test
- A testplan verifies the actual artifact through a deployed HTTP wrapper owned by the testplan suite, matching the repository large-test guidance that gRPC systems are tested through HTTP when deploy does not expose raw gRPC
- Repository build verification (`bazel build //...` and `bazel test //...`) validates no regressions

**Test approach**:
- Build the server artifact through Bazel
- Launch the TypeScript demo service and suite-owned HTTP wrapper as the system under test through the repository testplan/deploy workflow
- Use a Go HTTP large-test case to call the deployed wrapper endpoint `GET /say-hello?name=World`; the wrapper calls grpc-js `SayHello("World")` internally
- Verify the response is `Hello World`
- Let the testplan workflow clean up the launched service
- Build verification: Full `bazel build //...` and `bazel test //...`

**Alternatives considered**:
- Unit test that starts the server in-process — rejected by user requirement; it does not exercise service launch as an artifact
- No testplan — rejected because gRPC service behavior should be observable through the repository testplan workflow

---

## R8: Bazel Module Dependencies

### Decision: No new MODULE.bazel entries needed

**Rationale**: The existing Bazel modules already provide everything needed:
- `rules_proto 7.1.0` — proto_library rule
- `protobuf 33.4` — protoc compiler (for include paths)
- `aspect_rules_ts 3.8.8` — ts_project rule
- `aspect_rules_js 3.0.3` — js_binary, js_library rules
- `aspect_rules_swc 2.7.1` — SWC transpiler
- `rules_nodejs 6.7.4` — Node.js toolchain (v24.14.0)

The new npm packages (`@grpc/grpc-js`, `@grpc/proto-loader`) are added via pnpm catalog, not MODULE.bazel.

**Alternatives considered**: Adding `rules_proto_grpc` — rejected because we're writing custom rules using official tools, not using rules_proto_grpc's tooling.
