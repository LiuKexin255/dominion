# Feature Specification: Agent Resources Layout

**Feature Branch**: `020-agent-resources-layout`

**Created**: 2026-07-18

**Status**: Draft

**Input**: User description: "对 projects/game/agent/ 进行优化：1. 将 mcp，tools 和 skill 按目录进行整理，例如 mcp/{mcp_name}/{some_file}，其中 SKILL 按照常用规范使用 SKILL.md 保存（skill 分为 built-in 和用户创建的 skill，用户创建的 skill 使用 game.proto 当中接口管理，本次改动不涉及这一类skill；这里说的是 built-in skill）。在 mcp、tools、skill 增加 Readme 说明规范（目前应该还没有 mcp 和 skill，先增加目录和 readme 即可，这两个的 readme 仅涉及文件格式规范，暂不引入任何框架有关内容）。2. 为 tools 增加 standalone 字段，standalone = true 的 tools 才会被显示在 desktop 的 agent profile 配置页面（即这个页面隐藏 standalone = false 的 tools）。这是为将来以集合的方式使用 tools 准备。"

## Clarifications

### Session 2026-07-18

- Q: Where should the `mcp/`, `tools/`, and `skill/` resource directories live — under `projects/game/agent/src/` (inherit existing `src/**/*.ts` build glob), at the agent project root alongside `src/`, or a hybrid? → A: Under `src/` (`projects/game/agent/src/{mcp,tools,skill}/`). Inherits the existing `BUILD.bazel` glob at `projects/game/agent/BUILD.bazel:19` and keeps all TypeScript co-located.
- Q: Should the existing `mouse_move` and `mouse_click` tools be marked `standalone = true` (preserve today's desktop display) or `standalone = false` (signal them as future-toolset members, hiding them from desktop until toolset UI ships)? → A: `standalone = true`. Matches FR-011 as written; the desktop has no toolset UI today so hiding the only two existing tools would break every profile that uses them.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Discover Agent Resources by Category (Priority: P1)

A developer maintaining the game agent opens the agent source tree and finds three clearly separated top-level resource categories — `mcp/`, `tools/`, and `skill/` — each with a README explaining how files inside it must be laid out and named. The developer can tell at a glance where to add a new MCP integration, a new built-in skill, or a new tool without reading any framework wiring code.

**Why this priority**: A discoverable, self-documenting directory layout is the foundation for every later addition (new tools, new MCPs, new built-in skills). Without it, every future resource lands in the wrong place and the README contract that the desktop and prompt services assume cannot be enforced.

**Independent Test**: Can be fully tested by listing the agent source tree, confirming the three top-level resource directories exist, each carries a README, and the README text describes only the file-format conventions a contributor must follow for that category.

**Acceptance Scenarios**:

1. **Given** the agent source tree, **When** a developer opens `projects/game/agent/src/`, **Then** three sibling directories `mcp/`, `tools/`, and `skill/` are visible at the same level under `src/`.
2. **Given** any of the three resource directories, **When** the developer opens its README, **Then** the README documents the per-resource folder naming convention (`{category}/{resource_name}/{file}`) and the file each resource must expose.
3. **Given** the `skill/` directory, **When** the developer reads its README, **Then** the README mandates that each built-in skill is stored as a `SKILL.md` file under its own `skill/{skill_name}/` folder, matching the common SKILL.md convention used across agentic tools.
4. **Given** the `mcp/` and `skill/` READMEs, **When** the developer reads either, **Then** the README contains only file-format conventions (folder naming, required files, file naming), with no framework-integration or runtime content.

---

### User Story 2 - Existing Tools Relocate Without Behavior Change (Priority: P1)

A developer finds the existing mouse tools (`mouse_move`, `mouse_click`) already living under the new `tools/` directory layout. All existing imports, unit tests, large tests, and runtime behavior continue to work identically after the move; only the file paths change.

**Why this priority**: The relocation must be behavior-preserving or the agent stops dispatching mouse operations. This is the single riskiest piece of the refactor and must be verifiable in isolation.

**Independent Test**: Can be fully tested by running the existing tool unit tests (`mouse-tool.test.ts`, `llm-tools.test.ts`) and the agent test suite at their new locations, plus the existing testplan large test, and confirming identical pass/fail outcomes to the pre-refactor baseline.

**Acceptance Scenarios**:

1. **Given** the pre-refactor mouse tool source file, **When** the move is complete, **Then** every existing import of `mouse_move` / `mouse_click` resolves to the new location without code edits beyond the import paths.
2. **Given** the relocated tool source and its colocated test file, **When** the unit tests run, **Then** all pre-refactor test cases still pass with no test-logic edits.
3. **Given** the full agent build, **When** the existing testplan large test runs against the refactored image, **Then** mouse dispatch and ToolResult handling behave identically to the pre-refactor baseline.

---

### User Story 3 - Built-in Skills Follow the SKILL.md Convention (Priority: P2)

A developer adding a built-in skill creates a new folder `skill/{skill_name}/` containing a single `SKILL.md` file whose frontmatter and body match the common SKILL.md convention already used by other agentic tools in the ecosystem. The skill/ README documents this contract so any contributor can produce a conforming file without asking for examples.

**Why this priority**: Built-in skills are a new resource type; without a documented file-format contract the first addition will set a poor precedent that later skills copy. Note: this feature covers only the directory contract and README — it explicitly excludes user-created skills (which are managed through the `Skill` resource in `projects/game/game.proto:438`).

**Independent Test**: Can be tested by reading the `skill/` README and verifying it specifies the `SKILL.md` filename, the per-skill folder layout, and the file-format fields a built-in skill must carry.

**Acceptance Scenarios**:

1. **Given** the `skill/` README, **When** a developer reads it, **Then** the README mandates one `SKILL.md` file per `skill/{skill_name}/` folder and documents the file-format fields (e.g. frontmatter name/description and body sections).
2. **Given** the scope of this feature, **When** the developer reviews what was delivered, **Then** only the `skill/` directory, its README, and the file-format contract exist — no built-in skill is authored in this feature and no framework code is added.
3. **Given** the user-created skill path (`Skill` resource via `PromptService` in `projects/game/game.proto:140-167`), **When** the developer reviews this feature's deliverables, **Then** none of them touch the user-created skill interfaces (the in-scope scope is built-in skills only).

---

### User Story 4 - Tools Carry a Standalone Visibility Attribute (Priority: P1)

A developer defining or maintaining a tool sets a `standalone` attribute on the tool's definition. Tools with `standalone = true` are individually selectable on the desktop's agent-profile configuration page; tools with `standalone = false` are hidden from that page because they are intended to be used only as members of a future tool collection. The attribute is part of the tool definition itself, not a separate registry, so the visibility contract travels with the code that implements the tool.

**Why this priority**: The standalone attribute is the preparation step the user explicitly called out ("为将来以集合的方式使用 tools 准备"). Adding it now — before any collection mechanism exists — means future toolset work has the per-tool signal it needs without a second refactor of every existing tool.

**Independent Test**: Can be tested by reading each tool definition and asserting that it carries a `standalone` boolean, and that the two existing tools (`mouse_move`, `mouse_click`) carry `standalone = true` so the desktop's current visible-tool behavior is preserved.

**Acceptance Scenarios**:

1. **Given** any tool definition in the agent codebase after this feature, **When** a developer reads it, **Then** the definition carries a `standalone` boolean attribute.
2. **Given** the existing `mouse_move` and `mouse_click` tools, **When** the developer reads their definitions after this feature, **Then** both carry `standalone = true` so they remain individually selectable on the desktop profile page (preserving today's behavior).
3. **Given** a hypothetical future tool meant only for collection use, **When** the developer sets `standalone = false` on its definition, **Then** the desktop profile configuration page would not list that tool as an individually selectable option (the contract is in place even if the desktop consumer is delivered in a later feature).
4. **Given** a profile whose `tool_names` references any tool regardless of its `standalone` value, **When** the agent binds the profile, **Then** the tool is still instantiated and dispatched normally — `standalone` governs only desktop UI visibility, never runtime availability.

---

### Edge Cases

- What happens when a tool definition omits the `standalone` attribute? The contract must define a default so the agent and desktop agree; the safe default preserves today's behavior (treat as visible).
- What happens if a contributor adds a file under `mcp/{name}/` or `skill/{name}/` that does not match the README's file-format contract? The README is the source of truth; there is no automated enforcement in this feature, so the contract is review-enforced.
- What happens to the existing `mouse-tool.test.ts` and `llm-tools.test.ts` after the tools move? Their import paths must be updated; their assertions and test logic must not change.
- What happens if a `tool_names` entry on a profile names a `standalone = false` tool? The agent must still bind and dispatch it — `standalone` is purely a desktop-display signal, not a runtime permission.
- What happens to indirect imports (e.g. `buildTools` in `projects/game/agent/src/llm.ts:57` referencing `./mouse-tool`)? They must be updated as part of the relocation; this is in scope.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The agent source tree MUST contain three sibling resource directories — `src/mcp/`, `src/tools/`, and `src/skill/` — under `projects/game/agent/src/`.
- **FR-002**: The `mcp/`, `tools/`, and `skill/` directories MUST each carry a README that documents the per-resource folder naming convention (`{category}/{resource_name}/{file}`).
- **FR-003**: The `skill/` README MUST mandate that every built-in skill is stored as exactly one `SKILL.md` file inside its own `skill/{skill_name}/` folder.
- **FR-004**: The `skill/` README MUST specify the file-format fields a built-in skill's `SKILL.md` must carry (frontmatter and body structure) per the common SKILL.md convention used by agentic tools.
- **FR-005**: The `mcp/` and `skill/` READMEs MUST be limited to file-format conventions only — folder naming, required files, file naming — and MUST NOT include framework-integration, runtime, or wiring content.
- **FR-006**: The `tools/` README MUST document the per-tool folder convention and any file a tool must expose so a contributor can add a new tool by following the README alone.
- **FR-007**: The existing mouse tool source (`mouse_move`, `mouse_click` factories and their shared helper) MUST be relocated under `src/tools/` following the same `{tool_name}/` convention.
- **FR-008**: The relocation of the mouse tools MUST preserve all existing behavior: imports resolve, unit tests pass, the large test passes, and `buildTools` in `projects/game/agent/src/llm.ts` continues to map profile `tool_names` entries to the relocated tool factories.
- **FR-009**: Every tool definition in the agent MUST carry a `standalone` boolean attribute that signals whether the tool is individually selectable on the desktop profile configuration page.
- **FR-010**: The `standalone` attribute MUST default to a value that preserves today's desktop behavior when omitted, so introducing the attribute does not silently hide existing tools.
- **FR-011**: The existing `mouse_move` and `mouse_click` tools MUST be marked `standalone = true` so they remain individually selectable on the desktop profile configuration page after this feature ships.
- **FR-012**: The `standalone` attribute MUST affect only desktop UI visibility; the agent MUST instantiate and dispatch any tool referenced by a profile's `tool_names` regardless of the tool's `standalone` value.
- **FR-013**: User-created skills (managed via the `Skill` resource in `projects/game/game.proto:438-454` and the `PromptService` RPCs at `projects/game/game.proto:140-167`) MUST be out of scope — this feature touches neither their interface nor their storage.

### Key Entities *(include if feature involves data)*

- **Built-in Skill**: A skill authored as a `SKILL.md` file inside the agent's `src/skill/{skill_name}/` folder. Distinct from user-created skills, which are stored via the `Skill` proto resource at runtime and are out of scope here. Reference: `projects/game/game.proto:438`.
- **Tool Definition**: The code unit that implements one LangChain tool (e.g. `mouse_move`, `mouse_click`). After this feature it carries a `standalone` attribute and lives under `src/tools/{tool_name}/`. The mapping from profile `tool_names` to definitions happens in `buildTools` at `projects/game/agent/src/llm.ts:57`.
- **MCP Resource**: A Model Context Protocol integration authored as files inside `src/mcp/{mcp_name}/`. No MCP exists today; this feature only creates the directory and its file-format README.
- **`standalone` attribute**: A boolean on a tool definition that signals whether the tool is individually selectable on the desktop profile configuration page (`true`) or hidden because it is intended for collection-only use (`false`). The desktop may consume this attribute in a later feature; this feature only establishes the contract on the agent side.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A contributor can add a new tool, MCP, or built-in skill by reading only the relevant category README — no need to read framework wiring or ask an existing maintainer.
- **SC-002**: The mouse tool relocation produces zero changes to test logic — every pre-refactor unit test and the existing large test pass at their new file paths with assertions untouched.
- **SC-003**: Every tool definition in the agent codebase after this feature exposes a `standalone` boolean, verifiable by a single grep over the tools directory.
- **SC-004**: The two existing tools (`mouse_move`, `mouse_click`) remain individually selectable on the desktop profile configuration page after this feature — no regression in what the desktop offers today.
- **SC-005**: The `mcp/` and `skill/` READMEs contain zero references to framework integration, runtime wiring, RPC plumbing, or any agent-internal module — they describe only the file-format contract for their category.

## Assumptions

- The scope of this feature is **the agent project only** (`projects/game/agent/`). The desktop consumer of the `standalone` attribute is described here to define the *semantics* of the attribute (what `true`/`false` will mean once consumed); the actual desktop-side filtering of `TOOL_OPTIONS` in `projects/game/desktop/frontend/src/components/ProfileManagement.svelte:21` is a separate follow-up feature. This feature is the preparation step the user called out ("为将来以集合的方式使用 tools 准备").
- The default value of `standalone` when a tool omits the attribute is `true` (preserve today's visible-tool behavior). This default is documented in the tools README and applies only if a future tool forgets to set the attribute.
- The exact internal granularity of the `tools/{tool_name}/` folders (e.g. whether the two tightly-coupled mouse tools share a helper module or each carry a private copy) is an implementation decision deferred to `plan.md`; the spec only fixes the per-tool-name folder contract.
- The "common SKILL.md convention" referenced by the user means the well-known file format used by agentic tools across the ecosystem: a YAML frontmatter block (with at minimum `name` and `description`) followed by a markdown body. The exact field set is pinned in `plan.md` after surveying the conventions already used in this repo (e.g. `tools/test/guitar`'s SKILL, `.opencode/skills/testplan/SKILL.md`) and external references.
- The feature delivers directories + READMEs for `mcp/` and `skill/` only — no actual MCP integration and no actual built-in skill is authored here.
- Existing test files (`mouse-tool.test.ts`, `llm-tools.test.ts`) move with their source files; their assertions and test logic are not edited.
- The `BUILD.bazel` files are regenerated via `bazel run //:gazelle` after the move (per `AGENTS.md`); this is part of the implementation, not a separate concern.
