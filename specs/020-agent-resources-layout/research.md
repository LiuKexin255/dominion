# Research: Agent Resources Layout

**Feature**: 020-agent-resources-layout
**Date**: 2026-07-18
**Status**: Complete — all spec ambiguities resolved, design decisions fixed.

This file consolidates the three research threads that informed `plan.md`. Every claim is sourced; every source URL was actually fetched during research.

---

## §1 SKILL.md Convention

### Decision
Adopt the **agentskills.io open standard** as the documented file-format contract, with the OpenCode-recognized subset enforced as the minimum. This is the convention the user referred to as "常用规范" in the spec input.

### Rationale
1. It is the only SKILL.md convention published as a normative open standard with a reference validator (`skills-ref`).
2. The Dominion repo's existing skills already conform to it — both `.opencode/skills/testplan/SKILL.md` and `.opencode/skills/signoz/SKILL.md` use exactly `name` + `description` + `compatibility` + `metadata`.
3. OpenCode (the agent platform Dominion runs on) recognizes a **strict subset** of the spec; anything we write under Convention A's required+recommended-optional fields loads cleanly in OpenCode. Anything we avoid (`allowed-tools` and Claude Code's many extension fields) is non-portable.

### Required frontmatter (per agentskills.io spec)
| Field | Type | Constraint |
|---|---|---|
| `name` | string | 1–64 chars, regex `^[a-z0-9]+(-[a-z0-9]+)*$`, MUST match parent directory name |
| `description` | string | 1–1024 chars, non-empty, says what + when |

### Recommended-optional frontmatter (portable across Claude Code, OpenCode, claude.ai)
| Field | Type | Notes |
|---|---|---|
| `license` | string | SPDX-style |
| `compatibility` | string | 1–500 chars, environment requirements (e.g. `opencode`) |
| `metadata` | map<string,string> | Free-form; repo convention uses `audience` and `scope` sub-keys |

### Non-portable (MUST be documented as such in the skill README; MAY be omitted)
- `allowed-tools` — agentskills.io spec marks this **experimental**; OpenCode ignores it; only Claude Code uses it.
- All Claude-Code-only extension fields (`when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `disallowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`, `paths`, `shell`).

### Body
Free-form markdown. Recommended sections (from the spec): purpose, when-to-use, how-to-use, examples, edge cases. Keep under ~500 lines / ~5000 tokens; offload detail to sibling files (`scripts/`, `references/`, `assets/`) — those are NOT in scope for this feature but the README should mention that the SKILL.md is the entry point and other files may live alongside it inside `skill/{skill_name}/`.

### Sources (all fetched 2026-07-18)
- Open standard spec: https://agentskills.io/specification
- Spec source: https://github.com/agentskills/agentskills/blob/main/docs/specification.mdx
- Anthropic skills reference repo: https://github.com/anthropics/skills/blob/main/README.md
- Anthropic announcement: https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills
- Reference validator: https://github.com/agentskills/agentskills/tree/main/skills-ref
- OpenCode skills docs (the subset Dominion runs on): https://opencode.ai/docs/skills/ · source: https://github.com/sst/opencode/blob/dev/packages/web/src/content/docs/skills.mdx
- Claude Code authoring guide (superset — DO NOT use the extension fields): https://code.claude.com/docs/en/skills.md · https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview
- Real-world corroboration (Apache Groovy uses the same field set): https://github.com/apache/groovy/blob/master/.agents/skills/groovy-build/SKILL.md
- Cursor uses a different format (`.cursor/rules/*.mdc`), NOT SKILL.md — rejected.

### Rejected alternatives
- **skillmd / 402md** (https://github.com/402md/skillmd/blob/main/SPEC.md): adds `endpoints`, `pricingModel`, `auth`, `version`, `displayName`, `author`, `type`, `tags`. Aimed at paid API marketplaces. Overkill for Dominion.
- **skillrt** (https://github.com/jakejjoyner/skillrt/blob/main/spec/SKILL-SPEC.md): adds `runtime`, `inputs`, `outputs`, `dependencies`, `permissions`. Aimed at executable skills. Overkill.
- **Claude Code extension fields**: non-portable; rejected.

---

## §2 LangChain Tool Metadata Surface (for the `standalone` attribute)

### Decision
Use LangChain's **`extras` field** on the `tool()` factory's options bag to carry `standalone`. Add a typed `isStandalone(tool)` helper that reads it back. Do NOT use `metadata`, do NOT introduce a wrapper type.

### Rationale
1. `extras` is declared directly on `StructuredToolInterface` (https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/types.ts) and on `ToolWrapperParams` (the options bag of `tool()`), so it is statically typed end-to-end — no casts, no type-narrowing helper needed to *write* it.
2. The `tool()` factory returns a `DynamicStructuredTool` whose constructor stores `extras` as a real enumerable property of the instance (`libs/langchain-core/src/tools/index.ts`), so it survives the `createAgent({ tools })` round-trip unchanged (same instance reference, no clone, no strip).
3. **`extras` does NOT propagate to emitted `ToolMessage`s or LangSmith traces** — it is documented as "provider-specific extra fields for the tool… used to pass provider-specific configuration that doesn't fit into standard tool fields." This is exactly our use case (a Dominion-specific visibility flag).
4. **`metadata` was rejected** because research confirmed it propagates to every `ToolMessage` produced by the tool and to LangSmith traces (`StructuredTool.call` passes `this.metadata` into `_formatToolOutput`). That is semantic overloading of a LangChain-blessed observability channel; the `standalone` flag has nothing to do with observability.
5. **Wrapper-type (`{ tool, standalone }`) was rejected** because it adds a layer between `buildTools` and `createAgent` (`definitions.map(d => d.tool)`) for no functional gain in this feature — the spec Key Entities wording ("The attribute is part of the tool definition itself, not a separate registry") asks for the attribute to ride on the tool instance, which `extras` does natively.

### Pattern (final)
```typescript
// src/tools/types.ts
import type { StructuredToolInterface } from "@langchain/core/tools";

/** Shape of the per-tool `extras` payload that every tool definition MUST set. */
export interface StandaloneExtras {
  /** `true` = tool is individually selectable on the desktop profile-config page;
   *  `false` = hidden because intended for future toolset/collection use only. */
  standalone: boolean;
}

/**
 * Read the `standalone` flag off any tool. Defaults to `true` when the tool
 * omits `extras.standalone` (preserves today's desktop-display behavior —
 * spec Assumption "default value of `standalone`").
 */
export function isStandalone(tool: StructuredToolInterface): boolean {
  const extras = tool.extras as Partial<StandaloneExtras> | undefined;
  return extras?.standalone !== false;
}
```

Tool factory call site:
```typescript
// src/tools/mouse_move/mouse-move.ts
return tool(
  async ({ x_px, y_px }, config) => { /* unchanged */ },
  {
    name: "mouse_move",
    description: "...",
    schema: mouseMoveSchema,
    extras: { standalone: true } satisfies StandaloneExtras,
  },
);
```

### Sources (all fetched 2026-07-18)
- `StructuredToolInterface` shape (declares `extras?`): https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/types.ts
- `tool()` factory and `DynamicStructuredTool` constructor (stores `extras` and `metadata`): https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/index.ts
- `ToolWrapperParams extends ToolParams` (the options bag — both `extras` and `metadata` accepted): same file.
- Docs index for tools: https://v03.api.js.langchain.com/classes/_langchain_core.tools.StructuredTool.html
- No first-class "standalone" / per-tool-flag concept exists in LangChain — confirmed by exhaustive read of the above two source files.

### Rejected alternatives
- **`metadata` field**: rejected — propagates to `ToolMessage` and traces (semantic overloading).
- **Wrapper type `{ tool: StructuredToolInterface; standalone: boolean }`**: rejected — adds a layer for no functional gain; doesn't match spec wording "part of the tool definition itself".
- **Top-level property on the tool instance** (e.g. `(tool as any).standalone = true`): rejected — untyped, hacky, no LangChain support.

---

## §3 Dominion Repo Convention Survey

### Decision
Follow the repo's existing conventions exactly: kebab-case module files named after the module (no `index.ts` in `projects/game/agent/src/`), test files side-by-side with source (`.test.ts`), and minimal file-format-only READMEs modeled on `tools/templates/README.md`.

### Findings

#### Q1 — Test colocation
Side-by-side. Every `.test.ts` sits next to its `.ts` in flat `src/`. Examples: `projects/game/agent/src/mouse-tool.ts` + `projects/game/agent/src/mouse-tool.test.ts`; `projects/game/agent/src/llm.ts` + `projects/game/agent/src/llm.test.ts`. The `vitest_test` rule's default `args = ["run", "src/"]` (at `tools/dev/js/vitest_test.bzl`) sweeps everything under `src/`, so subdirectories like `src/tools/` are auto-covered.

#### Q2 — TS module naming
**kebab-case, named-after-module**. All 26 `.ts` files in `projects/game/agent/src/` follow this. No `index.ts` exists under `projects/game/agent/` (only in `common/js/*/src/` library barrels). So new files will be e.g. `tools/mouse_move/mouse-move.ts` (folder uses the LangChain tool name with underscores to match the `name` field; file uses kebab-case per repo style).

Note: folder names for tool folders use the **LangChain tool name verbatim** (`mouse_move`, `mouse_click`) so that the folder name === the value users put in `tool_names` on a profile. This is a deliberate one-off divergence from kebab-case for folder names — documented in the tools README.

#### Q3 — Existing SKILL.md files
Two exist, both under `.opencode/skills/`:
- `.opencode/skills/signoz/SKILL.md` — frontmatter: `name`, `description`, `compatibility: opencode`, `metadata: { audience: dominion, scope: observability }`.
- `.opencode/skills/testplan/SKILL.md` — same field set; `metadata: { audience: dominion, scope: large-test }`. Body is bilingual (Chinese + English) with sections "何时使用", "必须先读的仓库约定".

No other SKILL.md files exist anywhere in the repo. The new `src/skill/` directory starts empty except for its README — no built-in skill is authored in this feature.

#### Q4 — Existing `tools/` directories
Only one: the repo-root `tools/` containing build/dev/release/test infrastructure (`tools/dev/`, `tools/release/`, `tools/templates/`, `tools/test/`). No conflict with the new `projects/game/agent/src/tools/` (different parent, different purpose — the README disambiguates).

#### Q5 — `buildTools` dispatch pattern
At `projects/game/agent/src/llm.ts:57-70`. Simple `for` loop with `if/else if` chain on string tool names. Unknown names silently skipped. Each factory receives the session-scoped `OperationBridge`.

```typescript
export function buildTools(
	toolNames: string[],
	bridge: OperationBridge,
): StructuredToolInterface[] {
	const tools: StructuredToolInterface[] = [];
	for (const name of toolNames) {
		if (name === "mouse_move") {
			tools.push(createMouseMoveTool(bridge));
		} else if (name === "mouse_click") {
			tools.push(createMouseClickTool(bridge));
		}
	}
	return tools;
}
```

Imports at `projects/game/agent/src/llm.ts:17`: `import { createMouseClickTool, createMouseMoveTool } from "./mouse-tool";` — these two import paths will change to `"./tools/mouse_move/mouse-move"` and `"./tools/mouse_click/mouse-click"`. The `buildTools` body stays unchanged; no registry is introduced in this feature (out of scope; future feature can refactor).

#### Q6 — `vitest_test` location rules
Source: `tools/dev/js/vitest_test.bzl`. Default `args = ["run", "src/"]`. Caller-supplied `data = glob(["src/**/*.ts"])` sweeps the entire `src/` tree. Tests MUST live under `src/` (the discovery root); subdirectories are auto-included. No rule mandates that a test file must be at the same level as its source — but the repo convention is side-by-side, which we follow.

#### Q7 — README.md style precedents
The repo's READMEs span minimal → rich:
- `tools/templates/README.md` (16 lines) — **minimal, file-format-only**. Model for the new `mcp/` and `skill/` READMEs.
- `tools/test/guitar/README.md` (83 lines) — operational, command-reference.
- `projects/infra/deploy/README.md` (28 lines) — architectural overview.
- `projects/game/desktop/README.md` (54 lines) — procedural warnings.
- `projects/game/testplan/README.md` (175 lines) — **rich, framework-heavy** with schema tables.

The new `mcp/` and `skill/` READMEs follow `tools/templates/README.md`'s minimal style (spec FR-005 mandates file-format only). The new `tools/` README follows a slightly richer style — FR-006 allows framework content, and contributors need to know how `buildTools` consumes a new tool — but it still leads with the file-format contract.

### Sources
- File paths cited inline above (all repo-relative).
- All paths verified by `ls` / `read` during research (2026-07-18).

---

## Open questions resolved by this research

| Spec assumption / ambiguity | Resolution | Source |
|---|---|---|
| "Common SKILL.md convention" | agentskills.io open standard; required `name`+`description`, recommended `compatibility`+`metadata` | §1 above |
| Where the `standalone` attribute lives on a tool | LangChain `extras` field on the `tool()` options bag | §2 above |
| Whether `extras` survives `createAgent` | Yes — same instance reference, no clone | §2 above |
| Where shared helpers between tightly-coupled tools live | `tools/shared/` (sibling of per-tool folders, no underscore prefix) | §3 + plan.md Structure Decision |
| Whether test files split when source splits | Yes — `mouse-tool.test.ts` relocates-and-splits into 3 files; assertions preserved verbatim | §3 + plan.md Structure Decision |
| Whether `buildTools` becomes a registry | No — stays as if/else if; only import paths change | §3 Q5 |
| Folder naming when LangChain tool name uses underscores | Folder uses LangChain name verbatim (`mouse_move`) so folder === `tool_names` value; files inside still kebab-case | §3 Q2 |

## Open questions intentionally deferred to `/speckit.tasks`

- Exact line-by-line split boundaries for `mouse-tool.test.ts` (which `it(...)` block goes to which file). The current test file's header comment already partitions tests by tool — that comment is the split map.
- Whether to add a unit test for `isStandalone` covering the default-when-omitted case (yes; the helper's contract is testable in isolation).
- Whether to regenerate `BUILD.bazel` once or after each move (once, after all moves complete, via `bazel run //:gazelle projects/game/agent`).
