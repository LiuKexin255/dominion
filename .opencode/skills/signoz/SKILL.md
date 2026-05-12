---
name: signoz
description: Query SigNoz logs and traces through the local SigNoz MCP server.
compatibility: opencode
metadata:
  audience: dominion
  scope: observability
---

# signoz

Use this skill when you need to investigate SigNoz logs and traces through the local `obs` MCP server.

## When to use

- A test, deployment, or service behavior needs logs/traces for diagnosis.
- The user provides a `service`, `env`, `trace_id`, or time window and asks for SigNoz evidence.
- You need to verify that a fix removed the corresponding error logs or failed traces.

## Allowed MCP tools

- `obs_signoz_search_logs`
- `obs_signoz_aggregate_logs`
- `obs_signoz_search_traces`
- `obs_signoz_aggregate_traces`
- `obs_signoz_get_trace_details`

Do not use dashboard, alert, notification, view, metric, or write tools.

## Parameter mapping

- User `service` means SigNoz field `service.name`.
- User `env` means SigNoz field `deployment.environment.name`.
- User `trace_id` means SigNoz field `trace_id`.

## Default behavior

1. If `trace_id` is provided, call `obs_signoz_get_trace_details` first, then use `obs_signoz_search_logs` for logs with the same `trace_id` when log context is needed.
2. If `service` and `env` are provided, include both filters in every log/trace query.
3. If no time range is provided, use the last 30 minutes for errors and the last 1 hour for broader investigation.
4. Prefer narrow queries first: service + env + severity/error + time range.
5. Summarize findings with timestamps, service, env, trace IDs, and the likely root cause. Do not expose secrets or tokens from log bodies.

## Useful request forms

- `service=<app/service> env=<environment> recent errors`
- `service=<app/service> env=<environment> trace errors last 1h`
- `trace_id=<trace_id> explain request path and related logs`
