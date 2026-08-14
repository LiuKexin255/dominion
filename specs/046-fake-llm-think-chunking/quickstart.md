# Quickstart: Fake-LLM Think Chunking & Testdata Reorganization

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Contracts**: [contracts/](contracts/)

This is a **validation run guide** — runnable scenarios that prove the feature works end-to-end. It is not an implementation reference; code/field details live in [data-model.md](data-model.md) and [contracts/template-config.md](contracts/template-config.md). All commands run from the repository root via `bazel`.

## Prerequisites

- Repository builds clean at the feature branch (`046-fake-llm-think-chunking`).
- `bazel` available (build & test entry). Go commands run via `bazel run //:go`.

## Build & unit-test baseline

```bash
bazel build //projects/game/fake-llm/...
bazel test  //projects/game/fake-llm/...
```

Expected: build green; all unit tests pass, including the updated pin tests (`TestNewMessageStore_LoadsEmbeddedSamples` now expects 17 messages; `TestNewMessageStore_LoadsEmbeddedTools` still 13) and the new chunking/multi-message/validation tests.

---

## Scenario 1 — Multi-chunk reasoning reconstructs the full text (SC-001)

**Validates**: FR-001/FR-004/FR-005, and the streaming-sequence contract ([contracts/streaming-sequence.md](contracts/streaming-sequence.md) §2).

Drive a streaming request against a chunked-reasoning template and assert the `reasoning_content` deltas arrive in order and concatenate to the full reasoning.

- **How it runs**: the unit test `TestServeHTTP_ChunkedReasoning` builds a synthetic store via `newStoreFromMap` (`handler_test.go`) with a `reasoning_chunks` template (no delays), serves it through `httptest.NewRecorder`, scans the SSE frames, and asserts: frame 0 carries `role:"assistant"` + `reasoning_chunks[0]`; frames 1..R−1 carry the remaining chunks (no role); the content delta follows; finish + `[DONE]` terminate; concatenation == full reasoning.
- **Expected outcome**: all `reasoning_content` deltas present, in order; concatenation exact (zero loss/reorder); content delta after all reasoning deltas.

## Scenario 2 — Configured inter-chunk intervals are observable as real gaps (SC-002)

**Validates**: FR-002/FR-003, and the think-interruption gap mechanism (US1.3).

- **How it runs**: `TestServeHTTP_ChunkedReasoningDelays` serves a chunked template with small `chunk_delays` (e.g. `["50ms","50ms"]`) through a real `httptest.Server` + `*http.Client`, records wall-clock arrival times of each reasoning frame, and asserts each gap ≈ the configured delay (within scheduling tolerance). It also asserts that cancelling the request context mid-gap unblocks the handler promptly (context-aware sleep).
- **Expected outcome**: measured gaps match configured delays; caller cancellation during a gap unblocks the stream.

## Scenario 3 — Permanent stall positioned after a chosen chunk (US2, FR-008/FR-009/FR-010)

**Validates**: positionable permanent stall + backward compatibility for the legacy `stall: true`.

- **How it runs**:
  - `TestServeHTTP_ChunkedReasoningStallAfter` — a chunked template with `stall_after: 1`; asserts exactly the first 2 reasoning frames arrive, no further frame within a probe window, and cancelling the request context unblocks the handler.
  - The existing `TestServeHTTP_StreamingStallAfterFirstChunk` (legacy `stall: true`, single reasoning) continues to pass unchanged — proving FR-010.
- **Expected outcome**: stall fires at the configured chunk; only caller cancellation unblocks it; legacy behavior unchanged.

## Scenario 4 — Backward compatibility for every existing template (SC-004)

**Validates**: FR-006/FR-007, SC-004 (zero regression).

- **How it runs**:
  - `TestNewMessageStore_LoadsEmbeddedSamples` / `TestNewMessageStore_LoadsEmbeddedTools` assert the existing 14 messages + 13 tools (now in the reorganized files) plus the 3 new demonstration templates (17 messages total) load with byte-identical field values for every pre-existing template.
  - A byte-equality test (`TestTextStreamChunks_NonChunked_Unchanged`) pins that the non-chunked streaming output is byte-identical to the pre-change `textStreamChunks` for a representative legacy template.
  - The non-streaming path for a chunked template returns the concatenated reasoning as one value (FR-006).
- **Expected outcome**: every pre-existing template's streaming + non-streaming output unchanged; reorganization changed file grouping only.

## Scenario 5 — Validation rejects malformed chunking/stall config (FR-017, SC-006)

**Validates**: startup fail-fast for broken templates.

- **How it runs**: `TestValidate` table-cases (added in T003/T009) exercise each rule from [contracts/template-config.md](contracts/template-config.md) §2: empty `reasoning_chunks` entry; `chunk_delays` longer than `len(chunks)−1`; unparseable duration; `stall_after` out of range; both `reasoning` and `reasoning_chunks`; `tool_call` + `reasoning_chunks`; `tool_call` + `stall_after`; and (loader) a file with both `tools:` and `messages:` (`TestLoadFromFS_MultiMessageFile`, T010).
- **Expected outcome**: each malformed config aborts `LoadFromFS`/`Validate` with a descriptive error naming the offending template/file.

## Scenario 6 — Multi-message files merge identically to single-message files (US3, FR-012/FR-013/FR-014/FR-015/SC-005)

**Validates**: the loader's multi-message support and the reorganization invariant.

- **How it runs**:
  - `TestLoadFromFS_MultiMessageFile` — an in-memory `fstest.MapFS` with one multi-message file + one single-message file; asserts all templates merge, sort by `Name`, and match indistinguishably from all-single-message-files; a duplicate name across the two files is rejected.
  - The reorganization's content-neutrality is verified by the pin tests (Scenario 4): the same 14+13 existing templates load with identical values from the new 8-file layout.
- **Expected outcome**: multi-message files merge correctly; duplicate names rejected; reorganization preserves the existing set exactly.

## Scenario 7 — Fallback pool excludes delay/stall templates (FR-011)

**Validates**: an unrelated turn never stalls or hangs by accident.

- **How it runs**: `TestMatch_FallbackExcludesHangCapable` — a store where the only no-keyword-match candidates include a chunked-with-delay template, a `stall:true` template, and a chunked-without-delay template; asserts the random fallback never returns the delay/stall templates, but MAY return the chunked-without-delay one.
- **Expected outcome**: hang-capable templates excluded; chunked-without-delay retained.

## Scenario 8 — Per-chunk observability logs (FR-018, SC-007)

**Validates**: structured logs verifiable in signoz.

- **How it runs**: `TestServeHTTP_ChunkedReasoningLogs` (added in T006) captures `slog` output (or asserts the log call site) while serving a chunked+delay+stall template and verifies one structured entry per reasoning chunk carrying `chunk_index`, `role_kind`, `delay_ms`, and `stall_after` (on the stall frame). In a deployed large-test environment the same fields are queryable in signoz under service `fake-llm`.
- **Expected outcome**: one log entry per streamed reasoning chunk with the required fields.

---

## Downstream consumption (Feature 044 large-test resume)

This feature's value is consumed by [Feature 044](../044-llm-stall-recovery-fix/large-test-status.md) §5: once 045 deploy-config lands a controlled (short) agent idle timeout, 044 re-authors the `agent-stall` suite to point a large test at a fake-llm `think-interrupt-gap` template (Scenario 3.3 in [contracts/template-config.md](contracts/template-config.md)) and asserts the agent's stream-stall detector fires during the configured gap. That end-to-end validation runs through the `testplan` skill (`guitar run`) and is tracked under Feature 044, not here (see [plan.md](plan.md) Constitution Check Gate 5 exemption).
