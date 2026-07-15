<!--
Sync Impact Report
==================
Constitution version: 3.1.0 → 3.2.0
Bump type: MINOR (materially expanded guidance in an existing principle)

Modified principles:
- §II. External Dependency Research — added a new mandatory rule,
  "Transitive reading of cited references", requiring the planner to
  follow and read references cited by a dependency's official docs /
  source repo (linked sub-pages, RFCs, AIPs, upstream design docs,
  source sections) to a depth sufficient to justify the design
  decision. Transitive reading is scoped to the PLAN/design stage; it
  complements §V, which requires the planner to distill that research
  into an explicit per-task reading list rather than pushing transitive
  reading onto task executors.

Added sections:
- None (new rule within existing §II).

Removed sections:
- None.

Templates requiring updates:
- ✅ .specify/templates/plan-template.md — updated §II Constitution
  Check entry to require transitive reading of cited references.
- ⚠ .specify/templates/tasks-template.md — §II "same research" language
  already inherits the new rule for new-dependency tasks; no further
  change required.
- ⚠ .specify/templates/spec-template.md — no Constitution Check gate;
  not affected.

Follow-up TODOs:
- None.
-->

<!--
Sync Impact Report
==================
Constitution version: 3.0.0 → 3.1.0
Bump type: MINOR (new principle added)

Modified principles:
- None renamed or redefined.

Added sections:
- §V. Documentation First (文档优先) — new principle requiring every
  implementation task in tasks.md to declare, as part of the task
  itself, the documents (classified as 规范文档 / 官方文档 / 技术文章,
  each of which may be in-repo or external) that MUST be read BEFORE
  any code in that task is edited. 规范文档 = code style/spec docs
  (mainly style/*) plus the external standards they cite. The feature's
  own design docs under specs/[###-feature]/ are declared once at the
  top of tasks.md as required reading for every task. Goal: ensure the
  executor builds full context before editing, not just the narrow
  slice the task touches.

Removed sections:
- None.

Templates requiring updates:
- ✅ .specify/templates/tasks-template.md — added §V Constitution Check
  entry; added Required Reading to task Format spec; added a
  "Required Spec Docs" section (feature design docs declared once).
- ✅ .specify/templates/plan-template.md — added §V Constitution Check
  entry (plan seeds the reading list inherited by tasks).
- ⚠ .specify/templates/spec-template.md — no change required; §V is a
  task-level principle and spec-template has no Constitution Check gate.
- ⚠ .specify/templates/checklist-template.md — not modified; operational
  artifact, no principle-driven change expected.

Follow-up TODOs:
- None. All placeholders resolved.
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

### II. External Dependency Research (外部依赖研读)

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
- **Transitive reading of cited references**: at the planning/design
  stage, research is NOT limited to the dependency's top-level
  documentation. When the official documentation or source repository
  cites other references — linked sub-pages, referenced RFCs or
  standards, related AIPs, upstream design docs, or relevant sections of
  the source code — the author MUST follow and read those references to
  a depth sufficient to justify the design decision. Transitive reading
  is expected at plan time because the planner is building the
  authoritative research record that everything downstream depends on;
  findings from followed references MUST be recorded as inline citations
  (per §I) and surface in the plan's `## References` section. This rule
  is the complement of §V: §II performs the transitive research ONCE at
  plan time, and §V requires the planner to distill that research into
  an explicit, enumerated per-task reading list rather than imposing an
  implicit "follow the links yourself" obligation on the task executor.
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
trust. Following cited references transitively is reasonable at plan time
because the planner has the context to judge which references matter and is
building the single research record the rest of the feature relies on; this
is precisely why §V forbids pushing that same transitive obligation onto
task executors and instead requires the planner to enumerate the distilled
reading list explicitly.

### III. Refactoring-Oriented Changes (重构式变更)

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
(dependency research), which governs what is chosen; this principle
governs how the plan describes change itself.

### IV. Interface Design Coverage (接口设计覆盖)

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
  design is recorded. This gate is enforced at planning time, before
  code is written.
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
complements §III (refactoring discipline) by governing how interfaces
are described before any code is written, and complements §II (external
dependency research) when the design imports upstream API patterns.

### V. Documentation First (文档优先)

Every implementation task in `tasks.md` MUST explicitly declare, as part
of the task itself, the documents that MUST be read BEFORE any code in
that task is edited. The declaration is mandatory — not optional, not
deferred to the executor's judgment. The goal is to ensure the task
executor understands the full context surrounding the change, not just
the narrow slice the task directly touches.

**Mandatory rules**:

- **Pre-read declaration**: every implementation task MUST carry a
  "Required Reading" declaration enumerating the specific documents
  the executor MUST read before editing code. A task without this
  declaration is a violation and MUST NOT be started.
- **Coverage scope**: the declaration MUST cover context broader than
  the task's direct target — at minimum the code style docs that govern
  the affected unit and the existing code or modules the change
  interacts with. Declaring only the single file being edited is a
  violation — the purpose is full context, not just the edit site.
- **Three document categories**: every declared document MUST be
  classified into one of three categories, and each category MUST appear
  in the declaration. Any category may contain both repository-internal
  documents and external links — the internal/external axis is identical
  across all three:
  - **规范文档 (Code style/spec docs)** — code conventions and
    standards documents, primarily files under `style/`. External
    standards referenced by these docs (e.g., AIPs, RFCs, language
    style guides) are listed in THIS category, not in 官方文档 or
    技术文章, because they are part of understanding the governing code
    convention.
  - **官方文档 (Official docs)** — official documentation of external
    dependencies, components, frameworks, tools, or services the task
    touches, plus the README of any upstream codebase relied upon.
  - **技术文章 (Technical articles)** — technical blog posts, GitHub
    issues or PRs, design RFCs, conference talks, or other secondary
    sources that clarify non-obvious behavior or design intent.
  External links in any category MUST carry inline citations per §I.
- **Uniform spec-doc requirement (declared once)**: the feature's own
  design artifacts under `specs/[###-feature]/` (including but not
  limited to `spec.md`, `plan.md`, and other design docs such as
  `data-model.md`, `contracts/`, `quickstart.md`) are required reading
  for EVERY task. Rather than repeating them per task, the planner MUST
  declare this directory once at the top of `tasks.md` (a "Required
  Spec Docs" section), and every task executor MUST read those design
  docs alongside the task-specific Required Reading. Individual task
  declarations do NOT re-list these feature design docs.
- **Inheritance from plan**: the documentation research recorded in
  `plan.md` (per §II) seeds the 官方文档 and 技术文章 categories for
  downstream tasks. Tasks MUST inherit this list and MAY extend it with
  task-specific items; tasks MUST NOT silently drop a document the plan
  declared relevant to the unit being changed.
- **No-empty rule**: when a category genuinely has no applicable
  document for a given task, the declaration MUST state "None" for that
  category explicitly, so the absence is a deliberate decision rather
  than an oversight.
- **Planning-time gate**: the Required Reading declaration is a
  planning-time gate, enforced in `tasks.md` before implementation
  begins. Implementation MUST NOT start on a task whose declaration is
  missing or incomplete.

**Example per-task declaration**:

```
Required Reading:
- 规范文档: style/api.md; [AIP-2 AIP Numbering](https://google.aip.dev/2); [refer docs](URL)
- 官方文档: [dependency docs](URL) — version; [upstream README](URL)
- 技术文章: [issue/discussion/blog](URL)
```

**Rationale**: an executor who edits code without reading the governing
code conventions (and the external standards those conventions cite) or
the upstream dependency docs produces changes that are locally plausible
but globally inconsistent — they drift from the repo's conventions or
re-implement behavior a dependency already provides. Forcing every task
to declare its reading list at planning time makes the required context
explicit, concrete, and reviewable, ensures the executor builds full
context before touching code rather than discovering it mid-edit, and
complements §II (which obligates reading at plan time) by extending the
same discipline to every implementation task. Centralizing the feature
design docs as a one-time declaration avoids redundant repetition while
keeping them mandatory; this principle also complements §III
(refactoring discipline), since a sound refactor requires understanding
the existing unit's design — exactly the context this declaration forces
into view.

## Spec Artifact Scope

This constitution applies to the following Spec Kit artifacts:

| Artifact | Citations Required |
|----------|-------------------|
| `spec.md` | YES — all external facts, requirement rationale, and domain context |
| `plan.md` | YES — all technical decisions, dependency choices, and design references |
| `research.md` | YES — inherently research-driven; every finding needs a source link |
| `data-model.md` | YES — schema designs referencing standards or upstream contracts |
| `contracts/` | WHEN APPLICABLE — cite the spec or RFC an API contract derives from |
| `tasks.md` | YES — task descriptions that cite external libraries, tools, commands, patterns, or inherited design decisions; MUST also declare Required Reading per §V |
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
  Every external dependency or component referenced in `plan.md` (and any
  new dependency introduced in `tasks.md`) MUST show evidence of
  documentation research per §II, with inline citations to the official
  sources consulted, including transitive reading of references cited by
  those sources (linked sub-pages, RFCs, AIPs, upstream design docs)
  recorded as inline citations at the plan/design stage. Every change in `plan.md` and every implementation
  task in `tasks.md` MUST be classified as 新增 / 修改 / 删除 per §III,
  with 修改 changes implemented as refactors and every 修改 / 删除
  change carrying an explicit verdict on whether the existing design and
  layering still serve the new goal; outdated designs MUST be updated
  within the same change, never deferred as "out of scope". Every change
  in `plan.md` that introduces or modifies an externally callable boundary
  MUST include an explicit interface design per §IV covering all interface
  types touched, MUST comply with `style/api.md` (cited inline per §I),
  MUST be materialized in the feature's `contracts/` artifact, and MUST be
  inherited by the corresponding implementation tasks in `tasks.md`.
  Every implementation task in `tasks.md` MUST carry a Required Reading
  declaration per §V enumerating, across the three categories (规范文档 /
  官方文档 / 技术文章 — each of which may contain both in-repo docs and
  external links), the specific documents the executor MUST read before
  editing code; the declaration MUST cover context broader than the edit
  site, MUST inherit the plan's documented research (per §II), MUST state
  "None" explicitly for any empty category, and a task missing the
  declaration MUST NOT be started. The feature's design docs under
  `specs/[###-feature]/` MUST be declared once at the top of `tasks.md`
  as required reading for every task, not repeated per task.

**Version**: 3.2.0 | **Ratified**: 2026-06-18 | **Last Amended**: 2026-07-15
