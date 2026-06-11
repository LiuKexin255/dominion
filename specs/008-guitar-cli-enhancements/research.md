# Research: Guitar CLI Enhancements

## Decision: Keep enhancements inside the existing Go/pflag guitar CLI

**Rationale**: `tools/test/guitar/cmd/guitar.go` already owns `validate` and `run` parsing with `github.com/spf13/pflag`, and `pkg/run.Run` owns validation, deploy, test, and cleanup sequencing. Extending those seams is smaller and preserves existing behavior.

**Alternatives considered**:
- Replace with Cobra command hierarchy: rejected because current pflag command table is small and sufficient.
- Add a separate wrapper command: rejected because it would split the user-facing contract and duplicate validation/run behavior.

## Decision: Implement terminal-aware output with an internal reporter and direct ANSI codes

**Rationale**: The feature needs suite headers, indentation, status summaries, and automatic color disabling for redirected output. A small `pkg/run` reporter can own formatting decisions while keeping orchestration testable. Direct ANSI constants avoid adding dependencies and Bazel module churn.

**Alternatives considered**:
- Add a color library: rejected because the needed palette is tiny and no dependency is required.
- Print formatting inline in `run.go`: rejected because it would mix command orchestration with presentation and make no-color testing harder.

## Decision: Detect color capability at the writer boundary

**Rationale**: Current tests replace package-level `stdout`/`stderr`, so color capability should be injectable/testable rather than hard-coded to `os.Stdout`. Runtime detection should enable color only when the actual output is a terminal and `TERM` is neither empty nor `dumb`; non-terminal writers produce plain text.

**Alternatives considered**:
- Always color: rejected by FR-004 and redirected log cleanliness.
- Add a CLI `--color` flag now: rejected because the spec only requires automatic behavior and adding flags expands the contract.

## Decision: Add `--suite` only to `guitar run`

**Rationale**: The feature request scopes suite selection to execution. Validation should continue to validate the whole plan so users can catch all static issues unless future requirements add validate filtering. `run --suite` filters after parsing and before executing deploy/test/cleanup.

**Alternatives considered**:
- Support `--suite` for `validate`: rejected because it would weaken full-plan validation and is not requested.
- Allow partial/regex matching: rejected because the spec assumes exact matching.

## Decision: Select the first suite with a matching name and report all available names on miss

**Rationale**: The spec explicitly requires first-match behavior if duplicate names exist and a helpful error listing available suite names when no suite matches.

**Alternatives considered**:
- Reject duplicate names during validation: rejected because the spec requires first-match semantics.
- Run all matching duplicate suites: rejected by the acceptance scenario requiring only the first match.

## Decision: Model suite timeout as plain `int` on `config.Suite` (0 = not set = fallback to global)

**Rationale**: YAML needs a `timeout` field in seconds. With two-way semantics (omitted/0 = fallback to global, >0 = suite timeout), a plain `int` with `0` meaning "not set" is simpler than a pointer type. Both omitted and explicit `0` fall back to the global `--timeout`.

**Alternatives considered**:
- Use `time.Duration` in YAML: rejected because the spec says seconds and examples use integers.
- Use pointer/optional integer: rejected because two-way semantics make omission and `0` equivalent, so the pointer distinction is unnecessary.

## Decision: Use context nesting for both global and suite timeouts

**Rationale**: The spec assumption states suite timeout applies to the test execution phase only and does not cover deploy/cleanup. The existing global `--timeout` wraps the whole run via a context deadline. The suite timeout creates a child context with its own deadline — if the suite timeout fires first, the test is interrupted; if the global timeout fires first, the entire run is interrupted. No manual `min()` calculation is needed.

**Alternatives considered**:
- Apply suite timeout to deploy/test/cleanup: rejected by the feature assumption.
- Replace the global timeout with per-suite timeout: rejected because existing global timeout behavior must remain compatible.
- Manual `min()` of global and suite timeouts: rejected because context nesting achieves the same effect naturally.

## Decision: Keep empty `suites` invalid

**Rationale**: Existing `validate.Validate` already rejects empty suite lists with `config suites must not be empty`. The edge case remains an error for both `validate` and `run`, which is consistent with current behavior and avoids silently doing nothing.

**Alternatives considered**:
- Treat empty suites as a successful no-op: rejected because it would hide invalid testplans.

## Decision: Update documentation and example YAML as part of the contract

**Rationale**: `tools/test/guitar/README.md` and `tools/test/guitar/example.guitar.yaml` are the user-facing discovery points. The example should show `timeout` and the README should document `--suite`, timeout fallback, and color behavior.

**Alternatives considered**:
- Only update tests: rejected because CLI users need documented behavior.
