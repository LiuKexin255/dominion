<!--
Sync Impact Report
=====================================================================
Version change: 1.1.0 → 1.2.0

Modified principles:
  - None (existing §I and §II unchanged)

Added principles:
  - III. External Dependency Research (外部依赖研读): when authoring
    plan.md (and tasks that introduce new dependencies), every external
    dependency or component MUST be researched against its official
    documentation and source repository BEFORE the plan is finalized.

Added sections:
  - Core Principles → III. External Dependency Research

Removed sections:
  - None

Templates requiring updates:
  - .specify/templates/plan-template.md — ✅ updated (Constitution Check
    now references §III research-before-plan obligation)
  - .specify/templates/tasks-template.md — ✅ updated (Constitution Check
    now references §III inheritance for tasks introducing new dependencies)
  - .specify/templates/spec-template.md — ✅ verified (spec authoring
    precedes planning; §III applies at plan/tasks stage, no change needed)
  - .specify/templates/checklist-template.md — N/A (operational artifact)
  - .specify/templates/commands/*.md — N/A (directory does not exist)

Follow-up TODOs: none
=====================================================================

Version change: 1.0.0 → 1.1.0

Modified principles:
  - I. Citation Provenance (引用溯源): scope expanded from spec.md/plan.md
    to include tasks.md; added task-level provenance rule.

Added principles:
  - II. Code Style Precedence (代码规范优先): implementation tasks MUST read
    repository style guidelines before modifying code.

Added sections:
  - Core Principles → II. Code Style Precedence

Removed sections:
  - None

Templates requiring updates:
  - .specify/templates/spec-template.md — ✅ verified (References section already
    covers spec.md; no change required because tasks.md inherits from parent docs)
  - .specify/templates/plan-template.md — ✅ updated (Constitution Check now
    references tasks.md citation and style review requirements)
  - .specify/templates/tasks-template.md — ✅ updated (added Constitution Check
    gate and References section placeholder)
  - .specify/templates/checklist-template.md — N/A (operational artifact)
  - .specify/templates/commands/*.md — N/A (directory does not exist)

Follow-up TODOs: none
=====================================================================
-->

# Dominion Spec Constitution

This constitution governs the authoring and maintenance of specification,
planning, and implementation task artifacts produced by Spec Kit (`spec.md`,
`plan.md`, `tasks.md`, and related design documents). It does not duplicate
engineering conventions already covered in `AGENTS.md`, which retains supremacy
over runtime code and build practices.

## Core Principles

### I. Citation Provenance (引用溯源)

Every factual claim, design decision, API specification, or technical detail in
`spec.md`, `plan.md`, or `tasks.md` that originates from external material MUST
carry a traceable link to its source.

**Mandatory rules**:

- **Inline citation**: each referenced fact MUST use Markdown link syntax
  inline at the point of use — `[description](URL)`.
- **Acceptable source types**: official documentation, source code
  repositories (e.g., GitHub/GitLab commit, file, or issue URLs),
  published technical articles and blog posts, RFCs, and standards
  documents.
- **Version pinning**: when a citation depends on a specific version of
  a document, package, or revision, the version number or commit SHA
  MUST accompany the link so the referenced state is deterministic.
- **Public accessibility**: cited links MUST resolve to publicly
  reachable resources. Private or paywalled sources MUST be accompanied
  by a publicly accessible alternative or an archived snapshot (e.g.,
  `web.archive.org`).
- **Consolidated References section**: every `spec.md`, `plan.md`, and
  `tasks.md` MUST end with a `## References` section enumerating all cited URLs,
  grouped by category (Official Documentation / Repositories / Articles
  & RFCs). Documents that cite no external material MUST still include
  the section with a note stating "No external references."
- **Task-level provenance**: when a `tasks.md` item depends on an external
  library, tool, command, or documented pattern, the task description MUST
  include or link to the authoritative source. Inherited provenance from
  `spec.md` or `plan.md` MUST be referenced explicitly when the task restates
  a design decision.
- **No bare claims**: statements presented as external fact without a
  citation are treated as assumptions and MUST be relocated to the
  `## Assumptions` section of the artifact.

**Rationale**: traceable citations let reviewers verify design decisions
against authoritative sources, prevent hallucinated or outdated claims
from entering the spec pipeline, and give future maintainers a
deterministic path back to the original source of truth.

### II. Code Style Precedence (代码规范优先)

Every code-related task in `tasks.md` MUST reference the repository's code style
guidelines before any source file is created or modified.

**Mandatory rules**:

- **Read-first rule**: an implementation task MUST NOT begin until the assignee
  has read the style documents under `style/` (or the location designated by
  `AGENTS.md`) for the relevant language and project area.
- **Style gate in tasks**: every `tasks.md` implementation task that touches
  code MUST include an acceptance criterion or inline note confirming the
  relevant style guidelines were reviewed.
- **Conflict resolution**: if a task's proposed approach conflicts with an
  existing style rule, the style rule prevails unless the constitution itself
  is amended.
- **New conventions**: when a task introduces a pattern not covered by existing
  style guidelines, the assignee MUST document the new convention in the
  appropriate `style/` document or flag it for review before merging.

**Rationale**: reading style guidelines first prevents inconsistent formatting,
redundant conventions, and rework; it ensures the codebase evolves coherently
and that each contributor understands the project's engineering expectations
before writing code.

### III. External Dependency Research (外部依赖研读)

When authoring `plan.md`, every external dependency, library, framework,
service, or component referenced in the design MUST be researched against its
official documentation and source repository BEFORE the plan is finalized.

**Mandatory rules**:

- **Research-before-plan rule**: before a technical decision involving an
  external dependency or component is recorded in `plan.md`, the author MUST
  retrieve and read the dependency's official documentation and source
  repository (README, documentation site, API reference, CHANGELOG) to a depth
  sufficient to justify the decision. Relying on memory or prior assumptions
  about the dependency is a violation.
- **Scope of research**: research MUST cover, at minimum, the dependency's
  purpose, supported versions, the public API surface relevant to the plan,
  known constraints or deprecations, and licensing terms.
- **Evidence in plan**: the plan MUST record the documentation URLs consulted
  as inline citations (per §I) alongside a one-line summary of the finding
  that informed the decision. A bare dependency name without supporting
  documentation evidence is a violation.
- **Version grounding**: the specific version or version range researched MUST
  be pinned in the plan. If the latest released version differs from what the
  plan targets, the plan MUST note the delta and the reason for the choice.
- **Conflicting signals**: when official documentation and observed repository
  behavior diverge, the plan MUST flag the discrepancy and record which source
  was trusted and why.
- **Inheritance to tasks**: tasks in `tasks.md` that introduce NEW dependencies
  not already researched in `plan.md` MUST perform the same research and cite
  the findings before implementation begins.

**Rationale**: planning against stale memory or unverified assumptions about
external dependencies produces designs that break on implementation — APIs
change, versions deprecate, and constraints shift. Reading authoritative
sources before committing to a design ensures plans are grounded in current
reality, complements §I (which obligates citing sources) by obligating the
actual reading of them, and gives reviewers verifiable evidence rather than
trust.

## Spec Artifact Scope

This constitution applies to the following Spec Kit artifacts:

| Artifact | Citations Required |
|----------|-------------------|
| `spec.md` | YES — all external facts, requirement rationale, and domain context |
| `plan.md` | YES — all technical decisions, dependency choices, and design references |
| `research.md` | YES — inherently research-driven; every finding needs a source link |
| `data-model.md` | YES — schema designs referencing standards or upstream contracts |
| `contracts/` | WHEN APPLICABLE — cite the spec or RFC an API contract derives from |
| `tasks.md` | YES — task descriptions that cite external libraries, tools, commands, patterns, or inherited design decisions |
| `checklist.md` | NO — operational artifact, not a citation source |

## Governance

- **Supremacy**: within the scope of Spec Kit artifact authoring, this
  constitution supersedes ad-hoc practices. It does not override
  `AGENTS.md`, which governs runtime engineering conventions.
- **Amendment procedure**: any principle addition, removal, or
  redefinition MUST be recorded as a Sync Impact Report prepended to
  this file, accompanied by a semantic version increment and an ISO
  8601-dated amendment entry.
- **Versioning policy**: MAJOR for principle removals or incompatible
  redefinitions; MINOR for new principles or materially expanded
  guidance; PATCH for clarifications, wording, and typo fixes.
- **Compliance review**: the `/speckit.analyze` command and the
  plan-template "Constitution Check" gate MUST verify that `spec.md`,
  `plan.md`, and `tasks.md` contain a `## References` section when external
  material is cited, and that every inline claim has a matching link.
  Implementation tasks in `tasks.md` MUST reference the applicable `style/`
  documents and confirm they were reviewed before code changes begin.
  Every external dependency or component referenced in `plan.md` (and any
  new dependency introduced in `tasks.md`) MUST show evidence of
  documentation research per §III, with inline citations to the official
  sources consulted.

**Version**: 1.2.0 | **Ratified**: 2026-06-18 | **Last Amended**: 2026-06-21
