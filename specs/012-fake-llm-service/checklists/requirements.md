# Specification Quality Checklist: Fake LLM Service for Large-Test Integration

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-06-17
**Updated**: 2026-06-18
**Feature**: [spec.md](spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — *Spec does include Go / Bazel / OpenAI / LangChain terms because the feature is a test infrastructure component; these are treated as design constraints provided by the user rather than implementation choices.*
- [x] Focused on user value and business needs — *Centers on large-test coverage and maintainable test authoring.*
- [x] Written for non-technical stakeholders — *Scenarios and acceptance criteria are behavior-oriented.*
- [x] All mandatory sections completed — *User Scenarios, Requirements, Success Criteria, Assumptions, Out of Scope, References.*

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [ ] Requirements are testable and unambiguous — *See validation notes below.*
- [x] Success criteria are measurable — *Each SC is either a pass/fall percentage or an artifact inspection.*
- [ ] Success criteria are technology-agnostic (no implementation details) — *SC-002 references `additional_kwargs.reasoning_content` and `contentBlocks`, which are LangChain-specific. This is a tracked compromise because the underlying `@langchain/openai` behavior is a validated assumption.*
- [x] All acceptance scenarios are defined — *Each user story has explicit acceptance scenarios.*
- [x] Edge cases are identified — *Edge Cases section covers multi-match tiebreak, no-match random, empty keywords, duplicate name, non-streaming, malformed data, and model field.*
- [x] Scope is clearly bounded — *Out of Scope section lists nine explicit exclusions.*
- [x] Dependencies and assumptions identified — *Assumptions section and References.*

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria — *Each FR maps to scenarios or success criteria.*
- [x] User scenarios cover primary flows — *Full pipeline, configuration, removal, and test rewrite.*
- [x] Feature meets measurable outcomes defined in Success Criteria — *SC map to FRs and scenarios.*
- [ ] No implementation details leak into specification — *FR-023 names `artifact_pkg_go` + `artifact_image`; FR-024 names `AgentAdapterImpl` and `AIMessageChunk.additional_kwargs.reasoning_content`. These are necessary design constraints from the prototype validation, not arbitrary implementation choices.*

## Validation Notes

- **Technology-specific terms are intentional**: The feature is a test-infrastructure migration. The user's input explicitly named OpenAI Chat Completions, `@langchain/openai`, Go, and Bazel as constraints. The prototype validation confirmed that reasoning content preservation requires extracting from `additional_kwargs.reasoning_content`, which is why FR-024 and SC-002 reference LangChain internals. These are not implementation leaks but validated interface contracts.
- **Active-group ambiguity resolved (2026-06-18)**: The model was redesigned to stateless per-request matching. There is no active group, no position counter, and no cross-request state. The matching section in `data-model.md` defines the exact 4-step stateless algorithm. Concurrency hazards are eliminated by the stateless design.
- **Agent-side architecture change to Option B (2026-06-18)**: The original approach (deploy standard `agent` artifact with `OPENCODE_BASE_URL` hostname) does not work — dominion discovery is registry-resolver based, not k8s DNS. Replaced with: deploy `agent_test` artifact whose test bootstrap injects a resolver-aware provider resolving the fixed target `dominion:///game/fake-llm:8080` via the core resolver (`@dominion/common-js-resolver`). See FR-015, FR-016, FR-017, FR-025. The "single artifact" framing is retired; `agent_test` is retained as resolver-aware. Only the `FakeLlmAdapter` coverage bypass is removed.
- **Data-schema gap resolved (2026-06-18)**: The schema is now defined as a flat per-message structure: each entry has `name` (globally unique), `match_keywords` (non-empty array), `reasoning` (may be empty), and `text` (may be empty). Multiple files merge, sorted alphabetically by name. Duplicate names are rejected at startup. See `data-model.md` for the full definition.

## Notes

- Spec validated against checklist on 2026-06-17. Remaining open items are deliberate because the spec documents validated technical constraints rather than premature implementation choices.
