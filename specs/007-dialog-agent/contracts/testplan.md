# Contract: Large-Test Plan

## Single YAML entrypoint

The game system large-test entrypoint remains one YAML file:

```text
projects/game/testplan/system_test.yaml
```

It contains multiple suites. Each suite references a deploy YAML that includes only the services required for that suite.

## Required suite categories

| Suite category | Required services | Must not deploy | Purpose |
|---|---|---|---|
| Prompt/profile | mongo, prompt, gateway | agent, proxy | Profile CRUD and profile deletion behavior. |
| Session/proxy regression | mongo, session, proxy, gateway as needed | prompt unless required, real provider | Existing session/owner/WebSocket routing behavior. |
| Agent dialog | mongo, session, proxy, agent, gateway, fake LLM wiring | real provider calls | Create agent, connect chat, receive thinking + final response, queue messages. |
| Full surface | only when needed: session, proxy, prompt, agent, gateway, mongo | real provider calls | End-to-end desktop/gateway/proxy/agent behavior. |

## Fake LLM/provider rule

- Large tests must not send messages to the real provider.
- The substitute should be injected at the LLM module boundary so grpc-js server, proxy, gateway, and UI-facing protocol paths still run normally.
- The substitute must not add a new production service config.
- If a test-only artifact is needed, it must live under testplan material and be referenced only from suite deploy YAML.
- Prefer adding test artifact/env wiring to the existing deploy composition rather than changing `projects/game/agent/service.yaml` for tests.

## Guitar/testtool expectations

Each suite uses `endpoint.http.public` and Go tests read it through:

```go
testtool.MustEndpoint("http", "public")
testtool.MustEnv()
```

Validation commands:

```bash
guitar validate projects/game/testplan/system_test.yaml
guitar run projects/game/testplan/system_test.yaml
```
