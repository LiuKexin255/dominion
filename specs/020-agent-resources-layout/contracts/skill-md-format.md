# Contract: SKILL.md File Format

**Feature**: 020-agent-resources-layout
**Date**: 2026-07-18
**Status**: Phase 1 contract — MUST be satisfied by the `skill/` README and by every built-in skill authored in future features.

This contract pins the file-format that every built-in skill (`src/skill/{skill_name}/SKILL.md`) MUST follow. It is the authoritative reference for the `skill/README.md` and for any future skill-authoring task.

## Authority

- Spec: `specs/020-agent-resources-layout/spec.md` FR-003, FR-004, FR-005.
- Research: `specs/020-agent-resources-layout/research.md` §1 (SKILL.md convention — agentskills.io open standard).
- Constitution Principle III (Interface-First Design): this contract is settled BEFORE the `skill/` README is authored.

## Adopted convention

The **agentskills.io open standard** (https://agentskills.io/specification), with the **OpenCode-recognized subset** (https://opencode.ai/docs/skills/) enforced as the minimum portable surface.

The Dominion repo's existing skills already conform to this convention:
- `.opencode/skills/signoz/SKILL.md`
- `.opencode/skills/testplan/SKILL.md`

## File rules

1. Filename is **fixed**: `SKILL.md` (uppercase). Inside `src/skill/{skill_name}/SKILL.md`.
2. One `SKILL.md` per `{skill_name}/` folder.
3. Folder name `{skill_name}` === frontmatter `name` verbatim.

## Frontmatter contract

YAML frontmatter block delimited by `---` lines.

### Required fields

| Field | Type | Constraint | Source |
|---|---|---|---|
| `name` | string | 1–64 chars; regex `^[a-z0-9]+(-[a-z0-9]+)**$`; MUST equal the parent `{skill_name}` folder name | agentskills.io spec §name |
| `description` | string | 1–1024 chars; non-empty; states what the skill does AND when to use it | agentskills.io spec §description |

### Recommended-optional fields (portable across OpenCode, Claude Code, claude.ai)

| Field | Type | Constraint | Notes |
|---|---|---|---|
| `license` | string | SPDX-style | Optional |
| `compatibility` | string | 1–500 chars | Environment requirement. Repo convention: `"opencode"`. |
| `metadata` | map<string,string> | String-to-string map | Free-form. Repo convention: `audience: dominion`, `scope: <area>`. |

### Non-portable fields (MUST be documented in `skill/README.md` as non-portable; MAY be omitted)

- `allowed-tools` — marked **experimental** in the agentskills.io spec; **ignored by OpenCode**; only consumed by Claude Code CLI. Dominion skills SHOULD NOT set this field (it would silently no-op on the Dominion runtime).
- All Claude Code extension fields: `when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `disallowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`, `paths`, `shell`. These are non-portable; Dominion skills MUST NOT set them.

### Example (minimum viable)

```yaml
---
name: my-skill
description: This skill should be used when [trigger]. It does [what].
---
```

### Example (full recommended set, matching repo convention)

```yaml
---
name: my-skill
description: This skill should be used when [trigger]. It does [what].
compatibility: opencode
metadata:
  audience: dominion
  scope: <area>
license: MIT
---
```

## Body contract

- Format: free-form markdown.
- No mandatory section structure.
- Recommended sections (from agentskills.io spec): purpose, when-to-use, how-to-use, examples, edge cases.
- Soft size limit: <500 lines / <5000 tokens. Move detail to sibling files under `src/skill/{skill_name}/` (e.g. `references/`, `scripts/`, `assets/`) — those sibling files are NOT contracted by this feature; future features may add them.
- Repo convention (observed at `.opencode/skills/testplan/SKILL.md`): bilingual (Chinese + English) is acceptable. Body's `#`-heading MAY repeat the frontmatter `name` (the existing skills do this).

## Validation

No automated validator ships with this feature. The `skill/README.md` is the source of truth; conformance is review-enforced (per spec edge case "What happens if a contributor adds a file under `skill/{name}/` that does not match the README's file-format contract?").

The upstream `skills-ref` validator (https://github.com/agentskills/agentskills/tree/main/skills-ref) exists and MAY be added in a future feature; not required here.

## Out of scope for this contract

- Authoring any actual built-in skill (this feature ships only the directory + README).
- User-created skills — those are the `Skill` proto resource at `projects/game/game.proto:438-454`, managed via `PromptService` RPCs at `projects/game/game.proto:140-167`. This feature MUST NOT touch that surface (spec FR-013).
- A discovery mechanism that loads `SKILL.md` files into the agent runtime. Future feature.
- A registry connecting built-in skills to `AgentProfile.skill_names` (which today references user-created skills only). Future feature.
