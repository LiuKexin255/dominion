---

description: "Task list for gRPC-JS Build Support & Example Service"
---

# Tasks: gRPC-JS Build Support & Example Service

**Input**: Design documents from `/specs/003-grpc-js-support/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/ts_proto_library.md

**Tests**: This feature includes testplan-based acceptance testing (FR-009). No unit tests start the service process in-process for acceptance. Because deploy does not expose raw gRPC endpoints, the TypeScript grpc-js service is verified through the repository testplan workflow by deploying a Go HTTP wrapper in the suite; the Go `go_largetest` client resolves the wrapper HTTP endpoint with `common/gopkg/testtool`, calls the HTTP endpoint, and the wrapper forwards to the internal gRPC service.

**Constitution**: Generated tasks MUST satisfy `.specify/memory/constitution.md`. Include repository-specific verification tasks for style-guide review, formatting, Gazelle/dependency synchronization, pnpm catalog updates for TypeScript/JavaScript dependency versions, large-test/testplan execution for service code, and full-repository Bazel build/test validation.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Monorepo with Bazel**: Source files at repository root under namespaced directories
- **Custom rules**: `tools/proto/` for custom Bazel rule definitions
- **Example service**: `experimental/ts/grpc_hello_world/` following `experimental/ts/hello_world/` conventions except where generated TypeScript sources require a widened tsconfig
- **Service deploy material**: `experimental/ts/grpc_hello_world/service/service.yaml`, `experimental/ts/grpc_hello_world/testplan/wrapper/service.yaml`, and `experimental/ts/grpc_hello_world/testplan/deploy.yaml`, keeping wrapper material inside the testplan suite and mirroring `experimental/golang/grpc_hello_world/` while adding the HTTP wrapper required by deploy
- **Testplan**: `experimental/ts/grpc_hello_world/testplan/` with Go `go_largetest` HTTP acceptance code; `experimental/ts/grpc_hello_world/testplan/wrapper/` contains the suite-owned Go HTTP-to-gRPC bridge

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Add npm dependencies, read required style guidance, and configure repository ignore rules for the new TypeScript gRPC project.

- [ ] T001 Read `style/api.md`, `style/golang.md`, `style/large_test.md`, and `style/README.md` before modifying source files
- [ ] T002 Add `@grpc/grpc-js` ^1.14.0 and `@grpc/proto-loader` ^0.8.0 to the `catalog` section of `pnpm-workspace.yaml`
- [ ] T003 [P] Add `experimental/ts/grpc_hello_world/node_modules` to `.bazelignore`
- [ ] T004 Run `bazel run @pnpm -- --dir /mnt/code/dominion install` to resolve new catalog dependencies and update the lockfile

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Create the custom `ts_proto_library` Bazel rule that is required by both US1 (type generation verification) and US2 (server compilation).

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T005 Create the `ts_proto_library` custom Bazel rule in `tools/proto/ts_proto_library.bzl` — the rule accepts a `proto_library` target via `ProtoInfo`, uses individual attributes `grpc_lib`, `longs`, `enums`, `defaults`, `oneofs`, and `keep_case`, stages transitive proto sources in a TreeArtifact matching import paths, invokes `proto-loader-gen-types` via a Bazel executable action with `--outDir` set to the declared generated output directory, and declares `.ts` output files under a `generated/` layout consumable by `ts_project`; refer to `specs/003-grpc-js-support/contracts/ts_proto_library.md`
- [ ] T006 [P] Create `tools/proto/BUILD.bazel` with `exports_files` for `ts_proto_library.bzl` so the rule is loadable from other packages

**Checkpoint**: Custom rule exists and is loadable — user story implementation can now begin.

---

## Phase 3: User Story 1 - Proto-to-TypeScript gRPC Type Generation (Priority: P1) 🎯 MVP

**Goal**: A developer can generate TypeScript type definitions from a `.proto` file using a single `bazel build` command. The generated types provide compile-time checking for dynamically loaded gRPC services. Static JavaScript or TypeScript protobuf/gRPC stub code is not generated.

**Independent Test**: Add a `.proto` file, run `bazel build //experimental/ts/grpc_hello_world:greeter_types`, and verify that TypeScript type definition files appear in the build output.

### Implementation for User Story 1

- [ ] T007 [P] [US1] Create `experimental/ts/grpc_hello_world/greeter.proto` with package `experimental.ts.grpc_hello_world`, a `Greeter` service, a `SayHello` unary RPC, and `HelloRequest`/`HelloReply` messages per `specs/003-grpc-js-support/contracts/ts_proto_library.md`
- [ ] T008 [P] [US1] Create `experimental/ts/grpc_hello_world/annotations_fixture.proto` that imports `google/api/annotations.proto` and defines a minimal service or method option using Google API annotations for FR-003 verification
- [ ] T009 [US1] Create `experimental/ts/grpc_hello_world/BUILD.bazel` with `proto_library` targets `greeter_proto` and `annotations_fixture_proto`, `ts_proto_library` targets `greeter_types` and `annotations_fixture_types`, and `@googleapis//google/api:annotations_proto` dependency for the annotation fixture (depends on T005, T006, T007, T008)
- [ ] T010 [US1] Verify type generation: run `bazel build //experimental/ts/grpc_hello_world:greeter_types` and confirm `.ts` type files are produced under the Bazel output `generated/` layout with no static JS/TS protobuf or gRPC stubs
- [ ] T011 [US1] Verify Google API annotation support: run `bazel build //experimental/ts/grpc_hello_world:annotations_fixture_types` and confirm generation succeeds for `google/api/annotations.proto` imports

**Checkpoint**: At this point, the proto-to-TypeScript type generation pipeline is fully functional and independently testable. Both the simple greeter proto and Google API annotation fixture build.

---

## Phase 4: User Story 2 - TypeScript gRPC Service Implementation (Priority: P2)

**Goal**: A running gRPC service implemented entirely in TypeScript under `experimental/ts/`, using generated types for compile-time safety, dynamically loading `.proto` at runtime, and verified through a testplan that launches the service plus a Go HTTP wrapper as the system under test.

**Independent Test**: Deploy the example grpc-js service and Go HTTP wrapper through the testplan, run a Go large-test HTTP client, call `GET /say-hello?name=World` on the wrapper endpoint, and receive response message "Hello World" within 5 seconds after the wrapper forwards to gRPC.

### Implementation for User Story 2

- [ ] T012 [P] [US2] Create `experimental/ts/grpc_hello_world/package.json` with `@dominion/grpc_hello_world` name, `private: true`, and `"catalog:"` protocol for `@grpc/grpc-js`, `@grpc/proto-loader` dependencies and `typescript` devDependency, following the pattern in `experimental/ts/hello_world/package.json`
- [ ] T013 [P] [US2] Create `experimental/ts/grpc_hello_world/tsconfig.json` with CommonJS module, ES2020 target, strict mode, declaration output, no `rootDir`, and `include` covering both `src/**/*.ts` and `generated/**/*.ts` because generated `.ts` files come from Bazel outputs rather than `src/`
- [ ] T014 [P] [US2] Create `experimental/ts/grpc_hello_world/.swcrc` with SWC configuration matching the tsconfig settings, following `experimental/ts/hello_world/.swcrc`
- [ ] T015 [US2] Implement `experimental/ts/grpc_hello_world/src/server.ts` — import generated types from `../generated/ProtoGrpcType` and `../generated/experimental/ts/grpc_hello_world/Greeter`, dynamically load `greeter.proto` via `@grpc/proto-loader` with options matching `ts_proto_library` defaults (`longs=String`, `enums=String`, `defaults=true`, `oneofs=true`, omitted `keepCase` because `keep_case=False`), implement `GreeterHandlers` with `SayHello` returning `Hello {name}`, and start gRPC server (depends on T007, T010, T012, T013, T014)
- [ ] T016 [US2] Update `experimental/ts/grpc_hello_world/BUILD.bazel` to add `ts_project` target `server` with SWC transpiler referencing `:greeter_types` and `src/server.ts` as srcs, `js_binary` target `run` with `greeter.proto` in data and `src/server.js` as entry_point, and deployment artifact target(s) needed by `experimental/ts/grpc_hello_world/service/service.yaml` (depends on T009, T015)
- [ ] T017 [P] [US2] Create `experimental/ts/grpc_hello_world/service/service.yaml` for the TypeScript grpc-js demo server, following `experimental/golang/grpc_hello_world/service/service.yaml` and exposing an internal named gRPC port for the wrapper to call
- [ ] T018 [P] [US2] Create `experimental/ts/grpc_hello_world/testplan/wrapper/main.go` implementing a Go HTTP server with `GET /say-hello?name=...`; it dials the internal grpc-js service, sends `SayHello`, uses a 5-second upstream context deadline, returns `Hello {name}` as HTTP response, and returns a non-2xx response if the upstream gRPC call fails
- [ ] T019 [US2] Create `experimental/ts/grpc_hello_world/testplan/BUILD.bazel` with Go wrapper binary/image targets plus Go proto/gRPC client dependencies for `greeter.proto` and grpc-go deps needed by `wrapper/main.go` (depends on T009, T018)
- [ ] T020 [P] [US2] Create `experimental/ts/grpc_hello_world/testplan/wrapper/service.yaml` for the Go HTTP wrapper, exposing named HTTP port `http` as the deploy/testplan public entrypoint and configuring the upstream grpc-js service address for the wrapper (depends on T017, T018)
- [ ] T021 [US2] Create `experimental/ts/grpc_hello_world/testplan/deploy.yaml` with test deploy name using `{{run}}` and service artifact paths for both `//experimental/ts/grpc_hello_world/service:service.yaml` and `//experimental/ts/grpc_hello_world/testplan/wrapper:service.yaml`, following the repository deploy material style (depends on T017, T020)
- [ ] T022 [US2] Create `experimental/ts/grpc_hello_world/testplan/BUILD.bazel` with `go_largetest(name = "testplan_test")` and dependency on `//common/gopkg/testtool`; the test case is HTTP-only because gRPC client code lives in the deployed wrapper (depends on T021)
- [ ] T023 [US2] Create `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` defining the testplan suite, HTTP endpoint `public`, deploy target `//experimental/ts/grpc_hello_world/testplan:deploy.yaml`, and case target `//experimental/ts/grpc_hello_world/testplan:testplan_test` (depends on T021, T022)
- [ ] T024 [US2] Create `experimental/ts/grpc_hello_world/testplan/interface_test.go` implementing a Go HTTP acceptance test that resolves `testtool.MustEndpoint("http", "public")`, uses a 5-second client deadline, calls `/say-hello?name=World`, and asserts HTTP 200 with response body `Hello World` (depends on T022, T023)
- [ ] T025 [US2] Verify service and wrapper builds: run `bazel build //experimental/ts/grpc_hello_world:server`, `bazel build //experimental/ts/grpc_hello_world:run`, and `bazel build //experimental/ts/grpc_hello_world/testplan/wrapper:wrapper` or the actual wrapper image target
- [ ] T026 [US2] Verify testplan acceptance: run the TypeScript demo testplan via the repository testplan workflow (guitar) and confirm the Go large-test HTTP client receives `Hello World` within 5 seconds through the wrapper — do NOT verify by starting the service inside a unit test process

**Checkpoint**: At this point, the TypeScript gRPC example service is fully functional with testplan-verified acceptance. User Stories 1 AND 2 both work independently.

---

## Phase 5: User Story 3 - Repository-Wide Build Consistency (Priority: P3)

**Goal**: The entire repository builds and tests pass after adding gRPC-JS support, with zero regressions and full convention compliance.

**Independent Test**: Run `bazel build //...` and `bazel test //...` at repository root and verify all targets pass with zero failures.

### Implementation for User Story 3

- [ ] T027 [US3] Run `bazel run //:gazelle` as a repository synchronization/no-diff check for non-TypeScript generated BUILD files; TypeScript BUILD targets remain manual per spec assumptions
- [ ] T028 [US3] Run `bazel build //...` and verify all targets build with zero failures — Go, TypeScript, and other targets all succeed
- [ ] T029 [US3] Run `bazel test //...` and verify all tests pass with zero regressions — existing tests maintain their prior status and the new testplan target passes
- [ ] T030 [US3] Verify no generated proto or gRPC files exist in the source tree by running `git status` and confirming `experimental/ts/grpc_hello_world/` contains only source files (no generated `.ts` from proto, no `.js` stubs, no `node_modules/`)

**Checkpoint**: All user stories are independently functional. Repository is green.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final verification tasks required by the constitution and project governance.

- [ ] T031 [P] Run formatting on all changed TypeScript and Go code via repository Bazel wrappers
- [ ] T032 [P] Verify all TypeScript/JavaScript dependency versions in `experimental/ts/grpc_hello_world/package.json` use the `"catalog:"` protocol from `pnpm-workspace.yaml` — no inline versions except documented special exceptions
- [ ] T033 [P] Confirm Code Quality Review covers test-code review (Go large-test case matches FR-009 and SC-002 acceptance goals) and style review (changed code follows `style/` guides and `experimental/ts/hello_world` patterns where compatible with generated sources)
- [ ] T034 Run quickstart.md validation: execute the steps in `specs/003-grpc-js-support/quickstart.md` against the implemented feature to confirm they produce the documented results
- [ ] T035 Run final full-repository verification: `bazel build //...` and `bazel test //...` pass with zero failures — this is the definitive green state check

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion (pnpm catalog entries must exist) — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2 completion (custom rule must exist)
- **US2 (Phase 4)**: Depends on Phase 3 completion (generated types must exist for server compilation)
- **US3 (Phase 5)**: Depends on Phase 4 completion (all code must be in place for repo-wide verification)
- **Polish (Phase 6)**: Depends on Phase 5 completion (all stories verified before final polish)

### User Story Dependencies

- **User Story 1 (P1)**: Depends on Foundational (Phase 2). No dependencies on other stories.
- **User Story 2 (P2)**: Depends on US1 (needs generated types and proto). Independently testable once US1 is complete.
- **User Story 3 (P3)**: Depends on US2 (needs all code in place). Independently testable once US2 is complete.

### Within Each User Story

- Proto definitions before BUILD.bazel targets
- BUILD.bazel targets before build verification
- Project config files (package.json, tsconfig, .swcrc) before server implementation
- Server implementation before BUILD.bazel ts_project/js_binary targets
- Build targets before service/testplan deployment material
- Wrapper service before testplan deployment material
- Testplan BUILD/YAML before testplan Go acceptance test
- Testplan before testplan verification

### Parallel Opportunities

- **Phase 1**: T002 and T003 can run in parallel (different files)
- **Phase 2**: T005 and T006 can run in parallel (different files: .bzl and BUILD.bazel)
- **Phase 3**: T007 and T008 can run in parallel (different proto files)
- **Phase 4**: T012, T013, T014 can run in parallel (package.json, tsconfig.json, .swcrc are independent files)
- **Phase 4**: T017 and T018 can run in parallel (grpc-js service.yaml and wrapper main.go are independent files)
- **Phase 6**: T031, T032, T033 can run in parallel (independent verification tasks)

---

## Parallel Example: User Story 2

```bash
# Launch all project config files together:
Task: "Create package.json in experimental/ts/grpc_hello_world/package.json"
Task: "Create tsconfig.json in experimental/ts/grpc_hello_world/tsconfig.json"
Task: "Create .swcrc in experimental/ts/grpc_hello_world/.swcrc"

# Launch deployment materials in parallel after BUILD targets are clear:
Task: "Create service.yaml in experimental/ts/grpc_hello_world/service/service.yaml"
Task: "Create wrapper main.go in experimental/ts/grpc_hello_world/testplan/wrapper/main.go"
Task: "Create wrapper service.yaml in experimental/ts/grpc_hello_world/testplan/wrapper/service.yaml"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (style review + catalog + bazelignore + pnpm install)
2. Complete Phase 2: Foundational (custom ts_proto_library rule)
3. Complete Phase 3: User Story 1 (simple proto + Google API annotation fixture → type generation verified)
4. **STOP and VALIDATE**: Run `bazel build //experimental/ts/grpc_hello_world:greeter_types` and `bazel build //experimental/ts/grpc_hello_world:annotations_fixture_types`
5. The core build system capability is now available for any future TypeScript gRPC project

### Incremental Delivery

1. Complete Setup + Foundational → Rule infrastructure ready
2. Add User Story 1 → Test type generation independently → Core build capability delivered (MVP!)
3. Add User Story 2 → Test via Go HTTP wrapper large-test/testplan independently → Example service delivered
4. Add User Story 3 → Full repo verification → Green repository
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup (Phase 1) together
2. Team completes Foundational (Phase 2) together — single rule file, needs focused effort
3. Once Foundational is done:
   - Developer A: User Story 1 proto fixtures and type generation
4. Once US1 is done:
   - Developer B: Project config (T012-T014, parallel)
   - Developer C: Deployment materials and suite-owned wrapper service (T017-T021)
   - Developer A: Server implementation + testplan Go HTTP client (T015-T016, T022-T024)
5. US3 and Polish are verification phases requiring full integration

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- The custom rule (`tools/proto/ts_proto_library.bzl`) is the key reusable artifact — it enables any future TypeScript project to generate types from proto files
- No unit tests are included because the spec (FR-009) requires testplan-based acceptance and explicitly prohibits starting the service process inside a unit test
- Acceptance test code is Go (`go_largetest`) to reuse repository testplan infrastructure; the HTTP wrapper is suite-owned testplan material and is Go because deploy exposes HTTP, and the grpc-js service implementation remains TypeScript
- All generated files reside in `bazel-out/` — never commit generated proto/gRPC/TypeScript type files
- The `ts_proto_library` generation options (`longs=String`, `enums=String`, `defaults=true`, `oneofs=true`, `keep_case=False`) MUST match the runtime `protoLoader.loadSync()` options in `server.ts` (omit `keepCase` unless `keep_case=True`)
- Proto files must be available in the server binary's runfiles at runtime for dynamic loading
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Final validation must prove delivered behavior through the artifact's real surface (testplan HTTP call to wrapper, wrapper gRPC call to grpc-js service)
- Do not mark testplan tasks skipped unless the blocker and residual risk are documented
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
