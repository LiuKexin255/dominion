# Quickstart: Agent Resources Layout

**Feature**: 020-agent-resources-layout
**Date**: 2026-07-18
**Status**: Phase 1 validation guide — run these scenarios to prove the feature works end-to-end.

This guide tells you HOW to validate the feature once implementation is complete (post-`/speckit.tasks`). It is not implementation code; implementation belongs in `tasks.md`.

## Prerequisites

- A clean checkout of branch `020-agent-resources-layout`.
- Bazel, gazelle, pnpm available (per `AGENTS.md`).
- For the large-test step: the `testplan` skill (loaded into the agent session) and access to the deploy environment.

## Reference contracts

- Directory contract: `specs/020-agent-resources-layout/contracts/directory-layout.md`
- Tool Definition contract: `specs/020-agent-resources-layout/contracts/tool-definition.md`
- SKILL.md format contract: `specs/020-agent-resources-layout/contracts/skill-md-format.md`
- Data model: `specs/020-agent-resources-layout/data-model.md`

## Validation scenarios

### Scenario 1 — Directory layout exists (spec US1 / FR-001, FR-002)

**Steps:**
```bash
ls projects/game/agent/src/mcp/README.md
ls projects/game/agent/src/skill/README.md
ls projects/game/agent/src/tools/README.md
ls projects/game/agent/src/tools/types.ts
ls projects/game/agent/src/tools/shared/
ls projects/game/agent/src/tools/mouse_move/mouse-move.ts
ls projects/game/agent/src/tools/mouse_click/mouse-click.ts
```

**Expected:** every `ls` succeeds; no "No such file or directory".

### Scenario 2 — READMEs are file-format-only for `mcp/` and `skill/` (spec FR-005, SC-005)

**Steps:** grep the two READMEs for forbidden framework references.

```bash
# Should produce ZERO matches in each file:
rg -i 'langchain|createAgent|buildTools|AgentProfile|PromptService|grpc|proto|GameService' projects/game/agent/src/mcp/README.md
rg -i 'langchain|createAgent|buildTools|AgentProfile|PromptService|grpc|proto|GameService' projects/game/agent/src/skill/README.md
```

**Expected:** both commands produce no output.

### Scenario 3 — `skill/` README pins the SKILL.md contract (spec FR-003, FR-004)

**Steps:**
```bash
# Confirm the README references the agentskills.io spec or the contract file
rg -i 'SKILL\.md|skill_name|frontmatter|name:|description:' projects/game/agent/src/skill/README.md
```

**Expected:** matches mentioning the fixed `SKILL.md` filename, the per-skill `{skill_name}/` folder layout, and the required frontmatter fields `name` and `description`. Cross-reference: `specs/020-agent-resources-layout/contracts/skill-md-format.md`.

### Scenario 4 — Mouse tools relocated and behavior preserved (spec US2 / FR-007, FR-008, SC-002)

**Steps:**
```bash
# Confirm the old files are GONE
test ! -e projects/game/agent/src/mouse-tool.ts
test ! -e projects/game/agent/src/mouse-tool.test.ts

# Confirm the new files exist
ls projects/game/agent/src/tools/mouse_move/mouse-move.ts
ls projects/game/agent/src/tools/mouse_move/mouse-move.test.ts
ls projects/game/agent/src/tools/mouse_click/mouse-click.ts
ls projects/game/agent/src/tools/mouse_click/mouse-click.test.ts
ls projects/game/agent/src/tools/shared/result-blocks.ts
ls projects/game/agent/src/tools/shared/result-blocks.test.ts

# Confirm the old single-file import in llm.ts has been replaced
rg 'from "./mouse-tool"' projects/game/agent/src/llm.ts            # should produce no matches
rg 'from "./tools/mouse_move/mouse-move"' projects/game/agent/src/llm.ts
rg 'from "./tools/mouse_click/mouse-click"' projects/game/agent/src/llm.ts
```

**Expected:** the `test ! -e` lines return success (old files gone); the `ls` lines succeed; the first `rg` produces no matches; the last two `rg` lines each produce one match.

### Scenario 5 — Every tool carries `extras.standalone` (spec US4 / FR-009, FR-011, SC-003)

**Steps:**
```bash
# Every tool file should set extras.standalone explicitly:
rg 'standalone:' projects/game/agent/src/tools/
```

**Expected:** at minimum two matches — one in `mouse-move.ts` and one in `mouse-click.ts` — each setting `standalone: true` explicitly. Cross-reference: `specs/020-agent-resources-layout/contracts/tool-definition.md`.

### Scenario 6 — Build + unit tests pass (spec FR-008, SC-002)

**Steps:**
```bash
bazel build //projects/game/agent/...
bazel test //projects/game/agent/...
```

**Expected:** both commands succeed. The test suite at the new file locations must pass with assertions unchanged from the pre-refactor baseline (spec Assumption).

The relevant unit tests:
- `//projects/game/agent:lib_test` — includes the relocated `mouse-move.test.ts`, `mouse-click.test.ts`, `result-blocks.test.ts`, the new `types.test.ts`, and the unchanged `llm-tools.test.ts`.

### Scenario 7 — Large-test acceptance gate (spec US2 acceptance #3, Constitution Principle VI)

This is the end-to-end acceptance gate for the relocation. The existing testplan at `projects/game/testplan/` exercises mouse dispatch through the full stack (desktop ↔ gateway ↔ agent ↔ LLM). It MUST pass identically to the pre-refactor baseline.

**Steps:** load the `testplan` skill and run the existing mouse-dispatch testplan.

```
load skill testplan
# then follow the skill's instructions to run the mouse-dispatch testplan
# (the testplan file path and guitar invocation are documented in the skill)
```

**Prerequisite reading before running:** `style/large_test.md` (per `AGENTS.md` and Constitution Principle V).

**Expected:** all testplan cases pass; mouse dispatch and ToolResult handling behave identically to the pre-refactor baseline. No regression in profile binding, frame exchange, or operation execution.

## Out of scope for this quickstart

- Desktop-side validation of `standalone` filtering — the desktop is not updated in this feature (spec Assumption).
- Any new RPC or service endpoint — none is added.
- Authoring an actual built-in skill or MCP integration — none is authored here; only the directories + READMEs.

## Done criteria

The feature is accepted when **all seven scenarios above pass**. Scenarios 1–5 are mechanical file-shape checks; Scenario 6 is the small-test gate (Constitution Principle IV); Scenario 7 is the large-test acceptance gate (Constitution Principle VI).
