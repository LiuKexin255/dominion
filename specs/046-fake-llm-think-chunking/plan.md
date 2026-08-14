# Implementation Plan: Fake-LLM Think Chunking & Testdata Reorganization

**Branch**: `046-fake-llm-think-chunking` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/046-fake-llm-think-chunking/spec.md`

## Summary

Upgrade the `fake-llm` mock LLM (`projects/game/fake-llm/`) so a template's reasoning (think) content can be streamed as multiple SSE chunks with configurable inter-chunk output intervals, and the permanent stall can be positioned after any reasoning chunk. This lets large tests simulate a **think interruption** — a gap between think chunks that exceeds the agent's idle timeout — which is the missing capability blocking [Feature 044](../044-llm-stall-recovery-fix/spec.md)'s large-test acceptance (`large-test-status.md` §2/§4/§5). Companion enabler: [Feature 045 deploy-config](../045-deploy-config/spec.md) provides the controlled (short) agent idle timeout; this feature provides the gap-producing mock.

Secondary: the testdata loader gains multi-message-per-file support, and the shipped 17 one-template `sample_*` files are reorganized into ~8 scenario/module-based files (the one-case-per-file anti-pattern). All existing behavior is preserved byte-for-byte (FR-007/FR-010/SC-004); the 14-message / 13-tool pin tests (`message_store_test.go`) continue to pass.

Technical approach (from [research.md](research.md)): add explicit `reasoning_chunks` / `chunk_delays` / `stall_after` fields to the `Message` config; generalize `serveStreaming` (`handler.go`) from a fixed 3-frame emitter into a context-aware chunk loop that sleeps between reasoning deltas and can block (stall) after a chosen chunk; extend `LoadFromFS` (`message_store.go`) with unambiguous `messages:`-key multi-message file detection alongside the existing single-message and `tools:` shapes.

## Technical Context

**Language/Version**: Go (module `dominion`, repo Go toolchain via `bazel run //:go`). The fake-llm service is pure Go (`projects/game/fake-llm/`).

**Primary Dependencies**: `net/http`, `embed` (testdata baked into the binary), `encoding/json`, `gopkg.in/yaml.v3`, `log/slog` (structured logging), `math/rand/v2`, OpenAI-compatible Chat Completions over HTTP/SSE. No new third-party dependencies.

**Storage**: embedded testdata filesystem (`//go:embed testdata/*` in `message_store.go`). No external storage.

**Testing**: Go `testing` (unit), executed via `bazel test //projects/game/fake-llm/...`. Streaming behavior is tested through a real `httptest.Server` + `*http.Client` transport (see `handler_test.go` `TestServeHTTP_StreamingStallAfterFirstChunk`, `TestServeHTTP_RealServerSmoke`).

**Target Platform**: Linux server (stateless HTTP service, `kind: stateless` in `service.yaml`).

**Project Type**: web-service (OpenAI-compatible mock LLM, test infrastructure).

**Performance Goals**: N/A — this is a test mock; the inter-chunk delays are intentionally configurable (including multi-second gaps) to simulate reasoning cadence and stalls.

**Constraints**:
- Backward compatibility is absolute: every existing template and every existing test (incl. the 14-message / 13-tool pin tests in `message_store_test.go`) MUST stay green without modification to their assertions (FR-007/FR-010/SC-004/SC-005).
- The streamed wire shape MUST remain OpenAI Chat Completions-compatible: only the first reasoning delta carries `role:"assistant"`; subsequent reasoning deltas carry only `reasoning_content`; the text (`content`) delta follows all reasoning deltas; finish + `[DONE]` terminate (FR-004/FR-005).
- No agent / desktop / proto changes — chunked reasoning is already a protocol-valid wire shape the agent accumulates (`projects/game/agent/src/session-team.ts:855-865`, `reasoningChunks.join("")`).

**Scale/Scope**: Small. Touch surface: `message_types.go` (struct fields), `startup.go` (validation), `handler.go` (`responseSpec`, `serveStreaming`, chunk builders), `message_store.go` (multi-message detection), `matcher.go` (fallback-pool exclusion), and the `testdata/` reorganization + new demonstration templates + pin-test count update.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.3.0. Gates:

1. **文档阅读门禁 (V)** — PASS (deferred to `/speckit.tasks`). tasks.md will declare the per-phase reading list, including `style/golang.md` and the external style documents it references (Google Go Style Guide) plus the phase-relevant stdlib/OpenAI docs and this feature's own contracts. (`style/README.md` is a link index without normative content, so it is not required reading per V's "地址直接包含实际内容" rule.)
2. **实现门禁 (II 重构式变更 / III 接口优先)** — PASS by design.
   - *Refactoring over patching (II)*: `serveStreaming` is REFACTORED from a fixed-shape 3-frame emitter + a special-case `i==0` stall into a single general chunk loop with per-chunk delays and a positionable stall. This collapses the "first chunk then block" hack into the general model rather than layering another branch. The `Stall bool` legacy field is preserved as sugar (`stall:true` ≡ `stall_after:0`) so existing templates are unchanged.
   - *Interface-first (III)*: the template config schema (fields, types, validation, precedence) is fixed in Phase 1 as a written contract ([contracts/template-config.md](contracts/template-config.md)) BEFORE any code. The SSE chunk-sequence contract is fixed in [contracts/streaming-sequence.md](contracts/streaming-sequence.md).
3. **编译 + 单测门禁 (IV)** — applies per code change (`bazel build` + `bazel test //projects/game/fake-llm/...`). Not a separate task.
4. **引用门禁 (I)** — PASS; all design docs and (in implementation) code comments carry repository-relative or full-URL citations.
5. **大型测试验收门禁 (VI)** — JUSTIFIED EXCEPTION (see below).

### Gate 5 (Constitution VI) — justified exception

`fake-llm` is **test infrastructure** (a mock LLM consumed by other services' large tests), not a delivered service under test. Its correctness is fully covered by unit tests that exercise the **real HTTP transport** (`httptest.Server` + `*http.Client`, `handler_test.go`), which already validate the streaming/stall wire behavior end-to-end. A standalone large test for fake-llm would be circular (deploying a mock to test the mock) and adds no signal beyond the transport-level unit tests.

The end-to-end large-test **consumption** of this feature's chunking capability happens in [Feature 044](../044-llm-stall-recovery-fix/spec.md)'s large-test resume (`large-test-status.md` §5): once this feature + 045 deploy-config land, 044 re-authors the `agent-stall` suite to use a controlled idle timeout + a fake-llm think-interrupt template, then runs `guitar run` to full green. So the large-test validation of the *capability* is delivered through 044's acceptance, not a fake-llm-only test plan.

Per Constitution VI's exemption clause ("对于一些因特殊原因无法进行大型测试…可以在 README 中说明可豁免"), this exception MUST be documented in a README/note under `projects/game/fake-llm/` as part of the implementation. Tracked as a follow-up task.

### Post-Phase-1 re-check

After completing the Phase-1 design ([research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)), the Constitution gates are re-evaluated:

- **Gate 2 (II/III)** — reinforced: `serveStreaming` is refactored into one general context-aware chunk loop (collapsing the `i==0` stall special-case), and the template config schema + streaming sequence are fixed as written contracts BEFORE code. No new violations.
- **Gate 5 (VI)** — unchanged justified exception; the design adds no standalone large-test SUT for fake-llm. The end-to-end consumption path is documented in [quickstart.md](quickstart.md) §"Downstream consumption".
- No Constitution violations were introduced by the design. The change remains a localized, backward-compatible extension of one mock service.

## Project Structure

### Documentation (this feature)

```text
specs/046-fake-llm-think-chunking/
├── plan.md                     # This file
├── spec.md                     # /speckit.specify output
├── research.md                 # Phase 0 — deferred config-shape decisions resolved
├── data-model.md               # Phase 1 — Message/responseSpec/loader data changes
├── contracts/
│   ├── template-config.md      # Phase 1 — testdata template config schema (the author-facing contract)
│   └── streaming-sequence.md   # Phase 1 — SSE chunk sequence for chunked/stall templates
├── quickstart.md               # Phase 1 — runnable validation guide
└── checklists/requirements.md  # spec quality checklist
```

### Source Code (repository root)

```text
projects/game/fake-llm/
├── service/
│   ├── message_types.go        # Message struct: + ReasoningChunks/ChunkDelays/StallAfter
│   ├── message_store.go        # LoadFromFS: + multi-message (messages:) file detection
│   ├── startup.go              # Validate: + chunk/delay/stall validation rules
│   ├── handler.go              # responseSpec + serveStreaming: generalized chunk loop
│   ├── matcher.go              # Match fallback pool: exclude delay/stall templates (FR-011)
│   ├── *_test.go               # unit tests: chunking, multi-message, validation, backward-compat
│   └── testdata/               # reorganized by scenario/module + new think-interrupt templates
│       ├── chat.yaml
│       ├── stall_recovery.yaml
│       ├── saolei.yaml
│       ├── saolei_tools.yaml
│       ├── operation.yaml
│       ├── operation_tools.yaml
│       ├── planner.yaml
│       └── planner_tools.yaml
├── cmd/main.go                 # unchanged
├── BUILD.bazel                 # regenerated by gazelle if needed (no new deps expected)
└── service.yaml                # unchanged
```

**Structure Decision**: Single existing Go service package (`projects/game/fake-llm/service`). No new packages, no new dependencies. The change is additive within the existing files plus a testdata regrouping. (See [data-model.md](data-model.md) for the field-level changes and [contracts/](contracts/) for the schemas.)

## Complexity Tracking

No Constitution violations requiring justification beyond Gate 5 (documented above). The design is a localized, backward-compatible extension of one mock service; no architectural expansion.
