---

description: "Task list template for feature implementation"
---

# Tasks: [FEATURE NAME]

**Input**: Design documents from `/specs/[###-feature-name]/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: The examples below include test tasks. Tests are OPTIONAL - only include them if explicitly requested in the feature specification.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] [新增|修改|删除] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- **[新增|修改|删除]**: Change classification per Constitution §III —
  新增 only for modules/files/types/design elements that did not
  previously exist; 修改 for any change to an existing unit (including
  adding a function to an existing class); 删除 for removal of an
  existing unit. 修改 / 删除 tasks MUST record a design-applicability
  review before implementation begins.
- **Required Reading**: every implementation task MUST carry a
  "Required Reading" declaration per Constitution §V enumerating the
  specific documents the executor MUST read before editing code, grouped
  by category. Any category may contain both in-repo docs and external
  links. **规范文档** = code style/spec docs (mainly `style/*`, plus the
  external standards they cite — e.g. AIPs, RFCs); **官方文档** =
  official docs of external dependencies/components/tools, upstream
  READMEs; **技术文章** = blogs, GitHub issues/PRs, RFCs, talks. The
  declaration MUST cover context broader than the edit site, MUST
  inherit the plan's documented research (per §II), and MUST state
  "None" explicitly for any empty category. The feature's design docs
  under `specs/[###-feature]/` are NOT re-listed here — they are
  declared once in the "Required Spec Docs" section and read by every
  task. A task without this declaration MUST NOT be started. **Planner
  diligence (§V)**: before authoring each declaration, the planner MUST
  read the in-repo documents the task touches and follow their IN-REPO
  file references, so the declaration enumerates every in-repo file the
  executor needs — not only the file at the edit site. External
  references cited by in-repo docs are governed by §II and MUST NOT be
  re-chased here; the planner MUST apply materiality and enumerate only
  in-repo files genuinely relevant to the unit of change.
- Include exact file paths in descriptions

## Constitution Check

*GATE: Must pass before implementation begins.*

- **Citation Provenance (§I)**: every task that references an external
  library, tool, command, pattern, or inherited design decision MUST
  include an inline `[description](URL)` link or explicitly cite the
  parent `spec.md`/`plan.md` source. A matching entry MUST appear in the
  `## References` section at the end of this file.
- **External Dependency Research (§II)**: any task that introduces a NEW
  dependency, library, framework, service, or component not already
  researched in `plan.md` MUST perform the same documentation research
  (official docs + source repository) and cite the findings inline (per
  §I) before implementation begins. Dependencies inherited from
  `plan.md` MUST explicitly reference the plan's research rather than
  restating decisions without provenance.
- **Refactoring-Oriented Changes (§III)**: every implementation task MUST
  carry the 新增 / 修改 / 删除 (Add / Modify / Delete) classification
  inherited from `plan.md`, where 新增 applies ONLY to modules, files,
  types, or design elements that did not previously exist (adding a
  function to an existing class, a field to an existing struct, or a
  branch to an existing function is 修改, not 新增). 修改 tasks MUST be
  implemented as refactors of the existing unit, not as logic appended
  on top. Every 修改 or 删除 task that touches an existing unit MUST,
  before implementation begins, record a brief review of whether the
  existing design, architecture, and layering of that unit still serve
  the goal of the change; if they do not, the task MUST be expanded (or
  split into a companion task) to bring the design back into coherence
  in the same change set. "Out of scope" MUST NOT be used to leave an
  outdated design in place. A task is not complete until the design
  artifacts (`spec.md` / `plan.md` / `data-model.md` / `contracts/` /
  `style/`) and the implementation agree.
- **Interface Design Coverage (§IV)**: any implementation task that
  touches an externally callable boundary (RPC service, HTTP endpoint,
  message subscriber, event producer, etc.) MUST inherit its interface
  design from `plan.md` and reference the corresponding `contracts/`
  source (e.g., `.proto` file, OpenAPI spec) rather than restating or
  inventing interface shapes at implementation time. The implemented
  interface MUST comply with `style/api.md` — that file is the single
  source of truth for interface conventions and the implementation MUST
  conform to whatever it currently requires; a divergence between the
  contract and the implementation MUST be resolved before the task is
  complete.
- **Documentation First (§V)**: every implementation task MUST carry a
  Required Reading declaration enumerating, across three categories,
  the specific documents the executor MUST read before editing code.
  Any category may contain both in-repo docs and external links — the
  internal/external axis is identical across all three:
  - **规范文档 (Code style/spec docs)** — code conventions and
    standards docs, mainly files under `style/`. External standards
    referenced by these docs (e.g., AIPs, RFCs, language style guides)
    belong in THIS category, not in the other two.
  - **官方文档 (Official docs)** — official documentation of external
    dependencies, components, frameworks, tools, or services the task
    touches, plus the README of any upstream codebase relied upon.
  - **技术文章 (Technical articles)** — technical blog posts, GitHub
    issues/PRs, design RFCs, or other secondary sources clarifying
    non-obvious behavior.

  External links in any category MUST carry inline citations per §I.
  The declaration MUST cover context broader than the task's direct
  edit site (at minimum the governing code style docs and the existing
  code/modules the change interacts with), MUST inherit the plan's
  documented research (per §II) and MAY extend it, MUST state "None"
  explicitly for any empty category, and a task missing the declaration
  MUST NOT be started. The feature's own design docs under
  `specs/[###-feature]/` are NOT listed here — see the "Required Spec
  Docs" section below. **Planner-side transitive reading (§V)**: the
  author of this `tasks.md` MUST, before writing each task's Required
  Reading, read the in-repo documents the task touches and follow the
  IN-REPO file references they cite, so the declaration enumerates
  every in-repo file the executor needs rather than only the edit-site
  file. This is scoped to in-repo references only (external references
  remain governed by §II and MUST NOT be re-chased), binds the planner
  only (the executor inherits the enumeration verbatim), and demands
  materiality (only in-repo files genuinely relevant to the unit of
  change — not the entire transitively reachable graph). Suggested inline format:

  ```
  Required Reading:
  - 规范文档: style/api.md; [AIP-2 AIP Numbering](https://google.aip.dev/2); [refer docs](URL)
  - 官方文档: [dependency docs](URL) — version; [upstream README](URL)
  - 技术文章: [issue/discussion/blog](URL)
  ```

## Required Spec Docs *(declared once per Constitution §V)*

Every task executor MUST read the feature's design docs under
`specs/[###-feature-name]/` BEFORE starting any task — including but
not limited to the main files below. The planner declares this
directory once here; it is NOT repeated in individual task declarations.

- `specs/[###-feature-name]/spec.md`
- `specs/[###-feature-name]/plan.md`

## Path Conventions

- **Single project**: `src/`, `tests/` at repository root
- **Web app**: `backend/src/`, `frontend/src/`
- **Mobile**: `api/src/`, `ios/src/` or `android/src/`
- Paths shown below assume single project - adjust based on plan.md structure

<!--
  ============================================================================
  IMPORTANT: The tasks below are SAMPLE TASKS for illustration purposes only.

  The /speckit.tasks command MUST replace these with actual tasks based on:
  - User stories from spec.md (with their priorities P1, P2, P3...)
  - Feature requirements from plan.md
  - Entities from data-model.md
  - Endpoints from contracts/

  Tasks MUST be organized by user story so each story can be:
  - Implemented independently
  - Tested independently
  - Delivered as an MVP increment

  DO NOT keep these sample tasks in the generated tasks.md file.
  ============================================================================
-->

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [ ] T001 Create project structure per implementation plan
- [ ] T002 Initialize [language] project with [framework] dependencies
- [ ] T003 [P] Configure linting and formatting tools

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

Examples of foundational tasks (adjust based on your project):

- [ ] T004 Setup database schema and migrations framework
- [ ] T005 [P] Implement authentication/authorization framework
- [ ] T006 [P] Setup API routing and middleware structure
- [ ] T007 Create base models/entities that all stories depend on
- [ ] T008 Configure error handling and logging infrastructure
- [ ] T009 Setup environment configuration management

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - [Title] (Priority: P1) 🎯 MVP

**Goal**: [Brief description of what this story delivers]

**Independent Test**: [How to verify this story works on its own]

### Tests for User Story 1 (OPTIONAL - only if tests requested) ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T010 [P] [US1] Contract test for [endpoint] in tests/contract/test_[name].py
- [ ] T011 [P] [US1] Integration test for [user journey] in tests/integration/test_[name].py

### Implementation for User Story 1

- [ ] T012 [P] [US1] Create [Entity1] model in src/models/[entity1].py
- [ ] T013 [P] [US1] Create [Entity2] model in src/models/[entity2].py
- [ ] T014 [US1] Implement [Service] in src/services/[service].py (depends on T012, T013)
- [ ] T015 [US1] Implement [endpoint/feature] in src/[location]/[file].py
- [ ] T016 [US1] Add validation and error handling
- [ ] T017 [US1] Add logging for user story 1 operations

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - [Title] (Priority: P2)

**Goal**: [Brief description of what this story delivers]

**Independent Test**: [How to verify this story works on its own]

### Tests for User Story 2 (OPTIONAL - only if tests requested) ⚠️

- [ ] T018 [P] [US2] Contract test for [endpoint] in tests/contract/test_[name].py
- [ ] T019 [P] [US2] Integration test for [user journey] in tests/integration/test_[name].py

### Implementation for User Story 2

- [ ] T020 [P] [US2] Create [Entity] model in src/models/[entity].py
- [ ] T021 [US2] Implement [Service] in src/services/[service].py
- [ ] T022 [US2] Implement [endpoint/feature] in src/[location]/[file].py
- [ ] T023 [US2] Integrate with User Story 1 components (if needed)

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - [Title] (Priority: P3)

**Goal**: [Brief description of what this story delivers]

**Independent Test**: [How to verify this story works on its own]

### Tests for User Story 3 (OPTIONAL - only if tests requested) ⚠️

- [ ] T024 [P] [US3] Contract test for [endpoint] in tests/contract/test_[name].py
- [ ] T025 [P] [US3] Integration test for [user journey] in tests/integration/test_[name].py

### Implementation for User Story 3

- [ ] T026 [P] [US3] Create [Entity] model in src/models/[entity].py
- [ ] T027 [US3] Implement [Service] in src/services/[service].py
- [ ] T028 [US3] Implement [endpoint/feature] in src/[location]/[file].py

**Checkpoint**: All user stories should now be independently functional

---

[Add more user story phases as needed, following the same pattern]

---

## Phase N: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] TXXX [P] Documentation updates in docs/
- [ ] TXXX Code cleanup and refactoring
- [ ] TXXX Performance optimization across all stories
- [ ] TXXX [P] Additional unit tests (if requested) in tests/unit/
- [ ] TXXX Security hardening
- [ ] TXXX Run quickstart.md validation

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2 → P3)
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Can start after Foundational (Phase 2) - May integrate with US1 but should be independently testable
- **User Story 3 (P3)**: Can start after Foundational (Phase 2) - May integrate with US1/US2 but should be independently testable

### Within Each User Story

- Tests (if included) MUST be written and FAIL before implementation
- Models before services
- Services before endpoints
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel
- All Foundational tasks marked [P] can run in parallel (within Phase 2)
- Once Foundational phase completes, all user stories can start in parallel (if team capacity allows)
- All tests for a user story marked [P] can run in parallel
- Models within a story marked [P] can run in parallel
- Different user stories can be worked on in parallel by different team members

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together (if tests requested):
Task: "Contract test for [endpoint] in tests/contract/test_[name].py"
Task: "Integration test for [user journey] in tests/integration/test_[name].py"

# Launch all models for User Story 1 together:
Task: "Create [Entity1] model in src/models/[entity1].py"
Task: "Create [Entity2] model in src/models/[entity2].py"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo
4. Add User Story 3 → Test independently → Deploy/Demo
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1
   - Developer B: User Story 2
   - Developer C: User Story 3
3. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence

## References *(mandatory per Constitution §I — Citation Provenance)*

<!--
  ACTION REQUIRED: Every external source cited in any task description
  MUST appear here with a traceable link. If no external material is cited,
  keep the section and write "No external references."

  Group links by category and pin versions/commits where the cited state
  matters. Inline citations use [description](URL) at the point of use.
-->

### Official Documentation

- [Title or description](URL) — version/section if applicable

### Repositories

- [org/repo — file or commit description](URL)

### Articles & RFCs

- [Article or RFC title](URL)
