# Tasks: Fake-LLM Think Chunking & Testdata Reorganization

**Input**: Design documents from `/specs/046-fake-llm-think-chunking/` — [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/template-config.md](contracts/template-config.md), [contracts/streaming-sequence.md](contracts/streaming-sequence.md), [quickstart.md](quickstart.md).

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/.

**Tests**: No TDD was requested. Per Constitution IV, compile + unit tests are part of each code-change task (not separate tasks): every implementation task writes the relevant unit test(s) and verifies `bazel build` + `bazel test //projects/game/fake-llm/...`.

**Organization**: Tasks are grouped by user story (spec.md US1 P1, US2 P2, US3 P2). US1 and US2 both extend the streaming handler and are sequential (US2 builds on US1's chunk loop). US3 touches a different file (`message_store.go`) and the `testdata/` tree; its new demonstration templates depend on US1+US2 fields, so US3 follows US2.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task in the same phase)
- **[Story]**: Which user story this task belongs to (US1/US2/US3); Setup/Polish phases have no story label
- Exact file paths are included in every task

## Path Conventions

This feature lives entirely under `projects/game/fake-llm/` (single Go package `projects/game/fake-llm/service` + `testdata/`). Source-code and `style/` paths are repository-relative; this feature's design docs (`research.md`, `data-model.md`, `contracts/…`, `quickstart.md`) are relative to the feature directory `specs/046-fake-llm-think-chunking/`.

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm the existing tree builds and tests green before any change; verify no new third-party dependency is introduced (research.md D8).

### Documents to read for this phase

- **代码规范文档**: 无（本 phase 仅做基线验证，不编写代码）
- **官方文档**: 无
- **技术文章**: 无

- [X] T001 Verify baseline: run `bazel build //projects/game/fake-llm/...` and `bazel test //projects/game/fake-llm/...`; confirm all green (incl. `TestNewMessageStore_LoadsEmbeddedSamples` = 14 messages, `TestNewMessageStore_LoadsEmbeddedTools` = 13 tools). Record the pre-change pinned counts/names as the reorganization diff baseline (data-model.md §7). Confirm `go.mod` needs no new dependency for `time.ParseDuration`/`log/slog`/`embed`/`yaml.v3` (all already in use).

---

## Phase 2: User Story 1 — Think streamed in chunks with configurable intervals (Priority: P1) 🎯 MVP

**Goal** (spec.md US1): A template's reasoning streams as multiple `reasoning_content` SSE deltas with configurable per-gap delays, so a long gap can simulate a think interruption. Covers FR-001..FR-007, FR-011 (delay exclusion), FR-017 (validation rules V1/V2/V4/V5), and FR-018.

**Independent Test** (quickstart.md Scenario 1 & 2): a `reasoning_chunks` template streams its reasoning in order (concatenation exact) and the configured `chunk_delays` produce real, context-aware gaps between reasoning frames.

> **Scope note**: this phase keeps the **legacy `Stall bool`** behavior unchanged (stall after the first reasoning chunk). Generalizing the stall to an arbitrary chunk position is US2 (Phase 3). See [data-model.md](data-model.md) §2–§3.

### Documents to read for this phase

- **代码规范文档**:
  - `style/golang.md` (the binding repo Go style — pointer/value semantics, slice/map element rules, comments, unit-test conventions; **already the authoritative source for the rules below**)
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide) — referenced by `style/golang.md` §引用 as the canonical external spec (reference)
  - Note: the other two Google Go Style sub-docs referenced by `style/golang.md` §引用 ([Style Decisions](https://google.github.io/styleguide/go/decisions), [Best Practices](https://google.github.io/styleguide/go/best-practices)) are reference-only (readability mentors); **not required reading** for this phase.
- **官方文档**:
  - [Go `time` — `ParseDuration`](https://pkg.go.dev/time#ParseDuration) — the duration-string parser used by `chunk_delays` (research.md D2)
  - [Go `time` — `After`](https://pkg.go.dev/time#After) and [Go `context`](https://pkg.go.dev/context) — context-aware delay sleep (`select` on `r.Context().Done()`, data-model.md §3)
  - [Go `log/slog`](https://pkg.go.dev/log/slog) — structured per-chunk observability logs (FR-018)
  - [OpenAI Chat Completions streaming reference](https://platform.openai.com/docs/api-reference/chat/streaming) — the SSE chunk shape being extended
  - [DeepSeek `reasoning_content` streaming](https://api-docs.deepseek.com/guides/reasoning_model) — multi-chunk reasoning is the real-world shape being emulated
- **技术文章**: 无

### Implementation for User Story 1

- [X] T002 [US1] Add `ReasoningChunks []string` (json/yaml `reasoning_chunks,omitempty`) and `ChunkDelays []string` (json/yaml `chunk_delays,omitempty`) fields to the `Message` struct in `projects/game/fake-llm/service/message_types.go`, with doc comments per [contracts/template-config.md](contracts/template-config.md) §2. Add a `parseDelays([]string) ([]time.Duration, error)` helper (in `message_types.go`) wrapping `time.ParseDuration`. Add a unit test `Test_parseDelays` in `message_types_test.go` (if a test file does not exist, the existing tests are in `handler_test.go`/`message_store_test.go`; add the test there) covering valid durations, unparseable strings, and the empty case. Verify `bazel build` + `bazel test //projects/game/fake-llm/...`.
- [X] T003 [P] [US1] Extend `Validate` in `projects/game/fake-llm/service/startup.go` with rules V1 (every `reasoning_chunks` entry non-empty), V2 (`chunk_delays` length ≤ `len(reasoning_chunks)−1`; each parses via `parseDelays`), V4 (not both `reasoning` and `reasoning_chunks`), V5 (not `tool_call` together with `reasoning_chunks`/`chunk_delays`; `stall_after` is added to V5 in T009 when that field lands) per [research.md](research.md) §validation. Add table-driven cases to `TestValidate` in `projects/game/fake-llm/service/message_store_test.go` (where `TestValidate` lives) for each new rule. Verify `bazel build` + `bazel test`. (Depends on T002 fields; different file from T004.)
- [X] T004 [P] [US1] Generalize the random-fallback pool exclusion in `Match` (`projects/game/fake-llm/service/matcher.go:57-62`) to FR-011: add `isHangCapable(*Message) bool` returning true when `Stall` is set OR `reasoning_chunks` has any non-zero parsed delay; filter the pool with it. Add `TestMatch_FallbackExcludesHangCapable` in `projects/game/fake-llm/service/matcher_test.go` (quickstart.md Scenario 7): a store whose only fallback candidates include a delay template, a `stall:true` template, and a chunked-without-delay template; assert the first two are never picked and the third may be. Verify `bazel build` + `bazel test`. (Depends on T002 fields; different file from T003.)
- [X] T005 [US1] Refactor `responseSpec` in `projects/game/fake-llm/service/handler.go`: change `Reasoning string` → `Reasoning []string` (effective ordered pieces) and add `Delays []time.Duration`; keep `Stall bool` (legacy, US2 generalizes it). Update `specFromMessage` to compute effective reasoning (wrap legacy single `Reasoning` as a 1-element slice) and parse delays. Update `serveNonStreaming` to set `ReasoningContent: strings.Join(spec.Reasoning, "")` (FR-006 concatenation, no delays). Update `specFromTool` to yield empty `Reasoning`/`Delays`. Add/adjust unit tests so non-streaming chunked reasoning returns the joined string. Verify `bazel build` + `bazel test`. (Depends on T002; same file as T006 — do T005 before T006.)
- [X] T006 [US1] Refactor `serveStreaming` (`projects/game/fake-llm/service/handler.go:484-521`) into a general context-aware chunk loop per [data-model.md](data-model.md) §3 and [contracts/streaming-sequence.md](contracts/streaming-sequence.md) §2: emit reasoning deltas (role only on frame 0), sleeping the configured `Delays[i]` before chunk `i+1` via `select { case <-time.After(d): case <-r.Context().Done(): return }`; keep the legacy `Stall bool` behavior as "block after the first reasoning chunk" (do NOT yet generalize the position — that is US2); emit the content delta + finish + `[DONE]`; emit a structured `slog.Info` per reasoning chunk (fields `chunk_index`, `role_kind`, `delay_ms`, plus `stall_after` on the stall frame) for FR-018/SC-007. Replace `textStreamChunks` with a builder that is byte-identical for the non-chunked case (FR-007). Add `TestServeHTTP_ChunkedReasoning` (no delays; assert frame order, role-on-first, concatenation exact), `TestServeHTTP_ChunkedReasoningDelays` (small delays via real `httptest.Server`; assert gaps ≈ configured; assert caller-cancel mid-gap unblocks), and `TestServeHTTP_ChunkedReasoningLogs` (captures `slog` output or asserts the log call site for a chunked+delay+legacy-stall template: one entry per reasoning chunk with `chunk_index`/`role_kind`/`delay_ms`, plus `stall_after` on the stall frame — quickstart Scenario 8) in `projects/game/fake-llm/service/handler_test.go`. Add `TestTextStreamChunks_NonChunked_Unchanged` pinning byte-identical output vs pre-change for a legacy template (FR-007), plus a chunked reasoning-only template (`text == ""`) frame-count assertion covering the chunked-path content-delta omission nuance (data-model.md §3). Verify `bazel build` + `bazel test //projects/game/fake-llm/...` is fully green, including the unchanged `TestServeHTTP_Streaming` and `TestServeHTTP_StreamingStallAfterFirstChunk`.

**Checkpoint**: US1 delivered — chunked reasoning + configurable intervals + observability + backward compatibility. Think-interruption-via-long-gap is simulatable (SC-001/SC-002/SC-004). Legacy stall still works (FR-010 partial — position 0 only).

---

## Phase 3: User Story 2 — Permanent stall positionable after a think chunk (Priority: P2)

**Goal** (spec.md US2): The permanent stall can begin after any chosen reasoning chunk, not only the first. Covers FR-008, FR-009, FR-010 (full), FR-017 (V3 + V5-`stall_after` extension).

**Independent Test** (quickstart.md Scenario 3): a `stall_after: K` template emits chunks `0..K` then blocks until the caller cancels; legacy `stall:true` still stalls after the first chunk.

### Documents to read for this phase

- **代码规范文档**:
  - `style/golang.md` (binding repo Go style)
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide) (reference, per `style/golang.md` §引用)
  - Note: [Style Decisions](https://google.github.io/styleguide/go/decisions) and [Best Practices](https://google.github.io/styleguide/go/best-practices) (also in `style/golang.md` §引用) are reference-only; **not required reading** for this phase.
- **官方文档**:
  - [Go `context`](https://pkg.go.dev/context) — stall unblocks only via `r.Context().Done()` (FR-009)
  - [OpenAI Chat Completions streaming reference](https://platform.openai.com/docs/api-reference/chat/streaming) — wire-shape context
- **技术文章**: 无

### Implementation for User Story 2

- [ ] T007 [US2] Add `StallAfter *int` (json/yaml `stall_after,omitempty`) to the `Message` struct in `projects/game/fake-llm/service/message_types.go` with a doc comment per [contracts/template-config.md](contracts/template-config.md) §2. Verify `bazel build` + `bazel test`. (Depends on Phase 2 complete; first task of US2.)
- [ ] T008 [US2] Generalize the stall in `projects/game/fake-llm/service/handler.go`: change `responseSpec.Stall bool` → `StallAfter *int` (nil = no stall; else the reasoning-chunk index after which to block). Update `specFromMessage` to compute the effective stall position per [research.md](research.md) D3 (`StallAfter` if set, else `0` when legacy `Stall` is true, else `nil`). Update the `serveStreaming` loop (from T006) so the permanent block fires after the reasoning chunk at the effective index instead of the first chunk — block on `<-r.Context().Done()` only (FR-009). Add `TestServeHTTP_ChunkedReasoningStallAfter` (a `stall_after:1` template; assert exactly chunks 0–1 arrive, no further frame in a probe window, and cancelling the context unblocks) in `projects/game/fake-llm/service/handler_test.go`, and confirm `TestServeHTTP_StreamingStallAfterFirstChunk` (legacy `stall:true`) still passes unmodified (FR-010). Verify `bazel build` + `bazel test`. (Depends on T007; same file as T007's struct — sequence T007 then T008.)
- [ ] T009 [US2] Add validation rule V3 to `Validate` in `projects/game/fake-llm/service/startup.go`: if `StallAfter` is set it must satisfy `0 ≤ *StallAfter ≤ max(0, len(reasoning_chunks)−1)` (for a legacy single-`reasoning` template the implicit length is 1, so only `0` is valid) — reject out-of-range per FR-017. Extend validation rule V5 from T003 to also reject `tool_call` together with `stall_after` (D5 — research.md V5 / contracts §2 cover `reasoning_chunks`/`chunk_delays`/`stall_after`; the `stall_after` half lands here with the field). Extend `isHangCapable` in `projects/game/fake-llm/service/matcher.go` to also return true when `StallAfter != nil` (FR-011). Add table-driven `TestValidate` cases (in `message_store_test.go`) for in-range / out-of-range `stall_after` and for `tool_call` + `stall_after`, and extend `TestMatch_FallbackExcludesHangCapable` (in `matcher_test.go`) to cover a `stall_after` template. Verify `bazel build` + `bazel test`. (Depends on T007; run after T008 so the tree stays green at each checkpoint — different files from T008.)

**Checkpoint**: US2 delivered — positionable permanent stall + full backward compatibility. The full think-interruption matrix (long finite gap via US1, permanent stall at a position via US2) is simulatable.

---

## Phase 4: User Story 3 — Testdata organized by scenario/module (Priority: P2)

**Goal** (spec.md US3): The loader supports multiple messages per file, and the shipped 17 `sample_*` files are reorganized into ~8 scenario/module-based files; 3 new demonstration think-interrupt templates are added. Covers FR-012..FR-017 (incl. loader validation rule V6), SC-005/SC-006; FR-011's fallback exclusion (implemented in T004/T009) is verified for the new templates here.

**Independent Test** (quickstart.md Scenario 5 & 6): multi-message files merge indistinguishably from single-message files; the existing 14+13 templates load byte-identical from the new layout; duplicates are rejected; the new templates raise the count to 17.

### Documents to read for this phase

- **代码规范文档**:
  - `style/golang.md` (binding repo Go style — applies to the `message_store.go` loader change)
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide) (reference)
  - Note: [Style Decisions](https://google.github.io/styleguide/go/decisions) and [Best Practices](https://google.github.io/styleguide/go/best-practices) (also in `style/golang.md` §引用) are reference-only; **not required reading** for this phase.
- **官方文档**:
  - [Go `embed`](https://pkg.go.dev/embed) — the `//go:embed testdata/*` the loader reads
  - [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) — YAML parsing for multi-message files
  - [YAML 1.2 spec](https://yaml.org/spec/1.2.2/) — reference for the testdata document shape
- **技术文章**: 无

### Implementation for User Story 3

- [ ] T010 [US3] Add multi-message file detection to `LoadFromFS` in `projects/game/fake-llm/service/message_store.go`: generalize `tryParseToolsFile` into `detectAndParse(data, path)` using a combined `fileShapeProbe{Tools, Messages}` with precedence `tools:` > `messages:` > single-message, and reject a file carrying both `tools:` and `messages:` (rule V6) per [research.md](research.md) D4 and [data-model.md](data-model.md) §4. Merge multi-message entries into the flat slice exactly as single-message files (sort by `Name`, then `Validate`). Add `TestLoadFromFS_MultiMessageFile` in `projects/game/fake-llm/service/message_store_test.go` (quickstart.md Scenario 6): one multi-message file + one single-message file merge and sort correctly; a duplicate name across the two is rejected; a file with both `tools:` and `messages:` is rejected. Verify `bazel build` + `bazel test`. (Independent of US1/US2 handler changes — different file.)
- [ ] T011 [US3] Reorganize the existing testdata under `projects/game/fake-llm/service/testdata/` into the 8 scenario/module files in [data-model.md](data-model.md) §7 (`chat.yaml`, `stall_recovery.yaml`, `saolei.yaml`, `saolei_tools.yaml`, `operation.yaml`, `operation_tools.yaml`, `planner.yaml`, `planner_tools.yaml`). **Content is preserved verbatim** — only file grouping changes; convert each group's messages into a single `messages:` file (or keep `tools:` files as-is). This includes moving `stall-mid-reasoning` from `sample_stall.yaml` into `stall_recovery.yaml` (as a `messages:` file); the 3 new demonstration templates are added later in T012. Delete the old `sample_*` files. Verify the existing `TestNewMessageStore_LoadsEmbeddedSamples` (14) and `TestNewMessageStore_LoadsEmbeddedTools` (13) still pass with ZERO assertion changes (FR-015/SC-005) — this is the content-neutrality proof. Verify `bazel build` + `bazel test //projects/game/fake-llm/...`. (Depends on T010 for the multi-message loader.)
- [ ] T012 [US3] Add the 3 new demonstration templates to `projects/game/fake-llm/service/testdata/stall_recovery.yaml` (alongside `stall-mid-reasoning`, moved there in T011) per [contracts/template-config.md](contracts/template-config.md) §3.3–§3.5: `think-interrupt-gap` (chunked reasoning + a long finite gap), `think-interrupt-stall` (chunked reasoning + `stall_after`), `think-healthy-cadence` (chunked reasoning + short gaps, no stall). Each must be excluded from the fallback pool (FR-011 — already covered by T004/T009). Update the pin test `TestNewMessageStore_LoadsEmbeddedSamples` in `projects/game/fake-llm/service/message_store_test.go`: change the count `14 → 17` and extend the sorted-name list + add field assertions for the 3 new templates (additive — existing 14 assertions unchanged, FR-015). Verify `bazel build` + `bazel test //projects/game/fake-llm/...`. (Depends on T011 + US1/US2 fields being understood by the handler.)

**Checkpoint**: US3 delivered — multi-message files supported; testdata reorganized by scenario; new think-interrupt templates validate the capability end-to-end in the embedded store and serve Feature 044's resume.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Whole-tree green, tooling tidy, the Constitution VI exemption documented, and the quickstart validated.

### Documents to read for this phase

- **代码规范文档**: `style/golang.md` (final review pass)
- **官方文档**: 无
- **技术文章**: 无

- [ ] T013 Run whole-tree verification: `bazel build //...` and `bazel test //projects/game/fake-llm/...`; run `bazel run //:gazelle projects/game/fake-llm` (regenerate `BUILD.bazel` only if files were added/removed — testdata is `//go:embed`, no BUILD change expected, but confirm), `bazel run //:go -- mod tidy -v`, and `bazel mod tidy`. Confirm no new third-party dependency appeared (research.md D8). Confirm the agent/desktop trees are untouched and still build (`bazel build //projects/game/...` is NOT required to pass for this feature, but confirm no accidental edits outside `projects/game/fake-llm/`).
- [ ] T014 [P] Create `projects/game/fake-llm/README.md` documenting: the service purpose (OpenAI-compatible mock LLM for large tests), the testdata config format (link to [contracts/template-config.md](contracts/template-config.md)), and the **Constitution VI large-test exemption** (fake-llm is test infrastructure validated by real-transport unit tests; its end-to-end consumption flows through Feature 044's large-test resume — see [plan.md](plan.md) Constitution Check Gate 5). This satisfies the exemption's "在 README 中说明" requirement.
- [ ] T015 [P] Run the [quickstart.md](quickstart.md) validation scenarios end-to-end (Scenarios 1–8) via `bazel test //projects/game/fake-llm/...` and confirm each scenario's expected outcome; cross-check the per-chunk observability logs (Scenario 8) carry `chunk_index`/`role_kind`/`delay_ms`/`stall_after`.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: no dependencies — establishes the 14/13 baseline.
- **Phase 2 (US1)**: depends on Phase 1. MVP — delivers chunking + intervals + observability.
- **Phase 3 (US2)**: depends on Phase 2 (extends US1's `serveStreaming` loop and `responseSpec`).
- **Phase 4 (US3)**: T010/T011 depend on Phase 1 only (loader + reorganization are independent of the handler); **T012 depends on US1+US2** (the new templates use `reasoning_chunks`/`chunk_delays`/`stall_after`). So US3 as a whole follows US2.
- **Phase 5 (Polish)**: depends on all prior phases.

### User Story Dependencies

- **US1 (P1)**: starts after Setup. No dependency on other stories.
- **US2 (P2)**: starts after US1 (shares `handler.go`/`message_types.go`).
- **US3 (P2)**: loader/reorg independent of US1/US2; new templates depend on US1+US2.

### Within Each User Story

- Struct-field additions before the code that consumes them.
- `handler.go` tasks are sequential (same file): T005 → T006 (US1); T007 → T008 (US2, struct then loop).
- Each task writes its unit test(s) and verifies `bazel build` + `bazel test` (Constitution IV — no separate test tasks).

### Parallel Opportunities

- **US1**: after T002 (fields), T003 (`startup.go`) and T004 (`matcher.go`) are different files → parallel ([P]). T005/T006 are `handler.go` → sequential.
- **US2**: T009 (`startup.go` + `matcher.go`) runs after T008 (`handler.go`) — different files, but sequenced to keep the tree green.
- **US3**: T010 (loader) and T011 (reorganization) are sequential (T011 needs the multi-message loader), T012 after both.
- **Polish**: T014 and T015 are [P] (README vs test run — different concerns).

---

## Parallel Example: User Story 1

```text
# After T002 (struct fields land), launch the two different-file tasks together:
Task T003: "Extend Validate in startup.go (V1/V2/V4/V5) + tests"
Task T004: "Generalize Match fallback exclusion in matcher.go (FR-011) + tests"

# Then the handler.go work is strictly sequential:
Task T005: "Refactor responseSpec + specFromMessage + serveNonStreaming in handler.go"
Task T006: "Refactor serveStreaming chunk loop + delays + logging in handler.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1 (baseline).
2. Complete Phase 2 (US1): chunked reasoning + configurable intervals + observability.
3. **STOP and VALIDATE**: US1 independently — chunked reasoning reconstructs exactly; configured delays are observable; legacy behavior byte-identical.
4. At this point the think-interruption-via-long-gap scenario is already simulatable (the primary 044 unblocker).

### Incremental Delivery

1. Setup → US1 (MVP: chunking + intervals).
2. + US2 → positionable permanent stall (full stall matrix).
3. + US3 → organized testdata + demonstration templates (directly serves Feature 044's resume).
4. Polish → whole-tree green + README exemption + quickstart validation.

### Notes

- [P] tasks = different files, no dependency on an incomplete in-phase task.
- Backward compatibility is the dominant risk: T006's byte-equality pin and the unchanged pin tests (T011) are the guards (FR-007/FR-010/SC-004/SC-005).
- The new demonstration templates (T012) are additive; they raise the embedded message count 14 → 17 and require the pin-test update in the same task (FR-015's content-neutrality applies only to the existing 14).
- No agent/desktop/proto/testplan changes (research.md D8); the feature is localized to `projects/game/fake-llm/`.
- The Constitution VI large-test exemption MUST be documented in the README (T014) — fake-llm's correctness is covered by real-transport unit tests, and its end-to-end consumption is Feature 044's large-test resume.
