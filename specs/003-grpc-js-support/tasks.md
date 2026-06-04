---

description: "Task list for gRPC-JS Build Support & Example Service"
---

# Tasks: gRPC-JS Build Support & Example Service

**Input**: Design documents from `/specs/003-grpc-js-support/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ts_proto_library.md, quickstart.md

**Tests**: This feature includes testplan-based acceptance testing (FR-009). No unit tests start the service process in-process for acceptance. The TypeScript grpc-js service is verified through the repository testplan workflow by deploying a suite-owned Go grpc-gateway HTTP adapter; the Go `go_largetest` client resolves the adapter HTTP endpoint with `common/gopkg/testtool`, calls the HTTP endpoint through the `apitest.liukexin.com` route prefix, and the adapter forwards to the internal gRPC service.

**Constitution**: Generated tasks MUST satisfy `.specify/memory/constitution.md`. Include repository-specific verification tasks for style-guide review, formatting, Gazelle/dependency synchronization, pnpm catalog updates for TypeScript/JavaScript dependency versions, large-test/testplan execution for service code, and full-repository Bazel build/test validation.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **JS/TS tooling**: `tools/dev/js/` contains JavaScript/TypeScript build rules and helper executables, including `ts_proto_library.bzl` and the global `proto_loader_gen_types` target
- **Release tooling**: `tools/release/defs.bzl` owns `artifact_image`; extend it for Node/grpc-js while preserving existing Go defaults
- **Example service**: `experimental/ts/grpc_hello_world/` follows `experimental/ts/hello_world/` conventions except where generated TypeScript sources require a widened tsconfig
- **Service deploy material**: `experimental/ts/grpc_hello_world/service/service.yaml`, `experimental/ts/grpc_hello_world/testplan/gateway/service.yaml`, and `experimental/ts/grpc_hello_world/testplan/deploy.yaml`
- **Testplan**: `experimental/ts/grpc_hello_world/testplan/` contains Go `go_largetest` HTTP acceptance code; `experimental/ts/grpc_hello_world/testplan/gateway/` contains the suite-owned Go grpc-gateway adapter

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Read required guidance, add npm dependencies, and configure repository ignore rules for the new TypeScript gRPC project.

- [ ] T001 Read `style/api.md`, `style/golang.md`, `style/large_test.md`, `style/README.md`, and `tools/release/deploy/README.md` before modifying source files
- [ ] T002 Add `@grpc/grpc-js` ^1.14.0 and `@grpc/proto-loader` ^0.8.0 to the `catalog` section of `pnpm-workspace.yaml`
- [ ] T003 [P] Add `experimental/ts/grpc_hello_world/node_modules` to `.bazelignore`
- [ ] T004 Run `bazel run @pnpm -- --dir /mnt/code/dominion install` to resolve new catalog dependencies and update `pnpm-lock.yaml`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Move/create shared JS tooling and release image support required by US1 and US2.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T005 Move `tools/proto/ts_proto_library.bzl` to `tools/dev/js/ts_proto_library.bzl` and update its examples/docstrings to load from `//tools/dev/js:ts_proto_library.bzl`
- [ ] T006 Update `tools/dev/js/BUILD.bazel` to export `ts_proto_library.bzl` and define public target `proto_loader_gen_types` using the root `@grpc/proto-loader` package_json `bin` factory
- [ ] T007 Update `tools/dev/js/ts_proto_library.bzl` so attr `tool` defaults to `Label("//tools/dev/js:proto_loader_gen_types")` while still allowing explicit override
- [ ] T008 Delete `tools/proto/BUILD.bazel` after the rule is exported from `tools/dev/js/BUILD.bazel`
- [ ] T009 Extend `tools/release/defs.bzl:artifact_image` with backward-compatible Node/grpc-js options for `base`, `entrypoint`, `cmd`, and runtime data/runfiles packaging while preserving existing Go service defaults
- [ ] T010 [P] Update `tools/release/deploy/README.md` to document the new Node/grpc-js `artifact_image` options with a concise example

**Checkpoint**: JS tooling is loadable from `tools/dev/js`, normal projects use the global proto-loader executable by default, and release image packaging supports Node/grpc-js without breaking Go services.

---

## Phase 3: User Story 1 - Proto-to-TypeScript gRPC Type Generation (Priority: P1) 🎯 MVP

**Goal**: A developer can generate TypeScript type definitions from a `.proto` file using a single `bazel build` command. The generated types provide compile-time checking for dynamically loaded gRPC services. Static JavaScript or TypeScript protobuf/gRPC stub code is not generated.

**Independent Test**: Add a `.proto` file, run `bazel build //experimental/ts/grpc_hello_world:greeter_types`, and verify that TypeScript type definition files appear in the build output.

### Implementation for User Story 1

- [ ] T011 [P] [US1] Create `experimental/ts/grpc_hello_world/greeter.proto` with package `experimental.ts.grpc_hello_world`, a `Greeter` service, a `SayHello` unary RPC, and `HelloRequest`/`HelloReply` messages per `specs/003-grpc-js-support/contracts/ts_proto_library.md`
- [ ] T012 [P] [US1] Create `experimental/ts/grpc_hello_world/annotations_fixture.proto` that imports `google/api/annotations.proto` and defines a minimal annotated service or method for FR-003 verification
- [ ] T013 [US1] Create or update `experimental/ts/grpc_hello_world/BUILD.bazel` with `proto_library` targets `greeter_proto` and `annotations_fixture_proto`, `ts_proto_library` targets `greeter_types` and `annotations_fixture_types`, load path `//tools/dev/js:ts_proto_library.bzl`, and no project-local `proto_loader.proto_loader_gen_types_binary` target
- [ ] T014 [US1] Verify type generation by running `bazel build //experimental/ts/grpc_hello_world:greeter_types` and confirming `.ts` type files are produced under the Bazel output layout with no static JS/TS protobuf or gRPC stubs
- [ ] T015 [US1] Verify Google API annotation support by running `bazel build //experimental/ts/grpc_hello_world:annotations_fixture_types` and confirming generation succeeds for `google/api/annotations.proto` imports

**Checkpoint**: The proto-to-TypeScript type generation pipeline is fully functional and independently testable.

---

## Phase 4: User Story 2 - TypeScript gRPC Service Implementation (Priority: P2)

**Goal**: A running gRPC service implemented entirely in TypeScript under `experimental/ts/`, using generated types for compile-time safety, dynamically loading `.proto` at runtime, packaged by `artifact_image`, and verified through a testplan that launches the service plus a grpc-gateway HTTP adapter as the system under test.

**Independent Test**: Deploy the example grpc-js service and Go grpc-gateway adapter through the testplan, run a Go large-test HTTP client, call `GET /experimental/ts/grpc-hello-world/say-hello?name=World` on the adapter endpoint, and receive response message `Hello World` within 5 seconds after the adapter forwards to gRPC.

### Implementation for User Story 2

- [ ] T016 [P] [US2] Create `experimental/ts/grpc_hello_world/package.json` with `@dominion/grpc_hello_world` name, `private: true`, and `"catalog:"` protocol for `@grpc/grpc-js`, `@grpc/proto-loader`, `@types/node`, and `typescript` entries following the repository TypeScript package pattern
- [ ] T017 [P] [US2] Create `experimental/ts/grpc_hello_world/tsconfig.json` with CommonJS module, ES2020 target, strict mode, declaration output, no `rootDir`, and `include` covering both `src/**/*.ts` and generated type output imports
- [ ] T018 [P] [US2] Create `experimental/ts/grpc_hello_world/.swcrc` with SWC configuration matching the tsconfig settings and `experimental/ts/hello_world/.swcrc`
- [ ] T019 [US2] Implement `experimental/ts/grpc_hello_world/src/server.ts` with generated type imports, dynamic `@grpc/proto-loader` loading of `greeter.proto`, generation-option-compatible load options, `GreeterHandlers.SayHello`, and gRPC server bind on port 50051
- [ ] T020 [US2] Update `experimental/ts/grpc_hello_world/BUILD.bazel` to add `ts_project(name = "server")`, `js_binary(name = "run")` with `greeter.proto` in data, Go proto/grpc/gateway generation targets for the grpc-gateway adapter, and `artifact_image(name = "cmd_image")` for the Node/grpc-js service using the extended macro
- [ ] T021 [P] [US2] Create `experimental/ts/grpc_hello_world/service/service.yaml` for the TypeScript grpc-js demo server exposing named gRPC port `grpc` and artifact target `:cmd_image`
- [ ] T022 [US2] Create `experimental/ts/grpc_hello_world/testplan/gateway/main.go` implementing a Go grpc-gateway adapter that registers the generated Greeter HTTP handler, dials the internal grpc-js service, and serves the large-test route prefix `/experimental/ts/grpc-hello-world`
- [ ] T023 [US2] Update `experimental/ts/grpc_hello_world/testplan/BUILD.bazel` with grpc-gateway adapter `go_library`, `go_binary`, `artifact_image(name = "cmd_image")`, and `go_largetest(name = "testplan_test")` dependencies on generated Go proto/grpc/gateway code and `//common/gopkg/testtool`
- [ ] T024 [P] [US2] Create `experimental/ts/grpc_hello_world/testplan/gateway/service.yaml` for the Go grpc-gateway adapter, exposing named HTTP port `http` and artifact target `//experimental/ts/grpc_hello_world/testplan:cmd_image`
- [ ] T025 [US2] Create `experimental/ts/grpc_hello_world/testplan/deploy.yaml` with test deploy name using `{{run}}`, service artifact paths for the grpc-js service and grpc-gateway adapter, hostname `apitest.liukexin.com`, and HTTP path prefix `/experimental/ts/grpc-hello-world`
- [ ] T026 [US2] Create `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` defining the testplan suite without deprecated `env`, endpoint `http.public` at `https://apitest.liukexin.com/experimental/ts/grpc-hello-world`, deploy target `//experimental/ts/grpc_hello_world/testplan/deploy.yaml`, and case target `//experimental/ts/grpc_hello_world/testplan:testplan_test`
- [ ] T027 [US2] Create `experimental/ts/grpc_hello_world/testplan/interface_test.go` implementing a Go HTTP acceptance test that resolves `testtool.MustEndpoint("http", "public")`, uses a 5-second client deadline, calls `/say-hello?name=World` relative to the endpoint, and asserts HTTP 200 with response body `Hello World`
- [ ] T028 [US2] Verify service and adapter builds by running `bazel build //experimental/ts/grpc_hello_world:server //experimental/ts/grpc_hello_world:run //experimental/ts/grpc_hello_world:cmd_image //experimental/ts/grpc_hello_world/testplan:cmd_image //experimental/ts/grpc_hello_world/testplan:testplan_test`
- [ ] T029 [US2] Verify testplan config by running `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml` and confirming no `suite.env` validation error exists
- [ ] T030 [US2] Verify testplan acceptance by running the TypeScript demo testplan through the repository testplan workflow and confirming the Go large-test HTTP client receives `Hello World` within 5 seconds through the grpc-gateway adapter

**Checkpoint**: The TypeScript gRPC example service is fully functional with testplan-verified acceptance.

---

## Phase 5: User Story 3 - Repository-Wide Build Consistency (Priority: P3)

**Goal**: The entire repository builds and tests pass after adding gRPC-JS support, with zero regressions and full convention compliance.

**Independent Test**: Run `bazel build //...` and `bazel test //...` at repository root and verify all targets pass with zero failures.

### Implementation for User Story 3

- [ ] T031 [US3] Run `bazel run //:go -- fmt ./experimental/ts/grpc_hello_world/...` to format Go adapter and testplan code
- [ ] T032 [US3] Run `bazel run //:gazelle experimental/ts/grpc_hello_world` to synchronize Go proto/grpc/gateway BUILD metadata without relying on Gazelle for TypeScript targets
- [ ] T033 [US3] Run `bazel run //:gazelle tools/release tools/dev/js` as a repository synchronization/no-diff check for changed tool packages
- [ ] T034 [US3] Run `bazel build //...` and verify all targets build with zero failures across Go, TypeScript, and release tooling
- [ ] T035 [US3] Run `bazel test //...` and verify all tests pass with zero regressions at their prior status
- [ ] T036 [US3] Verify no generated proto or gRPC files exist in the source tree by checking `git status` and confirming `experimental/ts/grpc_hello_world/` contains no generated `.ts`, `.js`, or `node_modules/` artifacts

**Checkpoint**: All user stories are independently functional and the repository is green.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final verification tasks required by the constitution and project governance.

- [ ] T037 [P] Update stale references in `specs/003-grpc-js-support/plan.md`, `specs/003-grpc-js-support/contracts/ts_proto_library.md`, `specs/003-grpc-js-support/research.md`, `specs/003-grpc-js-support/data-model.md`, and `specs/003-grpc-js-support/quickstart.md` so they consistently mention `tools/dev/js`, global `proto_loader_gen_types`, `artifact_image`, and grpc-gateway adapter acceptance
- [ ] T038 [P] Verify all TypeScript/JavaScript dependency versions in `package.json`, `pnpm-workspace.yaml`, and `experimental/ts/grpc_hello_world/package.json` use the centralized catalog model with no inline versions except documented special exceptions
- [ ] T039 [P] Confirm Code Quality Review covers test-code review for `experimental/ts/grpc_hello_world/testplan/interface_test.go` and style review for `experimental/ts/grpc_hello_world/testplan/gateway/main.go`, `experimental/ts/grpc_hello_world/src/server.ts`, and `tools/release/defs.bzl`
- [ ] T040 Run quickstart validation by executing the steps in `specs/003-grpc-js-support/quickstart.md` against the implemented feature and confirming they produce the documented results
- [ ] T041 Run final full-repository verification with `bazel build //...` and `bazel test //...` and record any pre-existing failures separately from this feature

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion and blocks all user stories
- **US1 (Phase 3)**: Depends on Phase 2 completion because the shared rule and global tool target must exist
- **US2 (Phase 4)**: Depends on US1 completion because the service and gateway need generated proto types and Go proto/gateway targets
- **US3 (Phase 5)**: Depends on US2 completion because repository-wide verification requires all code and deployment material
- **Polish (Phase 6)**: Depends on all story phases

### User Story Dependencies

- **User Story 1 (P1)**: Depends on Foundational (Phase 2). No dependency on US2/US3.
- **User Story 2 (P2)**: Depends on US1 for proto/type generation. Independently testable through testplan once US1 is complete.
- **User Story 3 (P3)**: Depends on US2 because it validates the completed repository state.

### Within Each User Story

- Proto definitions before BUILD.bazel type targets
- Global proto-loader tool before `ts_proto_library` consumers
- `artifact_image` Node support before grpc-js service image target
- Server implementation before service image packaging
- grpc-gateway adapter before testplan deploy material
- Testplan YAML before `guitar validate` and testplan execution

### Parallel Opportunities

- **Phase 1**: T002 and T003 can run in parallel
- **Phase 2**: T010 can run in parallel with T005-T009 after the `artifact_image` API is chosen
- **Phase 3**: T011 and T012 can run in parallel
- **Phase 4**: T016, T017, T018 can run in parallel
- **Phase 4**: T021 and T024 can run in parallel after target names are fixed
- **Phase 6**: T037, T038, and T039 can run in parallel

---

## Parallel Example: User Story 2

```bash
# Launch all project config files together:
Task: "Create package.json in experimental/ts/grpc_hello_world/package.json"
Task: "Create tsconfig.json in experimental/ts/grpc_hello_world/tsconfig.json"
Task: "Create .swcrc in experimental/ts/grpc_hello_world/.swcrc"

# Launch deployment material after BUILD target names are clear:
Task: "Create service.yaml in experimental/ts/grpc_hello_world/service/service.yaml"
Task: "Create gateway service.yaml in experimental/ts/grpc_hello_world/testplan/gateway/service.yaml"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (style review + catalog + bazelignore + pnpm install)
2. Complete Phase 2: Foundational (shared JS tooling + release image macro support)
3. Complete Phase 3: User Story 1 (simple proto + Google API annotation fixture → type generation verified)
4. **STOP and VALIDATE**: Run `bazel build //experimental/ts/grpc_hello_world:greeter_types` and `bazel build //experimental/ts/grpc_hello_world:annotations_fixture_types`
5. The core build system capability is now available for any future TypeScript gRPC project without local proto-loader tool boilerplate

### Incremental Delivery

1. Complete Setup + Foundational → JS tooling and release image support ready
2. Add User Story 1 → Test type generation independently → Core build capability delivered
3. Add User Story 2 → Test via grpc-gateway large-test/testplan independently → Example service delivered
4. Add User Story 3 → Full repo verification → Green repository
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

1. Team completes Setup (Phase 1) together
2. One developer owns JS tooling (`tools/dev/js`), another owns `artifact_image` Node support (`tools/release/defs.bzl`)
3. Once Foundational is done, Developer A implements US1 proto fixtures and type generation
4. Once US1 is done, Developer B implements TypeScript service/package config, Developer C implements grpc-gateway/testplan material, Developer A integrates BUILD targets
5. US3 and Polish are verification phases requiring full integration

---

## Notes

- The custom rule (`tools/dev/js/ts_proto_library.bzl`) is the key reusable artifact — it enables future TypeScript projects to generate types from proto files
- The global `//tools/dev/js:proto_loader_gen_types` target removes repeated per-project `proto_loader.proto_loader_gen_types_binary` declarations
- No unit tests are included because the spec requires testplan-based acceptance and explicitly prohibits starting the service process inside a unit test
- Acceptance uses grpc-gateway as the HTTP adapter; do not replace it with a hand-written HTTP-to-gRPC wrapper
- Testplan suites must omit deprecated `env`; current guitar validation rejects it
- The large-test public route should use `apitest.liukexin.com` with a unique path prefix for the adapter service
- All generated files reside in `bazel-out/` — never commit generated proto/gRPC/TypeScript type files
- The `ts_proto_library` generation options (`longs=String`, `enums=String`, `defaults=true`, `oneofs=true`, `keep_case=False`) MUST match the runtime `protoLoader.loadSync()` options in `server.ts`
- Proto files must be available in the server image/runtime data for dynamic loading
- Stop at any checkpoint to validate story independently
- Final validation must prove delivered behavior through the artifact's real surface: testplan HTTP call to grpc-gateway, grpc-gateway gRPC call to grpc-js service
