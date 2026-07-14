# Research: LangChain.js MCP & Skill Integration + deep-agents Reference

**Feature**: 018-saolei-mcp | **Date**: 2026-07-14 | **Phase**: 0 (Constitution §III — External Dependency Research)

**Scope**: this is the first time the codebase introduces MCP and skill concepts. Per Constitution §III, the LangChain.js MCP/skill support, the MCP TypeScript SDK in-process transport, and deep-agents' skill/MCP patterns were researched against official documentation and source repositories before this plan was finalized. This document records the decisions (D-1, D-2 from [plan.md](./plan.md)), their rationale, the alternatives evaluated, and the citations.

## R-1 — MCP integration: in-process MCP server ⇒ ordinary LangChain tools, no new dependency

**Decision (confirms plan D-1)**: the saolei MCP server, running **in-process** inside the agent service, exposes its five tools as **ordinary LangChain tools** built with the existing `tool()` factory + `zod` schema pattern (identical to the mouse tool in `mouse-tool.ts`). The MCP wire protocol is **not** run over an in-memory transport. "MCP" is retained as the **conceptual/logical grouping**: one `mcp_names` profile entry (`"saolei"`) selects the bundle of tools + their shared board state. **No new runtime dependency is introduced.**

**Rationale**:

1. The official LangChain.js MCP client adapter, [`@langchain/mcp-adapters`](https://www.npmjs.com/package/@langchain/mcp-adapters) (current version **1.1.3**, filed 2026-03-12), supports **only three transports**: `stdio` (child process), `http` (StreamableHTTP), and `sse` (legacy). Its `ConnectionManager` hard-codes `transportTypes = ["http", "sse", "stdio"]` ([`connection.ts`](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/src/connection.ts)), and `clientConfigSchema` accepts only `stdioConnectionSchema | streamableHttpConnectionSchema` ([`types.ts`](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/src/types.ts)). There is **no in-process transport** and no extension point for one.
2. `loadMcpTools()` ([`tools.ts`](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/src/tools.ts)) converts each MCP tool into a `DynamicStructuredTool` by dereferencing + simplifying its JSON Schema into Zod, then wrapping `client.callTool()`. That is **the same type and interface** our existing `tool()` + zod factories already produce — so routing through the MCP wire protocol in-process would add a JSON-RPC serialization round-trip for zero functional gain.
3. The MCP TypeScript SDK *does* ship [`InMemoryTransport`](https://ts.sdk.modelcontextprotocol.io/v2/classes/_modelcontextprotocol_client.index.InMemoryTransport.html) (`createLinkedPair()`), but (a) `@langchain/mcp-adapters` cannot consume it (no custom-transport path), and (b) the SDK itself documents it as *"Intended for testing and development"* ([custom transports](https://ts.sdk.modelcontextprotocol.io/v2/advanced/custom-transports.html)) — not a production in-process channel.
4. Using `@langchain/mcp-adapters` would therefore force the saolei server into a **child process (stdio)** or a **localhost HTTP port** — adding subprocess/port lifecycle, serialization, and error surface for tools that already live in the same Node.js process as the agent.

**Alternatives considered**:

- **(a) `@langchain/mcp-adapters` over stdio**: spawn the saolei MCP as a child `node` process. *Rejected*: the saolei MCP needs the session-scoped `OperationBridge` (which lives in the agent process) to reach the desktop window; a child process could not share that bridge without an extra IPC layer, compounding the complexity. Also adds per-session subprocess churn.
- **(b) `@langchain/mcp-adapters` over localhost HTTP**: run a StreamableHTTP MCP endpoint in-process and connect back to it. *Rejected*: a loopback HTTP connection to serve tools to yourself is pure overhead (TLS-less HTTP + JSON-RPC framing) with no benefit over a direct function call.
- **(c) `@modelcontextprotocol/sdk` `InMemoryTransport` + manual `loadMcpTools()`**: connect a client/server pair via `createLinkedPair()` and pass the connected client to `loadMcpTools()`. *Rejected*: loses `MultiServerMCPClient` orchestration (reconnection, resource discovery, notifications), and the SDK marks `InMemoryTransport` as test-only. Not justified for production.

**Forward compatibility note**: if the saolei tools ever need to be consumed by an *external* MCP client (another agent, an IDE), they can later be wrapped with `@modelcontextprotocol/sdk`'s `Server` + `StdioServerTransport`/`StreamableHTTPServerTransport` as a **separate, complementary** concern — the plain-tool implementation does not preclude that.

## R-2 — Skill injection: assemble skill markdown into the system prompt at agent creation

**Decision (confirms plan D-2)**: a skill is a markdown document (adopting the [Agent Skills open standard](https://agentskills.io/specification) `SKILL.md` shape — YAML frontmatter `name`/`description` + markdown body). At agent creation, the profile's `skill_names` are resolved via the existing `GetSkill` RPC, and each skill's content is **concatenated into the `systemPrompt`** passed to LangChain v1 `createAgent` ([LangChain agents](https://docs.langchain.com/oss/javascript/langchain/agents)), behind a stable separator. **No new LangChain concept or dependency is introduced.**

**Rationale**:

1. This is precisely the mechanism deep agents uses. Its `SkillsMiddleware.modify_request()` ([`deepagents/middleware/skills.py`](https://github.com/langchain-ai/deepagents/blob/46e10640caf78a84f9715cb8807882ea1b825d6a/libs/deepagents/deepagents/middleware/skills.py), commit `46e10640c`) **appends a formatted block onto the system message** via `append_to_system_message()`. The default template lists each skill's `name` + `description` and how to load its full body. I.e. skill injection in deep agents *is* system-prompt assembly.
2. LangChain.js documents a "Skills" pattern ([LangChain skills](https://docs.langchain.com/oss/javascript/langchain/multi-agent/skills)) that is itself just middleware (`createMiddleware`) + a `load_skill` tool for **progressive disclosure** — i.e. system-prompt assembly with on-demand loading, not a native runtime type.
3. For a single, modestly-sized saolei skill, **full-body concatenation** into the system prompt is the simplest reliable path: the system prompt is present at every turn, survives context compression, and benefits from prompt caching on supported models. Progressive disclosure (summary in prompt + a read tool for the full body) is a deferred refinement if the skill grows large.

**Adopted from deep-agents / the open standard** (reference only — no deepagents code is imported):

- The `SKILL.md` file format (YAML frontmatter `name`/`description` + markdown body) from the [Agent Skills specification](https://agentskills.io/specification) and [Anthropic Agent Skills](https://code.claude.com/docs/en/skills) — gives the saolei skill a recognized, tooling-friendly structure.
- The **logical tool grouping** idea from `MultiServerMCPClient` (tools grouped under a named server): the saolei tools are grouped under the `"saolei"` mcp name, mirroring how MCP organizes a server's tools.
- The **load-once-at-startup, static-for-session** lifecycle: skills are resolved at agent creation and do not change mid-session (matches our spec FR-027 — dynamic mid-session injection is out of scope).

**Alternatives considered**:

- **Progressive disclosure (summary in prompt + `read_skill` tool)**: *Deferred* (not rejected) — adopt only if the saolei skill document grows large enough that full-body inclusion measurably bloats context. For v1, full-body inclusion is simpler and sufficient.
- **A dedicated skill middleware** mirroring `SkillsMiddleware`: *Rejected for v1* — overkill for a single skill; the adapter already composes `systemPrompt` in one place (`AgentAdapterImpl` constructor), which is the correct, minimal injection point.

## R-3 — Why the in-process MCP is still legitimately "MCP"

The spec's terminology note (and the user's correction) holds: this *is* the [Model Context Protocol](https://modelcontextprotocol.io/) conceptually — an MCP server exposing tools to an MCP client. The *only* thing that differs from the textbook MCP topology ([MCP architecture](https://modelcontextprotocol.io/docs/concepts/architecture)) is the **transport**: because the server is embedded in the agent service, the client and server share an address space, so the "transport" collapses to direct in-process function calls rather than a wire protocol. This is a deployment-form difference, not a semantic one — the tool discovery, schema, invocation, and result semantics are MCP-shaped. We retain the `mcp_names` profile field and the "MCP" naming to preserve that conceptual model.

## Summary of decisions folded back into plan.md

| Decision | Verdict | New dependency? |
|---|---|---|
| **D-1** MCP integration | Confirmed — plain LangChain tools (`tool()` + zod), grouped as `"saolei"` mcp | **No** |
| **D-2** Skill injection | Confirmed — skill markdown concatenated into `systemPrompt`; adopt `SKILL.md` format | **No** |

The §III gate in [plan.md](./plan.md) is now satisfiable: every external dependency referenced (LangChain v1, `@langchain/mcp-adapters`, `@modelcontextprotocol/sdk`, deep agents, the Agent Skills spec) has been researched against its official docs/source with version/commit pins below, and the decision introduces **no new runtime dependency**.

## References

### Official Documentation
- [LangChain.js — overview](https://docs.langchain.com/oss/javascript/langchain/overview) — LangChain v1; `createAgent` entry point (catalog: `langchain ^1.5.0`, `@langchain/core ^1.2.0`, `@langchain/langgraph ^1.4.4`).
- [LangChain.js — agents](https://docs.langchain.com/oss/javascript/langchain/agents) — `createAgent({ model, systemPrompt, tools, middleware, checkpointer })`.
- [LangChain.js — MCP](https://docs.langchain.com/oss/javascript/langchain/mcp) — official MCP client adapter docs.
- [LangChain.js — skills (multi-agent)](https://docs.langchain.com/oss/javascript/langchain/multi-agent/skills) — skills as middleware + progressive disclosure.
- [Model Context Protocol — introduction](https://modelcontextprotocol.io/) and [architecture](https://modelcontextprotocol.io/docs/concepts/architecture) — MCP host/client/server topology.
- [MCP TS SDK — InMemoryTransport](https://ts.sdk.modelcontextprotocol.io/v2/classes/_modelcontextprotocol_client.index.InMemoryTransport.html) — test-only in-process transport.
- [MCP TS SDK — custom transports](https://ts.sdk.modelcontextprotocol.io/v2/advanced/custom-transports.html) — "Intended for testing and development".
- [Agent Skills specification (agentskills.io)](https://agentskills.io/specification) — open `SKILL.md` standard.
- [Anthropic Agent Skills](https://code.claude.com/docs/en/skills) — skill file format and usage.

### Repositories
- [`@langchain/mcp-adapters` on npm](https://www.npmjs.com/package/@langchain/mcp-adapters) — version **1.1.3** (2026-03-12); peer deps `@langchain/core@^1.0.0`, `@langchain/langgraph@^1.0.0` (compatible with our catalog).
- [langchain-ai/langchainjs — `libs/langchain-mcp-adapters/src`](https://github.com/langchain-ai/langchainjs/tree/main/libs/langchain-mcp-adapters) — adapter source.
  - [`connection.ts`](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/src/connection.ts) — `transportTypes = ["http","sse","stdio"]` (no in-process).
  - [`types.ts`](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/src/types.ts) — `stdioConnectionSchema | streamableHttpConnectionSchema` only.
  - [`tools.ts`](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/src/tools.ts) — `loadMcpTools()` MCP→`DynamicStructuredTool` conversion.
- [langchain-ai/deepagents](https://github.com/langchain-ai/deepagents) — deep agents reference (MIT, Python); commit `46e10640c`.
  - [`deepagents/middleware/skills.py`](https://github.com/langchain-ai/deepagents/blob/46e10640caf78a84f9715cb8807882ea1b825d6a/libs/deepagents/deepagents/middleware/skills.py) — `SkillsMiddleware.modify_request()` appends to system message.
  - [`deepagents_cli/mcp_tools.py`](https://github.com/langchain-ai/deepagents/blob/bb27e62ebe44dd6e8104a504b1718ce87acc7ffa/libs/cli/deepagents_cli/mcp_tools.py) — MCP loading via `langchain-mcp-adapters` (stdio/sse/http only).

### Articles & RFCs
- [Minesweeper (video game) — keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game)) — F2 "new game" convention used by `saolei_init`.
