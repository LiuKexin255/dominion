# SigNoz MCP OpenCode template

This template configures OpenCode to start a local `signoz-mcp-server` process
through stdio, expose only logs/traces related SigNoz MCP tools, and provide an
optional `/signoz` command prompt in the same personal config file.

## Prerequisites

* `signoz-mcp-server` is installed on the developer machine.
* The local user has a SigNoz service account key with viewer/read-only access.
* The SigNoz instance contains logs/traces queryable by:
  * `service.name`
  * `deployment.environment.name`
  * `trace_id`

## Files

* `opencode.jsonc` — personal OpenCode MCP configuration template, including an
  optional `/signoz` command prompt.

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

## Fixed prompt

The template defines a `/signoz` command in `opencode.jsonc`. Use it for common
SigNoz log/trace investigations so the agent consistently applies the repository
field mapping and tool restrictions.

OpenCode supports Markdown files for commands, but MCP server configuration is
JSON/JSONC config only. Keeping the command prompt in `opencode.jsonc` lets the
MCP configuration and its usage prompt stay in one personal file.
