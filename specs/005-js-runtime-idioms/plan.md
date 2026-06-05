# Implementation Plan: JavaScript Runtime Packaging and Idiomatic Common APIs

**Branch**: `005-js-runtime-idioms` | **Date**: 2026-06-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/005-js-runtime-idioms/spec.md`

## Summary

Refine the newly introduced `common/js` packages so they behave like normal Node.js packages at runtime and expose JavaScript-idiomatic logging APIs. The implementation will remove the standalone `@dominion/common-js-logs-event` package, migrate the useful error/field helpers into `@dominion/common-js-logs`, fix OpenTelemetry log records to preserve structured attributes, add package entry metadata, and redesign `artifact_pkg_js` runtime dependency packaging so services declare direct runtime dependencies while the rule packages the transitive runtime closure.

## Technical Context

**Language/Version**: TypeScript 6.x, ES2020 target, CommonJS output preserved for existing Node.js 24.14.0 service startup.

**Primary Dependencies**:
- Existing `@aspect/rules_js`, `@aspect/rules_ts`, `@aspect/rules_swc` toolchain.
- Existing OpenTelemetry JS packages in the root catalog; no OTLP gRPC exporters.
- Existing `@grpc/grpc-js` and `@grpc/proto-loader` for TypeScript gRPC service acceptance.
- No new bundler dependency in this plan; bundling is rejected for this feature due OTel instrumentation load-order and runtime proto-loading risk.

**Storage**: N/A.

**Testing**: Vitest-under-Bazel for common JS packages; Bazel package inspection/startup smoke test for the service artifact; existing `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` when deployment infrastructure is reachable; final `bazel build //...` and `bazel test //...`.

**Target Platform**: Linux Node.js 24.14.0 packaged through repository OCI artifact rules.

**Project Type**: Shared library and build/release infrastructure update.

**Performance Goals**: Runtime dependency packaging adds no service startup overhead beyond normal Node module resolution. Logging calls preserve existing low overhead; OTel log export remains asynchronous/batched.

**Constraints**:
- Use Bazel-managed commands and repository wrappers.
- Keep dependency versions in root `pnpm-workspace.yaml` catalog.
- Do not use OTLP gRPC exporters because they load `@grpc/grpc-js` before instrumentation can patch it.
- Preserve gRPC instrumentation registration-before-load behavior.
- Do not require service BUILD files to enumerate indirect runtime dependencies.
- Keep CommonJS compatibility unless a later feature explicitly migrates module output.

**Scale/Scope**:
- Remove one workspace package: `common/js/logs/event`.
- Update three retained runtime packages: `common/js/logs`, `common/js/otel`, `common/js/grpc/otel`.
- Update `tools/release/defs.bzl` JS packaging behavior.
- Update `experimental/ts/grpc_hello_world` imports and packaging declarations.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Implementers must read `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, and the Google TypeScript Style Guide referenced by `style/README.md` before code changes.
- **Bazel Integrity**: PASS. All builds/tests use Bazel. PNPM changes, if any, use `bazel run @pnpm -- --dir /mnt/code/dominion`. TS BUILD files and release rules are manual where Gazelle cannot express the targets.
- **Generated Files & Dependencies**: PASS. No generated proto/grpc source is committed. Package manifests use catalog/workspace references only. Any dependency graph/package metadata changes must synchronize lock and Bazel state through documented commands.
- **Testing Strategy**: PASS. Unit tests must be updated before or alongside implementation to capture package removal, logging attribute behavior, and artifact runtime dependency packaging. Service change retains large-test acceptance via existing guitar testplan when infrastructure is available.
- **Behavioral Acceptance**: PASS. Acceptance validates the real artifact by inspecting/starting the packaged TypeScript service, not only compiling source.
- **Review Scope**: PASS. Plan includes code quality review focused on JS idioms, test-code review, and packaging correctness.
- **Repository Verification**: PASS. Final verification includes `bazel build //...` and `bazel test //...`.
- **Testplan Execution**: PASS. The existing grpc hello world testplan remains required when deployment infrastructure is reachable; skipped execution must record external blocker and residual risk.

## Project Structure

### Documentation (this feature)

```text
specs/005-js-runtime-idioms/
├── plan.md
├── spec.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── artifact-pkg-js-runtime-deps.md
│   └── logs-api.md
└── checklists/
    └── requirements.md
```

### Source Code (repository root)

```text
common/js/
├── logs/
│   ├── BUILD.bazel
│   ├── package.json              # add main/types/exports; remove dependency on logs-event
│   ├── src/
│   │   ├── index.ts              # primary public logging API and migrated helpers
│   │   ├── logger.ts             # object attributes first; no Event dependency
│   │   ├── reporter.ts           # OTel attributes preserved
│   │   └── context.ts
│   └── event/                    # remove package and references
├── grpc/otel/
│   ├── package.json              # add main/types/exports
│   └── src/index.ts
└── otel/
    ├── package.json              # add main/types/exports
    └── src/index.ts              # preserve HTTP exporters and load-order-safe init

tools/release/
└── defs.bzl                      # extend JS artifact packaging for direct runtime deps + transitive closure

experimental/ts/grpc_hello_world/
├── BUILD.bazel                   # use direct runtime deps; validate package artifact
├── package.json                  # remove obsolete workspace dependency if present
└── src/
    ├── bootstrap.ts
    └── server.ts
```

**Structure Decision**: This is a focused repository-infrastructure and common-library refinement. Existing package locations remain, except the obsolete `common/js/logs/event` package is removed and its retained helpers move into `common/js/logs`.

## Complexity Tracking

No constitution violations detected. The main complexity is designing `artifact_pkg_js` runtime packaging so it remains Bazel-first while avoiding a hand-maintained list of indirect runtime dependencies.

---

## Implementation Phases

### Phase A: Define runtime package metadata contract

**Goal**: Make each retained shared JS package resolvable as a normal Node package.

**Changes**:
- Add `main`, `types`, and `exports` fields to retained `common/js` package manifests.
- Ensure emitted paths match the actual `ts_project` output layout.
- Remove `@dominion/common-js-logs-event` from workspace consumers and package manifests.

**Tests/Verification**:
- Minimal Node driver can `require()` each retained package from the packaged layout.
- TypeScript typecheck still resolves declarations.

### Phase B: Remove `logs/event` and migrate retained helpers

**Goal**: Replace Go-style event structs with JS object-first logging.

**Changes**:
- Delete `common/js/logs/event` package and tests.
- Remove `Event` and variadic event arguments from logger methods unless a compatibility shim is explicitly documented as temporary.
- Add small helpers in `common/js/logs` only where they add real value, e.g. optional error attribute helper returning plain attributes or undefined.
- Rename Go-style `newOTelReporter` to `createOTelReporter`; decide whether to retain a deprecated alias only if tests show migration risk.
- Change `LogLevel` to a string union or const-object shape.

**Tests/Verification**:
- Logging calls using plain attributes pass.
- No source, test, or manifest imports `@dominion/common-js-logs-event`.
- Existing service logging compiles with the new API.

### Phase C: Fix structured OTel log records

**Goal**: Preserve queryable fields in OTel logs.

**Changes**:
- `OTelReporter` emits message text as log body and caller fields as log attributes.
- Severity text and severity number remain consistent.
- Error values are mapped to supported log attribute values or documented string fields.

**Tests/Verification**:
- Unit test asserts OTel emit receives `body`, `severityNumber`, `severityText`, and `attributes` separately.
- Console output remains JSON structured.

### Phase D: Redesign JS artifact runtime dependency packaging

**Goal**: Let service BUILD files declare direct runtime deps only, while artifact packaging includes the runtime closure.

**Changes**:
- Extend `artifact_pkg_js` with a runtime dependency input model that accepts direct workspace runtime packages and npm link targets.
- Introduce a small provider or rule wrapper for workspace JS runtime packages that exposes package name, package metadata, compiled runtime files, direct runtime deps, and transitive runtime files.
- Preserve third-party package runtime layout sufficiently for Node resolution; avoid flattening that breaks peer dependency or pnpm semantics.
- Detect missing package metadata or unresolved runtime deps during build/package validation.

**Tests/Verification**:
- Unit or analysis tests for packaging rule behavior where practical.
- Service package tar contains retained common JS packages, their metadata, and required third-party runtime packages.
- A local Node startup/import smoke test runs against packaged contents and fails before deployment if modules are missing.

### Phase E: Update TypeScript gRPC service acceptance

**Goal**: Prove the refined common packages and packaging model work through the existing service surface.

**Changes**:
- Update imports to use main logging package helpers only.
- Update `BUILD.bazel` runtime dependency declarations to direct-only shape.
- Preserve dynamic import/load-order pattern for gRPC instrumentation.

**Tests/Verification**:
- `bazel build //experimental/ts/grpc_hello_world:cmd_image` passes.
- Local packaged artifact startup/import smoke test passes.
- `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml` passes.
- `guitar run ...` executes when deployment infrastructure is reachable; otherwise blocker is recorded.

### Phase F: Final verification and review

**Goal**: Ensure the migration is complete and no Go-style leftovers remain unintentionally.

**Verification**:
- `rg "@dominion/common-js-logs-event|eventString|eventInt|eventBool|eventAny|newOTelReporter"` returns no active source usages except documented compatibility aliases if intentionally retained.
- `bazel build //common/js/... //experimental/ts/grpc_hello_world:cmd_image` passes.
- `bazel test //common/js/...` passes.
- `bazel build //...` and `bazel test //...` pass.
- Code quality review checks JS naming, package metadata, OTel attributes, and artifact dependency closure.

## Post-Design Constitution Check

- **Authority & Style**: PASS. Plan and artifacts identify required style/constitution reads.
- **Bazel Integrity**: PASS. All validation uses Bazel and repository wrappers.
- **Generated Files & Dependencies**: PASS. No generated proto source planned; package dependencies remain catalog/workspace managed.
- **Testing Strategy**: PASS. Unit, package artifact, service image, and testplan validations are defined.
- **Behavioral Acceptance**: PASS. The service artifact is started/imported through its runtime surface to prove module resolution.
- **Review Scope**: PASS. JS idiom and packaging reviews are explicit final checks.
- **Repository Verification**: PASS. Full build and test remain final gates.
- **Testplan Execution**: PASS with infrastructure caveat documented.
