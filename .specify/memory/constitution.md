<!--
Sync Impact Report
==================
Constitution version: 3.6.0 → 3.7.0
Bump type: MINOR (materially expanded guidance in an existing principle)

Modified principles:
- §VI. Test Verification Granularity — added a new mandatory rule,
  "No separate build/unit-test tasks; large tests may be separate",
  stating that build and unit-test verification MUST be embedded inside
  the code-changing task they verify and MUST NOT be materialized as
  standalone sibling tasks, because splitting them off decouples the
  per-change gate from the change it validates and lets a change be
  declared "done" before its verification runs. Large-test
  verification, by contrast, MAY be a separate standalone task, since
  it is scoped to feature/requirement acceptance rather than to any
  single code change. The "Materialization in artifacts" rule was
  rewritten to express the embedding: build and unit-test verification
  MUST live inside each code-changing task (never as separate
  standalone tasks), while large-test verification MAY be a separate
  standalone task scoped to feature-level acceptance. The rationale
  paragraph and the Governance compliance review §VI clause were
  extended to record why embedding matters (per-change gate stays
  attached to the change; large tests run on a different cadence).

Added sections:
- None (new rule within existing §VI).

Removed sections:
- None.

Templates requiring updates:
- ✅ .specify/templates/plan-template.md — §VI Constitution Check entry
  extended to state build + unit-test verification MUST be embedded
  inside each code-changing task (never separate tasks) and large-test
  verification MAY be a separate task.
- ✅ .specify/templates/tasks-template.md — §VI Constitution Check entry
  extended likewise; "Tests" note clarified to distinguish embedded
  build/unit verification from separate large/integration test tasks.
- ⚠ .specify/templates/spec-template.md — no Constitution Check gate;
  not affected.

Follow-up TODOs:
- None.
-->

<!--
Sync Impact Report
==================
Constitution version: 3.5.0 → 3.6.0
Bump type: MINOR (new principle added)

Modified principles:
- None renamed or redefined.

Added sections:
- §VI. Test Verification Granularity (测试验证颗粒度) — new principle
  requiring every code change to be verified on a granularity ladder that
  ascends from smallest to largest (build → unit tests → large tests),
  with execution frequency inversely proportional to granularity: the
  smallest layers (build, unit tests) run on every change as per-change
  validation, while the largest layer (large tests) runs only at
  feature/requirement milestones. Mandatory rules cover the granularity
  ladder ordering, frequency-vs-granularity mapping, build as the first
  gate, unit tests as the per-change validation, large tests as
  feature/requirement validation, no-skipping/no-inversion, and
  materialization of the ladder in `plan.md` / `tasks.md`. The principle
  is authored as a pure normative discipline: it intentionally does NOT
  reference specific files, commands, or tooling (those live in
  `AGENTS.md` and the `style/` docs, which retain supremacy over runtime
  practice); it governs only ordering, frequency, and how the ladder is
  declared in the Spec Kit artifacts.

Removed sections:
- None.

Templates requiring updates:
- ✅ .specify/templates/plan-template.md — added §VI Constitution Check
  entry requiring the plan to declare the verification layers (build,
  unit-test, and large-test scope) and the small-to-large ordering.
- ✅ .specify/templates/tasks-template.md — added §VI Constitution Check
  entry requiring every code-changing task to carry build + unit-test
  per-change verification, with large-test verification scoped to
  feature-level checkpoints.
- ⚠ .specify/templates/spec-template.md — no Constitution Check gate;
  not affected.
- ⚠ .specify/templates/checklist-template.md — operational artifact,
  no principle-driven change expected.

Follow-up TODOs:
- None.
-->

<!--
Sync Impact Report
==================
Constitution version: 3.4.0 → 3.5.0
Bump type: MINOR (materially changed guidance in an existing principle)

Modified principles:
- §III. Refactoring-Oriented Changes — removed the documentation-format
  requirements (the 新增 / 修改 / 删除 classification) and reframed the
  principle purely as a design and development discipline. The core
  refactoring obligations are retained: refactor-not-stack, design
  review, synchronous design update, and design-implementation
  coherence. Removed rules:
  (1) "Change classification" — every change/task MUST be labeled
      新增 / 修改 / 删除.
  (2) "Classification accuracy" — the rules distinguishing 新增 from
      修改 from 删除.
  The remaining rules were de-scoped from "changes classified as 修改 /
  删除" to "a change that touches an existing unit", so the obligation
  is expressed in design/development terms without any reference to the
  removed format. The intro paragraph no longer requires each change to
  be classified. Governance compliance review and the plan/tasks
  templates were updated to match — no artifact now carries or inherits
  a 新增 / 修改 / 删除 label.

Added sections:
- None.

Removed sections:
- "Change classification" rule within §III.
- "Classification accuracy" rule within §III.
- All 新增 / 修改 / 删除 label references from the Governance
  compliance review, plan-template Constitution Check, and
  tasks-template Format spec + Constitution Check.

Templates requiring updates:
- ✅ .specify/templates/plan-template.md — §III Constitution Check entry
  rewritten to drop the classification requirement; reframed as a
  refactor discipline for changes touching an existing unit.
- ✅ .specify/templates/tasks-template.md — Format line dropped the
  [新增|修改|删除] token; removed the classification bullet from the
  Format spec; §III Constitution Check entry rewritten to drop the
  classification requirement.
- ⚠ .specify/templates/spec-template.md — no Constitution Check gate;
  not affected.

Follow-up TODOs:
- None.
-->

<!--
Sync Impact Report
==================
Constitution version: 3.3.0 → 3.4.0
Bump type: MINOR (materially refined and expanded guidance in an existing principle)

Modified principles:
- §V. Documentation First — three changes:
  (1) Restructured the Required Reading declaration from a PER-TASK
  obligation (every implementation task carries its own declaration) to
  a SINGLE unified declaration covering the whole `tasks.md`. The
  per-task framing and per-task-specific language were removed; the
  declaration is now authored once for the feature.
  (2) Removed the "Uniform spec-doc requirement (declared once)" rule,
  which required the feature's own design docs under
  `specs/[###-feature]/` (`spec.md`, `plan.md`, `data-model.md`,
  `contracts/`, `quickstart.md`) to be declared as required reading in
  `tasks.md`. This duplicated context loading already performed by the
  implementation command (`/speckit.implement` step 3 — "Load and
  analyze the implementation context"), so it is now out of scope for
  §V; §V focuses on the documents that workflow does not already load
  (code style docs, external dependency docs, technical articles). A
  short scope-exclusion note records this so the design docs are not
  re-added.
  (3) Added two specificity rules: every declared entry MUST resolve to
  a concrete file path or link (never a description, category name, or
  summary), and every external entry MUST carry its own inline
  `[description](URL)` link even if the same source is cited elsewhere
  in the design artifacts, so each Required Reading entry is
  self-contained and verifiable.

Added sections:
- None (new rules within existing §V).

Removed sections:
- "Uniform spec-doc requirement (declared once)" rule within §V.
- Per-task Required Reading framing throughout §V.

Templates requiring updates:
- ✅ .specify/templates/tasks-template.md — converted Format spec
  "Required Reading" entry and Constitution Check §V entry to the
  unified declaration; added concrete-entry and external-link rules;
  removed the "Required Spec Docs" section (design-doc loading overlaps
  with /speckit.implement step 3).
- ✅ .specify/templates/plan-template.md — converted §V Constitution
  Check entry to seed the unified declaration; removed design-doc-
  declared-once language; added concrete-entry and external-link rules.
- ⚠ .specify/templates/spec-template.md — no Constitution Check gate;
  not affected.

Follow-up TODOs:
- None.
-->

<!--
Sync Impact Report
==================
Constitution version: 3.2.0 → 3.3.0
Bump type: MINOR (materially expanded guidance in an existing principle)

Modified principles:
- §V. Documentation First — added a new mandatory rule,
  "Planner must read in-repo docs transitively before declaring",
  requiring the task planner to read the in-repo documents a task
  touches AND follow the in-repo file references those docs cite before
  authoring the task's Required Reading declaration, so the declaration
  enumerates every in-repo file the executor needs — not only the file
  at the edit site. Lazy planning (declaring one in-repo file while
  omitting the in-repo files it references) is a violation. The rule is
  scoped three ways to prevent over-expansion: (1) IN-REPO references
  only — external references cited by in-repo docs remain governed by
  §II's transitive-reading rule at plan time and MUST NOT be re-chased
  here; (2) the PLANNER only — the task executor inherits the planner's
  enumeration and has no obligation to re-derive referenced files;
  (3) MATERIALITY — only in-repo files genuinely relevant to the task's
  unit of change are enumerated, not every transitively reachable file.
  This complements §II (external transitive research at plan time) and
  the existing §V "Inheritance from plan" rule (tasks inherit the
  plan's documented research).

Added sections:
- None (new rule within existing §V).

Removed sections:
- None.

Templates requiring updates:
- ✅ .specify/templates/tasks-template.md — updated §V Constitution
  Check entry and Required Reading Format spec to require the planner
  to read in-repo docs and follow in-repo references before declaring.
- ✅ .specify/templates/plan-template.md — updated §V Constitution
  Check entry to note the task planner must read in-repo docs and
  follow their in-repo references when exporting the per-task Required
  Reading to tasks.md.
- ⚠ .specify/templates/spec-template.md — no Constitution Check gate;
  not affected.

Follow-up TODOs:
- None.
-->

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
  an   explicit, enumerated reading list rather than imposing an
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

Every code change MUST be carried out as a refactor of the affected unit
— a natural extension of a still-coherent design — rather than as logic
stacked on top of an existing unit. The design and the implementation
MUST stay coherent across the change; outdated designs MUST NOT survive
into the new version under the excuse of "out of scope".

**Mandatory rules**:

- **Refactor-not-stack rule**: when an existing unit (module, file,
  type, function, or design element) is touched, the change MUST be
  carried out by refactoring that unit so the new behavior is a natural
  extension of a still-coherent design. Appending logic onto an existing
  unit without revisiting its structure — so the unit accrues
  conditional branches, parallel code paths, or responsibilities it was
  never designed for — is a violation, even when the new behavior is
  correct.
- **Design review**: when a change touches an existing unit, the plan
  MUST review (a) the existing design, architecture, and code layering
  of the affected unit and (b) give an explicit verdict on whether that
  design still serves the new goal. "The existing design still applies"
  is an acceptable verdict when true; when it does not, the change MUST
  be expanded to bring the design back into coherence with the new goal.
- **Synchronous design update**: when a change reveals that the
  surrounding design, architecture, layering, or documentation is
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
the code. Requiring each change that touches an existing unit to revisit
the affected design makes that drift visible at planning time, when it
is cheapest to fix. Requiring synchronous updates keeps the design and
the code as one artifact across versions, instead of letting them
diverge until a future "refactor pass" that never arrives. This
complements §II (dependency research), which governs what is chosen;
this principle governs how change itself is carried out.

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

`tasks.md` MUST declare a "Required Reading" section enumerating the
specific documents that MUST be read BEFORE any implementation code is
edited. The declaration is mandatory — not optional, not deferred to
the executor's judgment. The goal is to ensure the executor understands
the full context surrounding the change, not just the narrow slice each
task directly touches.

**Mandatory rules**:

- **Unified declaration**: `tasks.md` MUST carry a single "Required
  Reading" section enumerating the specific documents the executor MUST
  read before editing code. Implementation MUST NOT start while this
  section is missing or incomplete.
- **Scope excludes feature design docs**: the feature's own design
  artifacts under `specs/[###-feature]/` (`spec.md`, `plan.md`,
  `data-model.md`, `contracts/`, `quickstart.md`, etc.) are loaded by
  the implementation workflow itself and are NOT declared here. This
  declaration covers the documents that workflow does not already load
  — the governing code style docs, the official docs of external
  dependencies, and the relevant technical articles.
- **Concrete entries only**: every declared document MUST be a concrete
  file path or a concrete link — never a description, category name, or
  summary. An entry such as "the style docs" or "dependency
  documentation" is a violation; it MUST resolve to an exact file (e.g.,
  `style/api.md`) or an exact URL (e.g.,
  `[AIP-2](https://google.aip.dev/2)`).
- **External links are mandatory and never inherited by omission**:
  every external document declared in any category MUST carry an inline
  `[description](URL)` link (per §I). An external document MUST NOT be
  referenced by description alone on the assumption that it was cited
  elsewhere in the design artifacts — each Required Reading entry MUST
  include the link so the entry is self-contained and verifiable.
- **Coverage scope**: the declaration MUST cover context broader than
  the edit sites — at minimum the code style docs that govern the
  affected units and the existing code or modules the change interacts
  with. Declaring only the files directly being edited is a violation
  — the purpose is full context, not just the edit site.
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
    dependencies, components, frameworks, tools, or services the change
    touches, plus the README of any upstream codebase relied upon.
  - **技术文章 (Technical articles)** — technical blog posts, GitHub
    issues or PRs, design RFCs, conference talks, or other secondary
    sources that clarify non-obvious behavior or design intent.
- **Inheritance from plan**: the documentation research recorded in
  `plan.md` (per §II) seeds the 官方文档 and 技术文章 categories. The
  declaration MUST inherit this list and MAY extend it with
  feature-specific items; it MUST NOT silently drop a document the plan
  declared relevant to the change.
- **Planner must read in-repo docs transitively before declaring**: the
  Required Reading declaration is authored by the task planner, and the
  planner MUST, BEFORE writing the declaration, read the in-repo
  documents the change touches (the 规范文档 under `style/*` and any
  other in-repo doc relevant to the unit of change) in full and follow
  the IN-REPO file references they cite — so the declaration enumerates
  every in-repo file the executor will need, not only the file at the
  edit site. Declaring a single in-repo file while omitting the in-repo
  files it transitively references is lazy planning and a violation.
  This rule is scoped three ways to prevent work bloat:
  1. **In-repo references only.** Only references that resolve to a
     file inside this repository MUST be followed. External references
     (URLs to off-repo docs, RFCs, AIPs, upstream repos) cited by
     in-repo docs are governed by §II's transitive-reading rule at
     plan time and MUST NOT be re-chased by the task planner.
  2. **Planner only.** The rule binds the author of `tasks.md`. The
     task executor inherits the planner's enumeration verbatim and has
     NO obligation to re-derive referenced files; the executor reads
     exactly what the planner declared.
  3. **Materiality.** The planner MUST apply judgment and enumerate
     only in-repo files genuinely relevant to the unit of change — not
     every transitively reachable file. Chasing the full reference
     graph indiscriminately is itself a violation of this scoping.
- **No-empty rule**: when a category genuinely has no applicable
  document for the change, the declaration MUST state "None" for that
  category explicitly, so the absence is a deliberate decision rather
  than an oversight.
- **Planning-time gate**: the Required Reading declaration is a
  planning-time gate, enforced in `tasks.md` before implementation
  begins. Implementation MUST NOT start while the declaration is
  missing or incomplete.

**Example declaration**:

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
re-implement behavior a dependency already provides. Forcing `tasks.md`
to declare its reading list at planning time makes the required context
explicit, concrete, and reviewable, and ensures the executor builds full
context before touching code rather than discovering it mid-edit.
Requiring every entry to resolve to a concrete file path or link — and
forbidding description-only external references that lean on citations
made elsewhere — keeps the declaration self-contained and verifiable
rather than a vague pointer. This principle complements §II (which
obligates reading at plan time) by extending the same discipline to
implementation, and complements §III (refactoring discipline), since a
sound refactor requires understanding the existing unit's design —
exactly the context this declaration forces into view. The planner-side
transitive-reading rule closes the gap between "the declaration exists"
and "the declaration is complete": an in-repo doc that looks
self-contained routinely cites other in-repo files (a `style/*.md` that
defers to a sibling), and a planner who reads only the top-level file
produces a declaration that is syntactically present but materially
incomplete. Forcing the planner to follow in-repo references — while
explicitly exempting external ones (§II's job) and demanding
materiality (to avoid reference-graph bloat) — makes the declaration a
faithful map of the context the executor actually needs.

### VI. Test Verification Granularity (测试验证颗粒度)

Every code change MUST be verified on a granularity ladder that ascends
from smallest to largest — build, then unit tests, then (when
applicable) large tests — with execution frequency inversely
proportional to granularity: the smallest layers run on every change as
per-change validation, the largest run only as feature/requirement
validation. This principle is a normative discipline: it governs
verification ordering, frequency, and how the ladder is declared in the
Spec Kit artifacts. It intentionally does NOT reference specific files,
commands, or tooling — those live in `AGENTS.md` and the repo's style
docs, which retain supremacy over runtime practice.

**Mandatory rules**:

- **Granularity ladder rule**: verification MUST proceed in ascending
  granularity order — build → unit tests → large tests. A verifier MUST
  NOT advance to a larger-granularity layer for a change while a smaller
  layer covering that same change is failing or unrun; each layer gates
  the next.
- **Frequency inversely proportional to granularity**: the smaller the
  granularity, the higher the execution frequency. Build and unit tests
  are the default per-change verification and MUST run after every code
  change. Large tests run less frequently — only at feature/requirement
  milestones or on demand — never as a per-change gate.
- **Build is the first gate**: a successful build of the affected units
  MUST precede any test layer. A build failure blocks and invalidates
  all subsequent verification for that change.
- **Unit tests are the per-change validation**: after a successful
  build, the unit tests covering the changed units MUST pass. Build and
  unit tests together constitute the mandatory per-change verification;
  a change is not verified until both pass.
- **Large tests are feature/requirement validation**: large tests
  validate whole features or systems end-to-end. They are NOT required
  after every code change; they are scoped to feature-level acceptance.
- **No skipping, no inversion**: a change MUST NOT be declared verified
  by running only a large test while skipping build and unit tests, nor
  by running a large test before the smaller layers for that change
  pass. Conversely, when a large test exists for a feature,
  build-and-unit-only verification does not by itself satisfy feature
  acceptance. Each layer validates a distinct concern; layers do not
  substitute for one another.
- **No separate build/unit-test tasks; large tests may be separate**:
  build and unit-test verification MUST NOT be materialized as
  standalone tasks separate from the code change they verify. They are
  an integral, inseparable part of the code-changing task — that task
  is not complete until both its build and its unit-test verification
  pass. Splitting build or unit-test verification into a sibling task
  decouples the per-change gate from the change it validates, lets a
  change be declared "done" before its verification runs, and MUST NOT
  be done. Large-test verification, by contrast, MAY be materialized as
  a separate standalone task, because it is scoped to
  feature/requirement acceptance rather than to any single code change
  and is not a per-change gate.
- **Materialization in artifacts**: `plan.md` MUST declare the
  verification layers the feature uses — build scope, unit-test scope,
  and (when applicable) large-test scope — together with the
  small-to-large ordering. `tasks.md` MUST materialize the ladder as
  follows: build and unit-test verification MUST be embedded inside
  each code-changing task as its per-change gate (never as separate
  standalone tasks; see the preceding rule), while large-test
  verification, when applicable, MAY be a separate standalone task
  scoped to feature-level acceptance rather than repeated per
  code-changing task.

**Rationale**: running only coarse-grained tests after every change is
slow and gives late, unfocused feedback, while running only
fine-grained tests never proves the feature works end-to-end. Ordering
verification from small to large — and matching frequency to
granularity — makes each change fail fast at the cheapest layer (a
broken build or a red unit test) while reserving the expensive
end-to-end layer for the moments where it actually carries new
information: feature and requirement acceptance. Requiring the artifacts
to declare the ladder explicitly turns verification from an ad-hoc habit
 into a reviewable part of the design, complementing §III (a change is
not complete until verified) and §IV (an externally callable boundary
is not accepted until its end-to-end coverage passes). Requiring build
and unit-test verification to live inside the code-changing task —
rather than as a detached sibling task — keeps the per-change gate
attached to the change it validates, so no change can slip through as
"done" before its smallest-layer verification has actually run;
allowing large tests their own task reflects that they are a separate
concern (feature acceptance) operating on a different cadence.

## Spec Artifact Scope

This constitution applies to the following Spec Kit artifacts:

| Artifact | Citations Required |
|----------|-------------------|
| `spec.md` | YES — all external facts, requirement rationale, and domain context |
| `plan.md` | YES — all technical decisions, dependency choices, and design references |
| `research.md` | YES — inherently research-driven; every finding needs a source link |
| `data-model.md` | YES — schema designs referencing standards or upstream contracts |
| `contracts/` | WHEN APPLICABLE — cite the spec or RFC an API contract derives from |
| `tasks.md` | YES — task descriptions that cite external libraries, tools, commands, patterns, or inherited design decisions; MUST also carry a unified Required Reading declaration per §V and a per-task verification ladder per §VI |
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
  recorded as inline citations at the plan/design stage. Every change in `plan.md` (and every implementation task in `tasks.md`) that touches an
  existing unit MUST be carried out as a refactor per §III — not as
  logic appended on top — and MUST carry an explicit verdict on whether
  the existing design and layering still serve the goal of the change;
  outdated designs MUST be updated within the same change, never
  deferred as "out of scope". Every change
  in `plan.md` that introduces or modifies an externally callable boundary
  MUST include an explicit interface design per §IV covering all interface
  types touched, MUST comply with `style/api.md` (cited inline per §I),
  MUST be materialized in the feature's `contracts/` artifact, and MUST be
  inherited by the corresponding implementation tasks in `tasks.md`.
  Every `tasks.md` MUST carry a single Required Reading declaration
  per §V enumerating, across the three categories (规范文档 /
  官方文档 / 技术文章 — each of which may contain both in-repo docs and
  external links), the specific documents the executor MUST read before
  editing code. Every entry MUST resolve to a concrete file path or
  link (never a description, category name, or summary), and every
  external entry MUST carry its own inline `[description](URL)` link
  even if the same source is cited elsewhere; the declaration MUST
  cover context broader than the edit sites, MUST inherit the plan's
  documented research (per §II), MUST state "None" explicitly for any
  empty category, and implementation MUST NOT start while the
  declaration is missing or incomplete. The feature's own design docs
  under `specs/[###-feature]/` are NOT declared here — they are loaded
  by the implementation workflow. The author of `tasks.md` MUST,
  before writing the declaration, read the in-repo documents the change
  touches and follow their IN-REPO file references so the declaration
  enumerates every in-repo file the executor needs (not only the
  edit-site files); external references cited by in-repo docs remain
  governed by §II and MUST NOT be re-chased, and the planner MUST
  apply materiality so only in-repo files genuinely relevant to the
  unit of change are enumerated. Every feature's `plan.md` MUST declare
  its verification layers per §VI — build scope, unit-test scope, and
  (when applicable) large-test scope — together with the
  small-to-large ordering, and every code-changing task in `tasks.md`
  MUST materialize that ladder: build and unit-test verification as the
  per-change gate, with large-test verification scoped to feature-level
  checkpoints rather than repeated per task, and verification MUST
  proceed in ascending granularity order (build → unit tests → large
  tests) with frequency inversely proportional to granularity. Build
  and unit-test verification MUST be embedded inside each code-changing
  task (never as separate standalone tasks), while large-test
  verification, when applicable, MAY be a separate standalone task
  scoped to feature-level acceptance; the concrete build/test commands
  and tooling remain defined by `AGENTS.md` and the repo's style docs.

**Version**: 3.7.0 | **Ratified**: 2026-06-18 | **Last Amended**: 2026-07-16
