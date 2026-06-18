<!--
Sync Impact Report
=====================================================================
Version change: (uninitialized template) → 1.0.0

Modified principles:
  - N/A (first ratification; all placeholder slots removed)

Added sections:
  - Core Principles → I. Citation Provenance (引用溯源)
  - Spec Artifact Scope
  - Governance

Removed sections:
  - Placeholder principle slots [PRINCIPLE_1..5_NAME/DESCRIPTION]
  - Placeholder sections [SECTION_2_NAME/CONTENT], [SECTION_3_NAME/CONTENT]
  - Placeholder [GOVERNANCE_RULES], [PROJECT_NAME], version/date tokens

Templates requiring updates:
  - .specify/templates/spec-template.md — ✅ updated (References section added)
  - .specify/templates/plan-template.md — ✅ updated (References section + Constitution Check gate)
  - .specify/templates/tasks-template.md — N/A (task lists inherit provenance from parent spec/plan)
  - .specify/templates/checklist-template.md — N/A (operational artifact)

Follow-up TODOs: none
=====================================================================
-->

# Dominion Spec Constitution

This constitution governs the authoring and maintenance of specification
and planning artifacts produced by Spec Kit (`spec.md`, `plan.md`, and
related design documents). It does not duplicate engineering conventions
already covered in `AGENTS.md`, which retains supremacy over runtime code
and build practices.

## Core Principles

### I. Citation Provenance (引用溯源)

Every factual claim, design decision, API specification, or technical
detail in `spec.md` or `plan.md` that originates from external material
MUST carry a traceable link to its source.

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
- **Consolidated References section**: every `spec.md` and `plan.md`
  MUST end with a `## References` section enumerating all cited URLs,
  grouped by category (Official Documentation / Repositories / Articles
  & RFCs). Documents that cite no external material MUST still include
  the section with a note stating "No external references."
- **No bare claims**: statements presented as external fact without a
  citation are treated as assumptions and MUST be relocated to the
  `## Assumptions` section of the artifact.

**Rationale**: traceable citations let reviewers verify design decisions
against authoritative sources, prevent hallucinated or outdated claims
from entering the spec pipeline, and give future maintainers a
deterministic path back to the original source of truth.

## Spec Artifact Scope

This constitution applies to the following Spec Kit artifacts:

| Artifact | Citations Required |
|----------|-------------------|
| `spec.md` | YES — all external facts, requirement rationale, and domain context |
| `plan.md` | YES — all technical decisions, dependency choices, and design references |
| `research.md` | YES — inherently research-driven; every finding needs a source link |
| `data-model.md` | YES — schema designs referencing standards or upstream contracts |
| `contracts/` | WHEN APPLICABLE — cite the spec or RFC an API contract derives from |
| `tasks.md` | NO — task lists inherit provenance from their parent spec/plan |
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
  plan-template "Constitution Check" gate MUST verify that `spec.md`
  and `plan.md` contain a `## References` section when external material
  is cited, and that every inline claim has a matching link.

**Version**: 1.0.0 | **Ratified**: 2026-06-18 | **Last Amended**: 2026-06-18
