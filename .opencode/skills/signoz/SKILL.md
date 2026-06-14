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

## 日志查询方式

本仓库的被测服务支持以下三种日志查询方式：

**1. 查询指定 trace ID 日志**

使用 `obs_signoz_search_logs` 或 `obs_signoz_search_traces`，通过 trace ID 过滤。

**2. 查询应用上报日志**

使用 `obs_signoz_search_logs`，通过环境和 service 过滤：

- `deployment.environment.name`：测试计划中的 `suites[].env`（如 `game.lt`）
- `service.name`：被测服务标识（如 `game-agent`）

**3. 查询容器控制台输出**

使用 `obs_signoz_search_logs`，按 Pod 名称过滤：

- **过滤条件**：`k8s.pod.name CONTAINS '<env-scope>-<env-name>-<service-name>'`
- **字段来源**：
  - `<env-scope>` 和 `<env-name>`：`guitar run` 输出中部署环境的 scope 和 name 部分（例如环境 `game-ltkyfwdx`，scope 为 `game`，name 为 `ltkkyfwdx`）。
  - `<service-name>`：测试计划中部署的目标服务标识（如 `agent`、`gateway`）。
- **示例**：环境为 `game-ltkyfwdx`，服务为 `agent`：
  ```
  k8s.pod.name CONTAINS 'game-ltkyfwdx-agent'
  ```

> **注意**：控制台日志级别均为正常或 info。查询控制台输出时 `service.name` 与 `deployment.environment.name` 不生效，只能通过 `k8s.pod.name` 过滤。

## 日志查询 SOP

日志查询优先级按照 `trace id` >> 应用上报日志 >> 容器日志顺序进行查询。针对不同场景日志查询按以下 SOP 进行：

**a. 测试用例执行失败**：首先在测试代码中为请求设置 trace context（使用 `common/gopkg/otel/tracecontext/` 包），然后通过 trace ID 查询日志（方式 1），追踪请求的完整处理链路。

设置 trace context 的典型方式：

- 使用 `tracecontext.Ensure(ctx)` 确保请求携带 trace context
- 使用 `tracecontext.HTTPTransport` 将 traceparent 注入 HTTP 请求头
- 使用 `tracecontext.Environ(ctx)` 将 trace context 传递给子进程

**b. 不确定请求入口或请求入口不可控**：先指定环境和 service 检索应用上报日志（方式 2）；如果应用上报日志不存在，降级到容器控制台输出（方式 3）。

**c. 环境部署失败、服务异常退出**：查询容器控制台输出（方式 3），定位 OOM、启动崩溃、配置错误等。

修复后验证：重新执行测试，并按相同上下文再次查询确认错误日志/失败 trace 不再出现。
