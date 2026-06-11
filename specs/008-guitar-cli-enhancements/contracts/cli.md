# Contract: Guitar CLI

## Command: `guitar validate`

```bash
guitar validate [--timeout=10m] <plan.yaml>
```

Behavior remains full-plan validation. New suite `timeout` fields are accepted when omitted, zero, or positive integer seconds and rejected when negative or non-numeric.

`--suite` is not part of the validate contract for this feature.

## Command: `guitar run`

```bash
guitar run [--timeout=10m] [--suite <suite-name>] [-v|--verbose] <plan.yaml>
```

### Options

| Option | Value | Behavior |
|--------|-------|----------|
| `--timeout` | Go duration, default `10m` | Existing overall execution timeout. Also acts as the fallback test timeout for suites without YAML `timeout`. |
| `--suite` | exact suite name | Runs only the first suite whose `name` equals the value. Empty values are invalid. |
| `-v`, `--verbose` | bool | Existing trace ID output behavior remains. |

### Success behavior

- Without `--suite`, suites run in YAML order.
- With `--suite`, only the selected suite deploys, tests, and cleans up.
- Non-selected suites must not generate run IDs, call `deploy apply`, call `bazel test`, or call `deploy del`.
- Existing fail-fast behavior remains: the first failing executed suite stops the run after cleanup.

### Error behavior

- Missing plan argument: return the existing positional-argument error.
- `--suite ""` or whitespace-only: return an error before parsing/execution.
- `--suite nonexistent`: return an error before deploy/test/cleanup and include all available suite names.
- Duplicate suite names: run the first matching suite only.

### Timeout behavior

- Suite timeout applies only to `bazel test` for that suite.
- Omitted suite timeout: `bazel test` uses the global `--timeout` as fallback.
- Positive suite timeout: `bazel test` uses that deadline, shortened if the positive global run timeout is smaller.
- `timeout: 0`: no suite-level `bazel test` deadline is added; the global run context still bounds the process when `--timeout` is positive.

## Help output

`guitar --help` must document `run --suite <suite-name>` and keep existing command/flag descriptions.
