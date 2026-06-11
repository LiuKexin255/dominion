# Data Model: Guitar CLI Enhancements

## Test Plan Config

Represents one YAML testplan parsed by `tools/test/guitar/pkg/config.Parse`.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `name` | string | yes | Existing plan name; must be non-empty. |
| `description` | string | no | Existing descriptive text. |
| `suites` | list of Suite | yes | Must contain at least one suite; order is execution order. |

## Suite

Represents one deploy/test/cleanup unit in `suites`.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `name` | string | yes | Used for output headers and `--suite` exact matching. Duplicate names are allowed; `--suite` runs the first match. |
| `env` | string | no, unsupported | Existing validation rejects this field when set. Runtime env remains generated per suite. |
| `deploy` | string | yes | Deploy config path, resolved through existing workspace rules. |
| `endpoint` | map(protocol -> name -> URL) | no | Existing endpoint env mapping and validation remain. |
| `cases` | list of Bazel targets | yes | Bazel large-test targets passed to `bazel test --config=largetest`. |
| `timeout` | optional integer seconds | no | New field. Omitted means use global `--timeout` as test timeout fallback. `0` means no suite-level test timeout. Positive values set the suite test timeout. Negative or non-numeric values are invalid. |

### Suite timeout state

```text
omitted ──validate──> fallback to global --timeout for bazel test
0       ──validate──> no suite-level test timeout; still bounded by global run context
>0      ──validate──> suite test timeout in seconds, shortened by positive global timeout when global is smaller
<0      ──validate──> error
non-int ──parse─────> error
```

## Suite Filter

Runtime-only value from `guitar run --suite <name>`.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `name` | string | no | Empty or whitespace-only values are invalid. When omitted, all suites run in file order. |

### Filter behavior

- Omitted: all suites execute in original order.
- Present and matches: only the first suite with exactly equal `Suite.name` executes.
- Present and missing: execution stops before deploy/test/cleanup and reports the requested name plus all available suite names.

## Suite Run Result

Transient reporting model used by the run reporter.

| Field | Type | Notes |
|-------|------|-------|
| `suiteName` | string | Printed in suite header and final status. |
| `runID` | string | Generated per executed suite. |
| `environmentName` | string | Existing `{scope}.{runID}` value. |
| `deployPath` | string | Printed in suite header or deploy step. |
| `status` | enum `running`/`success`/`failure` | Drives color and summary text. |
| `failedStep` | enum `validate`/`deploy`/`test`/`cleanup` | Included on failure when known. |
| `error` | error | Printed without ANSI color when output is non-terminal. |

## Validation Rules

- Config name must be non-empty.
- Suites must be non-empty.
- Suite name, deploy path, and cases must be non-empty.
- Suite `env` remains unsupported when set.
- Endpoint names keep existing `^[a-zA-Z][a-zA-Z0-9]*$` validation.
- Deploy config must remain test type and use `{scope}.{{run}}` naming.
- Suite timeout must be omitted, zero, or positive integer seconds.
