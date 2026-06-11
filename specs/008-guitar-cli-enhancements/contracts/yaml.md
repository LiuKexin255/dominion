# Contract: Guitar Testplan YAML

## Suite schema addition

```yaml
name: example-testplan
description: Optional description

suites:
  - name: smoke
    deploy: //projects/example/testplan/deploy_smoke.yaml
    timeout: 60
    endpoint:
      http:
        public: https://example.liukexin.com
    cases:
      - //projects/example/testplan:smoke_test
```

## `timeout`

| Value | Valid | Meaning |
|-------|-------|---------|
| omitted | yes | Use CLI global `--timeout` as test timeout fallback. |
| `0` | yes | Do not set a suite-level test timeout. |
| positive integer | yes | Use that many seconds as the suite test timeout. |
| negative integer | no | `guitar validate` fails. |
| non-integer/non-number | no | YAML parse or validation fails. |

## Compatibility

Existing testplans without `timeout` remain valid and run with current global timeout behavior.

Existing suite fields keep their current contract:

- `name`, `deploy`, and `cases` are required.
- `env` is unsupported and rejected when set.
- `endpoint` remains optional and uses the existing protocol/name/URL map.

## Documentation updates

- `tools/test/guitar/README.md` must document `timeout` in the suite format.
- `tools/test/guitar/example.guitar.yaml` must include at least one suite with `timeout`.
