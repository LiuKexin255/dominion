# Quickstart: Guitar CLI Enhancements

## Prerequisites

- Repository root: `/mnt/code/dominion`
- Bazel available through the repository wrappers
- `deploy` installed when running real `guitar run` scenarios:

```bash
bazel run //:deploy_install
```

Install guitar after implementation:

```bash
bazel run //:guitar_install
```

## Targeted validation

Run package tests:

```bash
bazel test //tools/test/guitar/...
```

Build the tool:

```bash
bazel build //tools/test/guitar/...
```

## Manual CLI scenarios

Create or reuse a testplan with at least three suites. One suite should include a positive timeout and one should omit timeout:

```yaml
name: guitar-enhancement-check
suites:
  - name: suite-a
    deploy: //path/to/deploy_a.yaml
    timeout: 30
    cases:
      - //path/to:suite_a_test
  - name: suite-b
    deploy: //path/to/deploy_b.yaml
    cases:
      - //path/to:suite_b_test
  - name: suite-c
    deploy: //path/to/deploy_c.yaml
    timeout: 0
    cases:
      - //path/to:suite_c_test
```

### Scenario 1: validate timeout schema

```bash
guitar validate <plan.yaml>
```

Expected outcome: validation passes for omitted, zero, and positive timeout values. A plan with `timeout: -1` fails validation.

### Scenario 2: run all suites with readable output

```bash
guitar run <plan.yaml>
```

Expected outcome: each suite has a visible header, indented deploy/test/cleanup steps, and success/failure status. Multi-suite output can be scanned by suite name.

### Scenario 3: run one suite

```bash
guitar run --suite suite-b <plan.yaml>
```

Expected outcome: only `suite-b` deploys, tests, and cleans up. `suite-a` and `suite-c` do not execute.

### Scenario 4: missing suite name

```bash
guitar run --suite nonexistent <plan.yaml>
```

Expected outcome: command fails before deployment and lists available suite names.

### Scenario 5: redirected output has no ANSI color

```bash
guitar run --suite suite-b <plan.yaml> | tee /tmp/guitar-output.log
```

Expected outcome: `/tmp/guitar-output.log` contains suite headers, indentation, and statuses but no ANSI escape sequences.

## Repository verification

After implementation and targeted checks:

```bash
bazel build //...
bazel test //...
```

If a full-repository check is blocked by an unrelated pre-existing issue, record the command, failure summary, and residual validation risk in the implementation report.
