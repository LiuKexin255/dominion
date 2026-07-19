# mcp/

This directory holds MCP integrations for the game agent.

## File-format contract

* One folder per integration, named `{mcp_name}` in lowercase kebab-case
  (e.g. `src/mcp/my-integration/`).
* The entry-point file naming inside `{mcp_name}/` is TBD when the first MCP is
  authored. No MCP integration ships in this feature; only this directory and
  its README exist today.

This README is intentionally limited to file-format conventions (folder
naming and required files). It deliberately omits framework integration,
runtime, or wiring content.
