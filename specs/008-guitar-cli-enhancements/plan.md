# Implementation Plan: Guitar CLI Enhancements

**Branch**: `008-guitar-cli-enhancements` | **Date**: 2026-06-11 | **Spec**: `specs/008-guitar-cli-enhancements/spec.md`

**Input**: Feature specification from `specs/008-guitar-cli-enhancements/spec.md`

## Summary

Enhance the Go-based `guitar` large-test orchestration CLI so multi-suite runs are readable, debuggable, and selectively runnable. The implementation modifies the existing `tools/test/guitar` packages in place: add `run --suite <name>` parsing and filtering, extend suite YAML parsing/validation with optional per-suite `timeout` seconds, and refactor run output through a small terminal-aware reporter that prints suite headers, indented deploy/test/cleanup steps, and status-colored success/failure summaries only when stdout/stderr are terminals.

## Technical Context

**Language/Version**: Go under repository `rules_go`/Bazel wrappers; current guitar CLI is `tools/test/guitar/cmd` with packages under `tools/test/guitar/pkg/*`.

**Primary Dependencies**: Existing `github.com/spf13/pflag` for flags, `github.com/goccy/go-yaml` for YAML parsing, standard-library `context`, `time`, `io`, `os`, and command execution. Add no third-party color library; implement ANSI coloring behind an internal reporter to avoid dependency/catalog churn.

**Storage**: N/A. The feature changes CLI runtime behavior and YAML in-memory config only; no persistent state is introduced.

**Testing**: Go unit tests through Bazel `go_unittest` targets for `cmd`, `pkg/config`, `pkg/validate`, and `pkg/run`; validation of `tools/test/guitar/example.guitar.yaml`; manual CLI surface checks through `bazel run //:guitar_install` then `guitar validate`/`guitar run` with stubbed or test fixtures where practical.

**Target Platform**: Linux/macOS terminal and non-terminal CI/log redirection environments running Bazel-managed `guitar`.

**Project Type**: Go CLI tool within a Bazel monorepo.

**Performance Goals**: Suite filtering avoids all deploy/test/cleanup calls for non-selected suites. Reporter formatting adds negligible overhead relative to deploy and Bazel test execution. Per-suite timeouts terminate `bazel test` promptly at the configured deadline.

**Constraints**: Use Bazel for all build/test commands; update Go code with `bazel run //:go -- fmt`; update Gazelle-generated `BUILD.bazel` only through `bazel run //:gazelle tools/test/guitar` if package files change; keep existing no-`env` suite rule; do not change deploy semantics; colors must be disabled for non-TTY output and unsupported/disabled terminals; suite `timeout` is seconds in YAML, accepts `0` as no test-level timeout, rejects negatives/non-numbers via parse/validate; global `--timeout` remains overall run context and suite test timeout fallback.

**Scale/Scope**: Modify one CLI command package, three guitar library packages, docs/example YAML, and their tests. No service code, database, proto, TypeScript, or production deployment configuration changes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Planning read `.specify/memory/constitution.md`, root `README.md`, `tools/test/guitar/README.md`, `style/README.md`, `style/golang.md`, `style/large_test.md`, `style/api.md`, and current guitar source. Implementation tasks must require executors to re-read these files plus affected package files before code changes.
- **Bazel Integrity**: PASS. Build/test/format flows use `bazel run //:go -- fmt ...`, targeted `bazel test //tools/test/guitar/...`, `bazel build //tools/test/guitar/...`, `bazel run //:gazelle tools/test/guitar` only if BUILD metadata changes, and final `bazel build //...` / `bazel test //...` unless a pre-existing blocker is documented.
- **Generated Files & Dependencies**: PASS. No generated source is committed. No JS/TS/Python dependencies change. No new Go dependency is planned, so Bazel module state should remain unchanged unless implementation proves otherwise.
- **Testing Strategy**: PASS. Unit tests are practical and must be written/updated before or alongside implementation for flag parsing, config timeout parsing, validation failures, suite filtering, suite timeout command contexts, reporter color/no-color behavior, and error summaries. No service code changes, so no new large-test service acceptance is required.
- **Behavioral Acceptance**: PASS. Acceptance validates the real CLI surface: `guitar validate`, `guitar run`, `guitar run --suite`, terminal-like output capture, and redirected/non-TTY output capture.
- **Review Scope**: PASS. Code Quality Review must include Go style review and test-code review for CLI output assertions, timeout behavior, and suite filtering.
- **Repository Verification**: PASS. Final validation includes targeted guitar build/tests and whole-repository `bazel build //...` and `bazel test //...` or documented pre-existing blockers.
- **Testplan Execution**: PASS. This feature changes the `guitar` testplan runner itself, not a service testplan. It must validate a representative YAML testplan and execute safe CLI-level tests; repository large-test execution is not required unless implementation changes service testplans.
- **Test Impact Assessment**: PASS. Affected tests are listed below and must be updated: `tools/test/guitar/cmd/guitar_test.go` for `--suite` and usage; `tools/test/guitar/pkg/config/config_test.go` plus `testdata/valid.yaml` for suite timeout parsing; `tools/test/guitar/pkg/validate/validate_test.go` for timeout validation; `tools/test/guitar/pkg/run/run_test.go` for filtering, timeout fallback/override, output grouping/colors/no-colors, and cleanup/error formatting. Existing game/system testplan YAML should remain compatible and optionally gain suite timeout examples only when needed.
- **Change Classification**: PASS. Planned changes are documented as refactorings/modifications below.

**Post-Design Re-check**: PASS. Phase 0 and Phase 1 artifacts keep the implementation limited to the Go CLI, preserve Bazel-first workflows, require observable CLI acceptance, and explicitly cover test impact and refactoring scope.

## Project Structure

### Documentation (this feature)

```text
specs/008-guitar-cli-enhancements/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md
│   ├── output.md
│   └── yaml.md
└── tasks.md              # Created later by /speckit.tasks
```

### Source Code (repository root)

```text
tools/test/guitar/
├── README.md                         # modify: document --suite, timeout, output behavior
├── example.guitar.yaml               # modify: add suite timeout example and fix cases key if needed
├── cmd/
│   ├── guitar.go                     # modify/refactor: parse --suite, usage text, pass run options
│   └── guitar_test.go                # modify: flag/usage tests
└── pkg/
    ├── config/
    │   ├── config.go                 # modify: Suite.Timeout optional seconds field
    │   ├── config_test.go            # modify: parse timeout coverage
    │   └── testdata/valid.yaml       # modify: representative timeout fixture
    ├── validate/
    │   ├── validate.go               # modify: timeout non-negative validation
    │   └── validate_test.go          # modify: timeout validation coverage
    └── run/
        ├── run.go                    # modify/refactor: suite filter, per-suite test timeout, reporter calls
        ├── reporter.go               # new: terminal-aware suite/step/status output abstraction
        └── run_test.go               # modify: run behavior/output tests
```

**Structure Decision**: Keep the existing single Go CLI structure under `tools/test/guitar` and add at most one internal `pkg/run/reporter.go` file. The feature is a behavior enhancement to an existing tool, so creating a separate output package or replacing command parsing would be unnecessary scope expansion.

## Complexity Tracking

No constitution violations.

## Change Classification and Refactoring Scope

| Path | Classification | Refactoring goal | Invariants preserved |
|------|----------------|------------------|----------------------|
| `tools/test/guitar/cmd/guitar.go` | modify | Extend existing pflag parsing without replacing the command structure | Existing `validate`/`run` positional behavior, `--timeout`, `--verbose`, and help behavior remain compatible |
| `tools/test/guitar/pkg/config/config.go` | modify | Add suite timeout as typed config data | Existing YAML files without `timeout` parse unchanged |
| `tools/test/guitar/pkg/validate/validate.go` | modify | Centralize timeout validation with existing suite field validation | Existing required-field, endpoint, deploy type/name checks remain unchanged |
| `tools/test/guitar/pkg/run/run.go` | modify | Refactor run flow to choose suites and derive test command contexts per suite | Suite order, fail-fast behavior, generated run IDs, deploy apply/delete, and test env injection remain unchanged |
| `tools/test/guitar/pkg/run/reporter.go` | new | Isolate human-readable formatting/color decisions from orchestration logic | Command execution behavior does not depend on terminal styling |
| `tools/test/guitar/*_test.go` and fixtures | modify | Lock new CLI/config/run behavior through unit tests | Existing test helper style and Bazel `go_unittest` targets remain |
| `tools/test/guitar/README.md`, `example.guitar.yaml` | modify | Document the changed user-facing contract | Existing examples remain valid except corrected/expanded fields |

## Test Impact Assessment

- `tools/test/guitar/cmd/guitar_test.go`: add parse cases for `run --suite suite-b <plan.yaml>`, reject empty suite values, reject `--suite` on `validate` because suite selection is run-only, and update help text assertions.
- `tools/test/guitar/pkg/config/config_test.go`: add `timeout` fixture coverage for omitted, positive, and zero values; ensure existing fixtures without timeout remain equal except zero-value field.
- `tools/test/guitar/pkg/validate/validate_test.go`: add negative timeout rejection and positive/zero timeout acceptance.
- `tools/test/guitar/pkg/run/run_test.go`: add only-selected-suite execution, nonexistent-suite error with available names, first duplicate name selection, per-suite timeout override, global timeout fallback, `timeout: 0` no test-level timeout, output title/indent/status checks, failure-color/status checks, and no-color checks for non-TTY writer mode.
- `tools/test/guitar/example.guitar.yaml`: validate after update with `guitar validate` or parser tests; keep it as documentation fixture if deploy paths are illustrative rather than runnable.

## Verification Plan

1. `bazel run //:go -- fmt tools/test/guitar/cmd/guitar.go tools/test/guitar/pkg/config/config.go tools/test/guitar/pkg/validate/validate.go tools/test/guitar/pkg/run/run.go tools/test/guitar/pkg/run/reporter.go tools/test/guitar/cmd/guitar_test.go tools/test/guitar/pkg/config/config_test.go tools/test/guitar/pkg/validate/validate_test.go tools/test/guitar/pkg/run/run_test.go`
2. `bazel test //tools/test/guitar/...`
3. `bazel build //tools/test/guitar/...`
4. `bazel run //:guitar_install` and manual CLI checks from `quickstart.md`
5. `bazel build //...` and `bazel test //...`, or record pre-existing blockers with evidence
