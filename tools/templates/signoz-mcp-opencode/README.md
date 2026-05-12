# SigNoz MCP OpenCode template

This template configures OpenCode to start a local `signoz-mcp-server` process
through stdio and expose only logs/traces related SigNoz MCP tools.

## Prerequisites

* `signoz-mcp-server` is installed on the developer machine.
* The local user has a SigNoz service account key with viewer/read-only access.
* The SigNoz instance contains logs/traces queryable by:
  * `service.name`
  * `deployment.environment.name`
  * `trace_id`

## Files

* `opencode.jsonc` — personal OpenCode MCP configuration template.

## Install locally

Copy `opencode.jsonc` into the personal OpenCode config, or merge its contents
into the existing file:

```bash
mkdir -p ~/.config/opencode
cp tools/templates/signoz-mcp-opencode/opencode.jsonc ~/.config/opencode/opencode.json
```

Then edit `~/.config/opencode/opencode.json` and replace:

* `/absolute/path/to/signoz-mcp-server`
* `<your-signoz-url>`
* `<your-signoz-service-account-key>`

Do not copy this template into `.opencode/opencode.json`; project-local MCP
configuration would be loaded automatically for everyone using the repository.

## Shared skill

The repository defines a shared `signoz` skill in `.opencode/skills/signoz/SKILL.md`.
Load it for common SigNoz log/trace investigations so the agent consistently
applies the repository field mapping and tool restrictions.

OpenCode supports Markdown files for skills, but MCP server configuration is
JSON/JSONC config only. This template only configures the personal MCP connection;
the shared skill stays in the repository.
