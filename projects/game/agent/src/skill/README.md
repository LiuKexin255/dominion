# skill/

This directory holds built-in skills for the game agent. Each built-in skill
is a single `SKILL.md` file under its own folder.

This README covers **built-in skills only**. User-created skills (the `Skill`
resource managed at runtime, distinct from these files) are out of scope here.

## File-format contract

* One folder per built-in skill, named `{skill_name}` in lowercase hyphenated
  form (e.g. `src/skill/my-skill/`).
* The folder name MUST equal the SKILL.md frontmatter `name` field verbatim.
* The skill itself is exactly one file per folder: `SKILL.md` (uppercase). Do
  NOT use `skill.md`, `Skill.md`, or `{skill_name}.md`.
* Optional sibling files (e.g. `references/`, `scripts/`, `assets/`) may live
  alongside `SKILL.md` inside `{skill_name}/` when a skill needs more detail;
  `SKILL.md` is always the entry point.

## SKILL.md frontmatter contract

YAML frontmatter block delimited by `---` lines. Adopted convention: the
agentskills.io open standard
(https://agentskills.io/specification), with the OpenCode-recognized subset
(https://opencode.ai/docs/skills/) as the minimum portable surface.

### Required fields

| Field         | Type   | Constraint                                                                                          |
| ------------- | ------ | --------------------------------------------------------------------------------------------------- |
| `name`        | string | 1–64 chars; regex `^[a-z0-9]+(-[a-z0-9]+)*$`; MUST equal the parent `{skill_name}` folder name.     |
| `description` | string | 1–1024 chars; non-empty; states what the skill does AND when to use it.                             |

### Recommended-optional fields (portable across OpenCode, Claude Code, claude.ai)

| Field           | Type                 | Constraint / Notes                                                                                       |
| --------------- | -------------------- | -------------------------------------------------------------------------------------------------------- |
| `license`       | string               | SPDX-style.                                                                                              |
| `compatibility` | string               | 1–500 chars. Environment requirement. Repo convention: `opencode`.                                       |
| `metadata`      | map<string, string>  | Free-form string-to-string map. Repo convention sub-keys: `audience: dominion`, `scope: <area>`.        |

### Non-portable fields (MAY be omitted; documented as non-portable)

The following fields are NOT recognized by OpenCode and are NOT portable
across agent runtimes. Dominion built-in skills SHOULD NOT set them:

* `allowed-tools` — marked experimental in the agentskills.io spec; ignored by
  OpenCode; only consumed by Claude Code CLI.
* All Claude Code extension fields: `when_to_use`, `argument-hint`,
  `arguments`, `disable-model-invocation`, `user-invocable`,
  `disallowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`,
  `paths`, `shell`.

## Body contract

* Format: free-form Markdown.
* No mandatory section structure.
* Recommended sections: purpose, when-to-use, how-to-use, examples, edge cases.
* Soft size limit: keep `SKILL.md` under 500 lines / ~5000 tokens. Move detail
  to sibling files under `{skill_name}/` rather than bloating the entry point.

## Authoritative contract

The full SKILL.md format contract lives at
`specs/020-agent-resources-layout/contracts/skill-md-format.md`. The upstream
open standard is at https://agentskills.io/specification. This README is
intentionally limited to file-format conventions; it deliberately omits
framework integration, runtime, or wiring content.
