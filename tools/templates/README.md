# Templates

This directory stores copy-only templates for local developer setup.

Templates in this directory are not loaded by Bazel, OpenCode, or any runtime
component automatically. Copy the relevant template into the documented target
location and replace placeholder values locally.

Do not commit secrets, API keys, service account keys, tokens, or local absolute
paths after filling in a template.

## Available templates

* `signoz-mcp-opencode/` — personal OpenCode configuration and optional command
  prompt for querying SigNoz logs and traces through the SigNoz MCP server in
  stdio mode.
