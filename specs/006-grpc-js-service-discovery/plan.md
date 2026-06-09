# Implementation Plan: gRPC-JS Service Discovery

**Branch**: `006-grpc-js-service-discovery` | **Date**: 2026-06-09 | **Spec**: `specs/006-grpc-js-service-discovery/spec.md`

**Input**: Feature specification from `specs/006-grpc-js-service-discovery/spec.md`

## Summary

Add a Node.js/CommonJS TypeScript package at `common/js/grpc/resolver/` that lets grpc-js clients resolve `dominion:///app/service:port` and `dominion-stateful:///app/service:port?instance=N` targets through the dominion deploy HTTP API. The Go packages `common/gopkg/solver` and `common/gopkg/grpc/solver` are behavioral references for target parsing, deploy endpoint filtering, stateful instance selection, refresh semantics, and last-known-good handling, but the JavaScript implementation must use JS-native shapes: Promise-returning functions, `async`/`await`, plain configuration objects, injected `fetch`/environment providers, timer handles, and descriptive error classes.

## Technical Context

**Language/Version**: TypeScript targeting Node.js, CommonJS module output, `ES2020` target, strict type checking, declaration output.

**Primary Dependencies**: Existing catalog entries `@grpc/grpc-js`, `@types/node`, `typescript`, and `vitest`. Custom grpc-js resolver registration uses the grpc-js `experimental` namespace (`experimental.registerResolver`, `experimental.Resolver`, `experimental.ResolverListener`, `experimental.Endpoint`). No new runtime dependency is required if the HTTP client uses Node's built-in `fetch`; if implementation discovers a Node runtime gap, dependency changes must go through the root `pnpm-workspace.yaml` catalog.

**Storage**: N/A; resolver state is in-memory only (`lastResolvedEndpoints`, timer handle, closed flag).

**Testing**: Vitest through Bazel `js_test`, matching `common/js/*` package patterns. Contract-level tests mock the deploy API and grpc-js listener/channel helper behavior.

**Target Platform**: Node.js services running in the dominion platform; not browser-compatible.

**Project Type**: Shared TypeScript library package for grpc-js client service discovery.

**Performance Goals**: Initial endpoint resolution completes within one deploy API round trip; unchanged refreshes avoid grpc-js state updates; default propagation of endpoint changes occurs within one 30-second refresh interval.

**Constraints**: Use Bazel and Bazel-managed PNPM only; preserve centralized dependency catalog; do not commit generated proto/grpc output; do not copy Go idioms into JS APIs; custom grpc-js resolver listener calls must be asynchronous and publish grpc-js `Endpoint[]` rather than Go-style flat address structs; cleanly release refresh timers on resolver close.

**Scale/Scope**: One package under `common/js/grpc/resolver/` with parser, deploy API client, stateless resolver, stateful resolver, grpc-js resolver plugin, unit tests, and package/Bazel metadata.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Planning read `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, and `style/large_test.md`. Implementation tasks must require executors to re-read these files plus `specs/006-grpc-js-service-discovery/plan.md` before code changes. TypeScript style follows the Google TypeScript style reference linked from `style/README.md` and existing `common/js/*` package patterns.
- **Bazel Integrity**: PASS. Build, test, and PNPM workflows use Bazel: `bazel run @pnpm -- --dir /mnt/code/dominion ...`, `bazel run //:gazelle common/js/grpc/resolver`, `bazel build //...`, and `bazel test //...`.
- **Generated Files & Dependencies**: PASS. `BUILD.bazel` is generated/updated through Gazelle before manual target additions if needed. No generated proto/grpc source is committed. JS dependency versions remain in the root `pnpm-workspace.yaml` catalog and package manifests use `catalog:`.
- **Testing Strategy**: PASS. Implementation should add Vitest tests before or alongside code for target parsing, deploy HTTP contract mapping, endpoint filtering, stateful resolution, grpc-js `experimental.Resolver` registration/listener behavior, async publication, refresh/update/error behavior, and timer cleanup. This is a shared library, not a deployed service, so no large-test plan is required unless tasks later add service code.
- **Behavioral Acceptance**: PASS. Acceptance is through the library surface: direct async resolver calls with mocked HTTP, grpc-js resolver registration/build behavior with a fake listener/channel helper, and a quickstart smoke script using the package API.
- **Review Scope**: PASS. Tasks must include test-code review and TypeScript/JS style review, with explicit checks that the implementation did not import Go-style API shapes.
- **Repository Verification**: PASS. Final verification includes package-level Bazel build/test, then `bazel build //...` and `bazel test //...` unless a pre-existing repository blocker is documented.
- **Testplan Execution**: PASS. No feature testplan or large-test plan exists for this library planning phase. If one is introduced later, execution must use the `testplan` skill.

**Post-Design Re-check**: PASS. Phase 0 and Phase 1 artifacts preserve the same gates: the API contract is library-level TypeScript, deploy API integration is documented without generated code, quickstart validation is behavioral, and the migration rule explicitly rejects Go-style APIs.

## Project Structure

### Documentation (this feature)

```text
specs/006-grpc-js-service-discovery/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── public-api.md
└── tasks.md              # Created later by /speckit.tasks
```

### Source Code (repository root)

```text
common/js/grpc/resolver/
├── BUILD.bazel
├── package.json
├── run_vitest.mjs
├── tsconfig.json
└── src/
    ├── index.ts
    ├── target.ts
    ├── errors.ts
    ├── environment.ts
    ├── deploy-client.ts
    ├── endpoint-filter.ts
    ├── resolver.ts
    ├── stateful.ts
    ├── grpc-js-resolver.ts
    └── *.test.ts
```

**Structure Decision**: Use the existing `common/js/*` package layout: package metadata at package root, `src/` for TypeScript sources/tests, Bazel `ts_project`/`js_library`/`js_test` targets, and `run_vitest.mjs` to launch Vitest programmatically. The package belongs under `common/js/grpc/resolver/` because it is a shared grpc-js enhancement and the feature spec identifies that placeholder location.

## Complexity Tracking

No constitution violations. No special dependency or build exceptions are planned.
