# Implementation Plan: [FEATURE]

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]

**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

[Extract from feature spec: primary requirement + technical approach from research]

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: [e.g., Python 3.11, Swift 5.9, Rust 1.75 or NEEDS CLARIFICATION]

**Primary Dependencies**: [e.g., FastAPI, UIKit, LLVM or NEEDS CLARIFICATION]

**Storage**: [if applicable, e.g., PostgreSQL, CoreData, files or N/A]

**Testing**: [e.g., pytest, XCTest, cargo test or NEEDS CLARIFICATION]

**Target Platform**: [e.g., Linux server, iOS 15+, WASM or NEEDS CLARIFICATION]

**Project Type**: [e.g., library/cli/web-service/mobile-app/compiler/desktop-app or NEEDS CLARIFICATION]

**Performance Goals**: [domain-specific, e.g., 1000 req/s, 10k lines/sec, 60 fps or NEEDS CLARIFICATION]

**Constraints**: [domain-specific, e.g., <200ms p95, <100MB memory, offline-capable or NEEDS CLARIFICATION]

**Scale/Scope**: [domain-specific, e.g., 10k users, 1M LOC, 50 screens or NEEDS CLARIFICATION]

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: Every external fact, dependency choice,
  API reference, or design decision in this plan MUST carry an inline
  `[description](URL)` link and a matching entry in the `## References`
  section below. Statements without a citation are assumptions and MUST
  move to the spec's `## Assumptions` section. Any design decision
  restated in `tasks.md` MUST carry or explicitly inherit a citation.
- Version pins or commit SHAs MUST accompany any citation whose
  referenced state matters.
- All cited links MUST resolve to publicly accessible resources.
- **Code Style Precedence (§II)**: every agent that executes a
  code-touching task exported to `tasks.md` MUST read the applicable
  `style/` documents and their cited external references before modifying
  code in that task. This read obligation is per-executor and per-task
  (non-transferable across agents); it is enforced as a dispatch-time
  precondition and MUST NOT be enumerated as a per-task acceptance
  criterion, a dedicated "read style guide" task, or a separate workflow
  step.
- **External Dependency Research (§III)**: every external dependency,
  library, framework, service, or component referenced in this plan MUST
  be researched against its official documentation and source repository
  BEFORE the plan is finalized. Research MUST cover purpose, supported
  versions, the relevant public API surface, known constraints or
  deprecations, and licensing terms. The plan MUST record the
  documentation URLs consulted as inline citations (per §I) with a
  one-line summary of the finding, and pin the version or version range
  researched. Relying on memory or prior assumptions is a violation.
- **Refactoring-Oriented Changes (§IV)**: every change described in this
  plan MUST be explicitly classified as 新增 (Add), 修改 (Modify), or
  删除 (Delete), where 新增 applies ONLY to modules, files, types, or
  design elements that did not previously exist (adding a function to an
  existing class, a field to an existing struct, or a branch to an
  existing function is 修改, not 新增). 修改 changes MUST be implemented
  as refactors of the existing unit, not as logic appended on top. Every
  修改 or 删除 change MUST be accompanied by a review of the existing
  design, architecture, and layering of the affected unit, with an
  explicit verdict on whether that design still serves the new goal;
  when it does not, the change MUST be expanded to bring the design
  back into coherence in the same version. "Out of scope" MUST NOT be
  used to carry an outdated design forward. The task list exported to
  `tasks.md` MUST inherit and preserve these classifications.
- **Interface Design Coverage (§V)**: every change in this plan that
  introduces or modifies an externally callable boundary (RPC service,
  HTTP endpoint, message subscriber, event producer, etc.) MUST include
  an explicit interface design enumerating every interface surface it
  adds or changes — for each, the protocol (e.g., gRPC, HTTP/REST), the
  service and method names, the request/response (or resource/message)
  shapes, and the error codes. Every interface designed here MUST comply
  with `style/api.md`; the constitution does not restate that file's
  rules — `style/api.md` is the single source of truth for interface
  conventions and the plan MUST conform to whatever it currently
  requires. This plan MUST reference `style/api.md` inline (per §I) and
  confirm it was reviewed before any interface design is recorded. The
  interface design MUST be materialized in the feature's `contracts/`
  artifact (e.g., `.proto` files, OpenAPI specs) as part of this plan's
  design output, and the implementation tasks exported to `tasks.md`
  MUST inherit the design and reference the corresponding `contracts/`
  source rather than restating interface shapes at implementation time.

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)
<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths (e.g., apps/admin, packages/something). The delivered plan must
  not include Option labels.
-->

```text
# [REMOVE IF UNUSED] Option 1: Single project (DEFAULT)
src/
├── models/
├── services/
├── cli/
└── lib/

tests/
├── contract/
├── integration/
└── unit/

# [REMOVE IF UNUSED] Option 2: Web application (when "frontend" + "backend" detected)
backend/
├── src/
│   ├── models/
│   ├── services/
│   └── api/
└── tests/

frontend/
├── src/
│   ├── components/
│   ├── pages/
│   └── services/
└── tests/

# [REMOVE IF UNUSED] Option 3: Mobile + API (when "iOS/Android" detected)
api/
└── [same as backend above]

ios/ or android/
└── [platform-specific structure: feature modules, UI flows, platform tests]
```

**Structure Decision**: [Document the selected structure and reference the real
directories captured above]

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |

## References *(mandatory per Constitution §I — Citation Provenance)*

<!--
  ACTION REQUIRED: Every external source cited in this plan MUST appear
  here with a traceable link. If no external material is cited, keep the
  section and write "No external references."

  Group links by category and pin versions/commits where the cited state
  matters. Inline citations use [description](URL) at the point of use.
-->

### Official Documentation

- [Title or description](URL) — version/section if applicable

### Repositories

- [org/repo — file or commit description](URL)

### Articles & RFCs

- [Article or RFC title](URL)
