<!--
Sync Impact Report
=====================================================================
Version change: 2.0.0 → 2.1.0

Modified principles:
  - None (existing §I, §II, §III, §IV unchanged)

Added principles:
  - V. Interface Design Coverage (接口设计覆盖): every solution design
    in `plan.md` that introduces or modifies an externally callable
    boundary MUST include an explicit interface design covering all
    interface types touched (including but not limited to gRPC and
    HTTP/REST), and the design MUST comply with the repository's
    interface specification under `style/api.md`. The design MUST be
    materialized in the feature's `contracts/` artifact and inherited
    by implementation tasks.

Added sections:
  - Core Principles → V. Interface Design Coverage

Removed sections:
  - None

Templates requiring updates:
  - .specify/templates/plan-template.md — ✅ updated (Constitution Check
    now references §V interface-design + style/api.md compliance gate)
  - .specify/templates/tasks-template.md — ✅ updated (Constitution Check
    now references §V inheritance from plan.md / contracts/)
  - .specify/templates/spec-template.md — ✅ verified (spec authoring
    precedes planning; §V applies at plan/tasks stage, no change needed)
  - .specify/templates/checklist-template.md — N/A (operational artifact)
  - .specify/templates/commands/*.md — N/A (directory does not exist)

Follow-up TODOs: none
======================================================================
Sync Impact Report
=====================================================================
Version change: 1.4.0 → 2.0.0

Modified principles:
  - V. Refactoring-Oriented Changes → renumbered to IV. Refactoring-
    Oriented Changes (no content change beyond cross-reference updates)

Added principles:
  - None

Removed sections:
  - Core Principles → IV. Implementation Checkpointing (实现检查点插入):
    the principle mandating check-task insertion at phase boundaries in
    `tasks.md`. Removed entirely; check-task insertion is no longer
    constitutionally mandated.

Templates requiring updates:
  - .specify/templates/tasks-template.md — ✅ updated (Constitution Check
    §IV bullet removed; sample `> §IV check task` callouts and `CHECK`
    task examples removed; §V references renumbered to §IV; closing
    "Check tasks are §IV checkpoints" note removed)
  - .specify/templates/plan-template.md — ✅ updated (Constitution Check
    §IV bullet removed; §V reference renumbered to §IV)
  - .specify/templates/spec-template.md — ✅ verified (spec authoring
    precedes planning; §IV applies at tasks.md stage, no change needed)
  - .specify/templates/checklist-template.md — N/A (operational artifact)
  - .specify/templates/commands/*.md — N/A (directory does not exist)

Follow-up TODOs: none
===================================================================

Version change: 1.3.0 → 1.4.0

Modified principles:
  - None (existing §I, §II, §III, §IV unchanged)

Added principles:
  - V. Refactoring-Oriented Changes (重构式变更): every change in
    `plan.md`/`tasks.md` MUST be classified as 新增 / 修改 / 删除
    (Add / Modify / Delete), where 新增 applies ONLY to modules, files,
    types, or design elements that did not previously exist (adding a
    function to an existing class is 修改, not 新增). 修改 MUST be done
    as a refactor, not logic stacking. 修改 and 删除 MUST review the
    existing design and layering and explicitly verdict whether it still
    serves the new goal; outdated designs MUST be updated in the same
    change. "Out of scope" MUST NOT carry stale designs forward.

Added sections:
  - Core Principles → V. Refactoring-Oriented Changes

Removed sections:
  - None

Templates requiring updates:
  - .specify/templates/plan-template.md — ✅ updated (Constitution Check
    now references §V classification + design-review obligation)
  - .specify/templates/tasks-template.md — ✅ updated (Constitution Check
    now references §V; Format header extended to carry the A/M/D label)
  - .specify/templates/spec-template.md — ✅ verified (spec authoring
    precedes planning; §V applies at plan/tasks stage, no change needed)
  - .specify/templates/checklist-template.md — N/A (operational artifact)
  - .specify/templates/commands/*.md — N/A (directory does not exist)

Follow-up TODOs: none
====================================================================

Version change: 1.2.0 → 1.3.0

Modified principles:
  - None (existing §I, §II, §III unchanged)

Added principles:
  - IV. Implementation Checkpointing (实现检查点插入): when tasks.md
    contains many tasks, check tasks MUST be inserted at appropriate
    positions to verify the implementation has not deviated from the
    task plan and plan.md, and that code is correctly committed.
    Deviations MUST be fixed promptly.

Added sections:
  - Core Principles → IV. Implementation Checkpointing

Removed sections:
  - None

Templates requiring updates:
  - .specify/templates/tasks-template.md — ✅ updated (Constitution Check
    now references §IV; check task examples inserted at phase checkpoints)
  - .specify/templates/plan-template.md — ✅ updated (Constitution Check
    now references §IV checkpointing obligation)
  - .specify/templates/spec-template.md — ✅ verified (spec authoring
    precedes task planning; §IV applies at tasks.md stage, no change needed)
  - .specify/templates/checklist-template.md — N/A (operational artifact)
  - .specify/templates/commands/*.md — N/A (directory does not exist)

Follow-up TODOs: none
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

### IV. Refactoring-Oriented Changes (重构式变更)

Every code change described in `plan.md` and `tasks.md` MUST be expressed
as a refactor of the affected unit, not as logic stacked on top of it.
Each change MUST be explicitly classified as 新增 (Add), 修改 (Modify),
or 删除 (Delete), and the plan MUST keep the design and the
implementation coherent — outdated designs MUST NOT survive into the new
version under the excuse of "out of scope".

**Mandatory rules**:

- **Change classification**: every change recorded in `plan.md` and every
  implementation task in `tasks.md` MUST be labeled as one of 新增 /
  修改 / 删除 (Add / Modify / Delete). The label MUST describe what is
  happening to the unit of code being touched.
- **Classification accuracy**: 新增 applies ONLY to a module, file,
  type, or design element that did not previously exist. Adding a
  function to an existing class, a field to an existing struct, a method
  to an existing interface, or a branch to an existing function is 修改
  — not 新增 — because the enclosing unit already existed. 删除 applies
  when an existing module, file, type, function, field, or design
  element is being removed.
- **Refactor-not-stack rule**: changes classified as 修改 MUST be
  carried out by refactoring the existing unit so the new behavior is a
  natural extension of a still-coherent design. Appending logic onto an
  existing unit without revisiting its structure — so the unit accrues
  conditional branches, parallel code paths, or responsibilities it was
  never designed for — is a violation, even when the new behavior is
  correct.
- **Design review for 修改 and 删除**: every 修改 or 删除 change MUST be
  accompanied in `plan.md` by a review of (a) the existing design,
  architecture, and code layering of the affected unit and (b) an
  explicit verdict on whether that design still serves the new goal.
  "The existing design still applies" is an acceptable verdict when
  true; when it does not, the change MUST be expanded to bring the
  design back into coherence with the new goal.
- **Synchronous design update**: when a 修改 or 删除 change reveals that
  the surrounding design, architecture, layering, or documentation is
  outdated or no longer applicable, the change MUST be expanded to
  update those elements in the same version. Carrying a stale design
  into a new version on the grounds that fixing it is "out of scope" or
  "belongs to a separate task" is a violation.
- **Design-implementation coherence is part of the change**: the change
  is not complete until the design (in `spec.md` / `plan.md` /
  `data-model.md` / `contracts/` / `style/` as applicable) and the
  implementation agree. A divergence between design and implementation
  knowingly left in place counts as an incomplete change.

**Rationale**: stacking logic onto existing units without refactoring
them is how codebases accrue accidental complexity — a class gains a
third responsibility, a function gains a seventh branch, a layer gains a
third caller pattern, and the original design silently stops describing
the code. Forcing every change to be classified and to revisit the
affected design makes that drift visible at planning time, when it is
cheapest to fix. Requiring synchronous updates keeps the design and the
code as one artifact across versions, instead of letting them diverge
until a future "refactor pass" that never arrives. This complements §II
(style), which governs how code is written, and §III (dependency
research), which governs what is chosen; this principle governs how the
plan describes change itself.

### V. Interface Design Coverage (接口设计覆盖)

Every solution design in `plan.md` that introduces or modifies an
externally callable boundary MUST include an explicit interface design
covering all interface types the change touches (including but not
limited to gRPC and HTTP/REST). The interface design MUST comply with
the repository's interface specification under `style/api.md`.

**Mandatory rules**:

- **Coverage rule**: when a change in `plan.md` introduces or modifies
  any service, module, or component that exposes an external boundary
  (RPC service, HTTP endpoint, message subscriber, event producer, etc.),
  the plan MUST enumerate every interface surface it adds or changes —
  for each surface, the protocol (e.g., gRPC, HTTP/REST), the service
  and method names, the request/response (or resource/message) shapes,
  and the error codes. A change that crosses an external boundary but
  records no interface design is a violation.
- **Compliance with repo interface spec**: every interface designed in
  `plan.md` MUST comply with `style/api.md`. The constitution does NOT
  restate the rules of `style/api.md` here — `style/api.md` is the
  single source of truth for interface conventions, and the plan MUST
  conform to whatever it currently requires. Conflicts between a plan's
  proposed interface and `style/api.md` MUST be resolved in favor of the
  repo spec unless the constitution itself is amended.
- **Style gate in plan**: the plan MUST reference `style/api.md`
  (inline, per §I) and confirm it was reviewed before any interface
  design is recorded. This gate parallels §II for code style but is
  enforced at planning time, before code is written.
- **Materialization in contracts**: the interface design MUST be
  materialized in the feature's `contracts/` artifact (e.g., `.proto`
  files, OpenAPI specs) as part of the plan's design output, so that
  downstream implementation tasks reference a concrete contract rather
  than prose.
- **Inheritance to tasks**: implementation tasks in `tasks.md` that
  touch an interface MUST inherit the design from `plan.md` and
  reference the corresponding `contracts/` source, rather than
  restating or inventing interface shapes at implementation time.

**Rationale**: an interface is a public contract — once callers depend
on it, fixing inconsistencies is far more expensive than designing it
correctly up front. Forcing every interface-affecting change to carry
an explicit, repo-spec-compliant design at planning time prevents
divergent APIs, undocumented endpoints, and the silent drift between a
service's declared contract and its implementation. This principle
complements §II (code style) and §IV (refactoring discipline) by
governing how interfaces are described before any code is written, and
complements §III (external dependency research) when the design imports
upstream API patterns.

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
  sources consulted. Every change in `plan.md` and every implementation
  task in `tasks.md` MUST be classified as 新增 / 修改 / 删除 per §IV,
  with 修改 changes implemented as refactors and every 修改 / 删除
  change carrying an explicit verdict on whether the existing design and
  layering still serve the new goal; outdated designs MUST be updated
  within the same change, never deferred as "out of scope". Every change
  in `plan.md` that introduces or modifies an externally callable boundary
  MUST include an explicit interface design per §V covering all interface
  types touched, MUST comply with `style/api.md` (cited inline per §I),
  MUST be materialized in the feature's `contracts/` artifact, and MUST be
  inherited by the corresponding implementation tasks in `tasks.md`.

**Version**: 2.1.0 | **Ratified**: 2026-06-18 | **Last Amended**: 2026-07-14
