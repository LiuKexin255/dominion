# Feature Specification: Fake-LLM Think Chunking & Testdata Reorganization

**Feature Branch**: `046-fake-llm-think-chunking`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "为 `projects/game/fake-llm/` 增加功能：think 部分分 chunk，并且支持设置 chunk 之间的输出间隔。背景是 `specs/044-llm-stall-recovery-fix/large-test-status.md`，目前为 deploy 增加配置文件这样可以调整默认 chunk timeout。但是 fake-llm 目前不能模拟 think 中断的情况。升级 fake-llm 以支持这个功能。另外：testdata 虽然当时设计 merge 多个文件的测试用例，但每个 llm case 都要单独一个文件也是反模式，应当根据场景或者模块组织测试数据。"

## Motivation

`fake-llm` (`projects/game/fake-llm/`) is the OpenAI-compatible mock LLM used by the game's large tests. It matches a request's user text against keyword-gated templates embedded under `service/testdata/` and serves a canned streaming or non-streaming response (`projects/game/fake-llm/service/handler.go`).

Today, a stall can be simulated in exactly one way: a template flagged `stall: true` emits the **first** chunk (the role + reasoning delta) and then blocks forever with the connection alive but no further data, until the caller cancels the request (`projects/game/fake-llm/service/handler.go:513-517`, `projects/game/fake-llm/service/testdata/sample_stall.yaml`). This models the "TCP alive, no SSE data" failure mode — but **only after the very first chunk, and only as a permanent block**.

Two gaps make this insufficient for the stall-recovery test program:

### Problem 1 — fake-llm cannot simulate a stall *during* the thinking phase ("think interruption")

[Feature 044 — LLM Stream Stall Recovery](../044-llm-stall-recovery-fix/spec.md) detects stalls via the **gap between consecutive SSE chunks** (the agent's `idleTimeout`, now a 120s default / 60s minimum, with a per-reasoning-model floor). The agent accumulates reasoning across incremental `reasoning-delta` chunks (`projects/game/agent/src/session-team.ts:855-865` joins `reasoningChunks`), so a reasoning model that streams thinking over time is the normal, supported wire shape.

But fake-llm has no way to:

- split the think (`reasoning_content`) into multiple chunks emitted over time, and
- control the **output interval (delay)** between consecutive think chunks.

Without these, large tests cannot simulate a **think interruption** — a reasoning model that emits a few thinking chunks, then goes silent long enough for the agent's idle watchdog to fire mid-thinking. This is exactly the scenario 044's large-test acceptance needs: [Feature 044 large-test status](../044-llm-stall-recovery-fix/large-test-status.md) §2 records the T012 blocker (the heartbeat test cannot get a controlled idle window), §4 records that "fake-llm has no recoverable-silence template", and §5's resume plan calls for "controlled config values (shorter/controlled idle timeouts, heartbeat intervals)" — **enabled by the deploy-config feature** ([Feature 045 — Deploy Config Support](../045-deploy-config/spec.md)) — **combined with a fake-llm that can emit reasoning chunks with controlled gaps**. Today neither half exists for think-phase stalls.

### Problem 2 — testdata is organized one-message-per-file (an anti-pattern)

The loader (`projects/game/fake-llm/service/message_store.go:66-122` `LoadFromFS`) was designed to **merge multiple files** into one flat message slice. But in practice every shipped template lives in its **own** file — `sample_greeting.yaml`, `sample_chat.yaml`, `sample_farewell.json`, `sample_saolei_start.yaml`, … (17 `sample_*` files under `service/testdata/`). One case per file is an anti-pattern: related scenarios (e.g. the multi-step saolei/planner tool chains, or the stall family) are spread across many files, and a new test scenario typically needs *several* cooperating templates that are easier to read and maintain together. Test data should be organized by **scenario or module**, grouping the templates that a given test story exercises.

### Goal

This feature upgrades fake-llm so that:

1. A template's think content can be streamed as multiple chunks with **configurable inter-chunk output intervals**, enabling simulation of a reasoning model's real streaming cadence and — critically — a stall that occurs *during* the thinking phase (a gap between think chunks that exceeds the agent's idle timeout). The existing permanent-stall behaviour is preserved and becomes positionable after a think chunk rather than only after the first.
2. The loader supports **multiple messages per file**, and the shipped testdata is reorganized by scenario/module so a test story's templates live together.

## Clarifications

### Session 2026-08-13

- Q: How should a test author declare the chunked reasoning *content* of a template — as an explicit list of chunk strings, or as a single reasoning string that the handler splits? → A: **Explicit list of chunk strings.** Each reasoning piece is a separate declared entry, so chunk boundaries (and therefore where a mid-thinking stall lands) are exact and author-controlled; each inter-chunk gap can carry its own delay (FR-003); and the stall position (FR-008) is a clean index into the list. Auto-split is rejected because implicit boundaries make precise stall positioning harder and may split mid-word.
- Q: Should fake-llm emit structured logs of each chunk emission and its configured delay, so test operators can verify the think-cadence via signoz during 044's large tests? → A: **Yes.** Emit a structured log per chunk on emission carrying the chunk index and configured delay, matching the existing `logSystemPrompts` observability pattern (`projects/game/fake-llm/service/handler.go`) so operators can verify the configured cadence/stall actually fired in signoz.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Think streamed in chunks with configurable output intervals (Priority: P1)

A large-test author configures a fake-llm template whose reasoning (think) content is split into several streaming chunks, and sets the output interval (delay) between consecutive chunks. When such a template matches a request, the streaming response delivers the reasoning in pieces, separated by the configured wall-clock delays — mirroring how a real reasoning model streams `reasoning_content` over time. By setting one interval longer than the agent's idle timeout, the author can simulate a **think interruption**: the gap between two think chunks triggers the agent's stall detector mid-thinking, exactly the condition 044's large tests must exercise.

**Why this priority**: This is the capability 044's large-test acceptance is blocked on. Without it, the stall-recovery feature cannot be validated against a think-phase stall at controlled, fast config values (the 045 deploy-config enabler has no fake-llm counterpart to drive it). Everything else in this spec is secondary to unblocking 044.

**Independent Test**: Configure a template with reasoning split into chunks and a known inter-chunk interval, then drive a streaming request against it and record arrival times of the SSE frames. Verify the reasoning arrives as multiple `reasoning_content` deltas separated by the configured delays, and that concatenating the deltas reconstructs the full reasoning text. Separately, set an interval above a configured idle timeout and verify the consuming agent's stall detector fires during the gap.

**Acceptance Scenarios**:

1. **Given** a template whose reasoning is declared as multiple chunks with a configured inter-chunk interval, **When** the template matches a streaming request, **Then** the response emits one `reasoning_content` delta per chunk, and the wall-clock gap between consecutive reasoning frames equals the configured interval (within normal scheduling tolerance).
2. **Given** a template with chunked reasoning, **When** the streaming deltas are concatenated, **Then** they reconstruct the template's full reasoning text exactly (no loss, duplication, or reordering relative to the declared chunk order).
3. **Given** a template whose configured inter-chunk interval exceeds the consuming agent's idle timeout, **When** the gap between two reasoning chunks elapses with no new frame, **Then** the agent's stream-stall detector fires during the thinking phase (the think-interruption / recoverable-silence scenario).
4. **Given** a template that declares **different intervals for different gaps** between its reasoning chunks, **When** the stream is served, **Then** each gap independently reflects its configured delay — so a stall can be positioned at a specific point in the thinking phase.
5. **Given** a template that combines chunked reasoning with the permanent-stall behaviour, **When** the stream reaches the configured stall point (after a reasoning chunk), **Then** no further data is emitted while the connection stays alive — a stall positioned mid-thinking, unblocked only when the caller cancels the request (mirrors the existing `stall: true` semantics, generalised to a chunk position).
6. **Given** an existing template that declares a single reasoning string with **no** chunk/interval configuration, **When** it matches a streaming request, **Then** its behaviour is byte-for-byte identical to before this feature (all reasoning in the first delta, no delays) — full backward compatibility.

---

### User Story 2 - Permanent stall positionable after a think chunk (Priority: P2)

A large-test author can request that the permanent stall (the existing "emit a chunk then block until the caller cancels" behaviour) begins **after a chosen think chunk** rather than only after the very first delta. This complements the interval-based gap stall (US1.3) with the true "connection alive, no data forever" failure mode, positioned anywhere within the thinking phase.

**Why this priority**: US1's interval mechanism alone can simulate a detectable stall (a gap that trips the idle timeout). But 044's existing passing cases rely on the permanent-stall shape (block-until-cancel), and some assertions distinguish "gap that exceeds the timeout" from "connection goes permanently silent". Making the stall positionable preserves those tests and covers the full matrix without forcing authors to choose one mechanism.

**Independent Test**: Configure a template that emits N reasoning chunks and then stalls. Verify the first N chunks arrive, no further frame arrives within a probe window, and cancelling the request context unblocks the stream (the documented unblock path).

**Acceptance Scenarios**:

1. **Given** a template configured to stall after the K-th reasoning chunk, **When** the stream is served, **Then** exactly K reasoning frames are delivered and no further data arrives while the connection stays alive.
2. **Given** such a stalled stream, **When** the caller cancels the request (as the agent's idle-timeout abort does), **Then** the handler unblocks and the request completes — the stall is unblockable only via caller cancellation, identical to the existing `stall: true` contract.
3. **Given** an existing template flagged only with the simple `stall: true` (no chunked reasoning), **When** it matches a streaming request, **Then** it behaves exactly as before (stall after the first chunk) — backward compatible.

---

### User Story 3 - Testdata organized by scenario/module (Priority: P2)

A large-test author or maintainer can group several cooperating templates into a single testdata file organized around a scenario or module (e.g. all stall-recovery templates together; all saolei tool-chain templates together), instead of spreading one template per file. The loader merges every file's templates into one flat store regardless of how they are grouped, so grouping is purely an organization choice that does not change matching behaviour.

**Why this priority**: One-message-per-file is the stated anti-pattern. Fixing it improves maintainability and makes a test story's full template set readable in one place. It is lower priority than US1 (which unblocks 044) because it is a quality refactor with no behavioral change.

**Independent Test**: Put two templates in one file and a third in another, then load the store. Verify all three are present, correctly merged, sorted, and matched exactly as if they had been in three separate files; verify a duplicate name across the two files is rejected.

**Acceptance Scenarios**:

1. **Given** a testdata file containing multiple message templates, **When** the store is loaded, **Then** every template in the file is parsed, validated, and merged into the flat message slice indistinguishably from single-template files.
2. **Given** the shipped testdata reorganized into scenario/module-based files, **When** the store is loaded, **Then** the merged set of templates (by Name, keywords, reasoning, text, tool_call, stall) is identical to the pre-reorganization set — reorganization changes file grouping only, not template content or matching.
3. **Given** two templates that declare the same `name` (whether in the same multi-template file or across files), **When** the store is loaded, **Then** startup validation rejects the duplicate (the existing name-uniqueness invariant holds for multi-template files too).
4. **Given** an existing single-template file, **When** the store is loaded, **Then** it continues to parse and merge exactly as before — single-template files remain fully supported (backward compatible).

---

### Edge Cases

- **Chunked reasoning with no interval configured**: a chunked template that does not specify an interval emits its chunks back-to-back with no deliberate delay (or a sensible zero/negligible default), so chunking alone does not force a stall — the gap-based stall requires an explicit interval above the idle timeout.
- **Single-chunk reasoning declared via the chunking mechanism**: declaring the reasoning as exactly one chunk is equivalent to the legacy single-reasoning form (one delta, no delay) and MUST NOT change behaviour.
- **Chunked reasoning combined with a tool-call response**: chunking applies to the reasoning of a text response. A tool-call template carries no streamed reasoning; the chunking mechanism MUST be a no-op (or rejected at validation) for tool-call templates. The exact treatment (silent no-op vs. validation error) is a plan-level decision.
- **Non-streaming request for a chunked-reasoning template**: chunking and intervals are streaming-only behaviours. A non-streaming (single JSON object) response returns the full reasoning concatenated as one value with no delays.
- **A configured interval that the consumer's idle timeout never reaches**: if every interval is below the consumer's idle timeout, no stall fires and the turn completes normally — chunking with short intervals simulates a healthy reasoning model's cadence, which is itself a useful test scenario (proving no false stall).
- **Interval that is long-but-finite vs. permanent stall**: a long-but-finite interval lets the stream eventually resume; the permanent-stall path (US2) blocks until the caller cancels. A test chooses the mechanism matching the failure mode it wants to assert; both are available.
- **Empty or single-character reasoning chunk**: an empty reasoning chunk would produce an empty `reasoning_content` delta. Validation SHOULD reject chunks that would emit empty deltas (the agent already filters empty deltas, `session-team.ts:860`, but fake-llm should not rely on that), so each declared chunk carries meaningful text.
- **Stall positioned at a chunk index beyond the declared chunks**: declaring a stall-after-chunk index that does not correspond to an actual chunk is a configuration error and MUST be rejected at validation rather than silently never stalling.
- **Interaction with the no-match random fallback**: a template that triggers a stall or a long interval MUST remain excluded from the random-fallback pool exactly as today (`projects/game/fake-llm/service/matcher.go:57-62` excludes `Stall` templates); chunked/interval templates that can hang a stream MUST likewise never be picked for an unrelated turn.
- **Reorganization must not change Name-keyed sorting or matching**: the merged store is sorted alphabetically by `Name` and matched lowest-`Name`-first (`matcher.go:34-41`); regrouping files MUST NOT alter Name values and therefore MUST NOT alter which template wins a multi-match.
- **`tools:` files and multi-message files coexisting**: the loader already detects a top-level `tools:` key as a tool-config file (`message_store.go:163-180`). A new multi-message file shape MUST be distinguishable from both the single-message shape and the tools shape, with unambiguous precedence.

## Requirements *(mandatory)*

### Functional Requirements

**Think chunking & configurable intervals (US1)**

- **FR-001**: A template's chunked reasoning MUST be declared as an **explicit list of chunk strings** (one entry per reasoning chunk). The streaming response MUST emit one `reasoning_content` SSE delta per declared chunk, in the declared order, instead of a single delta. (The exact config field name is a `/speckit.plan` decision; the shape — an explicit ordered list — is fixed here, per Clarifications Session 2026-08-13.)
- **FR-002**: The output interval (wall-clock delay) between consecutive reasoning chunks MUST be **configurable** per template, and the configured interval MUST translate to an actual delay between the corresponding SSE frames so a consuming stall detector can observe it.
- **FR-003**: It MUST be possible to configure **different intervals for different gaps** between the reasoning chunks of a single template, so a long gap (stall) can be positioned at a specific point in the thinking phase.
- **FR-004**: Concatenating the streamed reasoning deltas of a chunked template MUST reconstruct the template's full reasoning text exactly — no loss, duplication, or reordering relative to the declared chunk order.
- **FR-005**: When a chunked-reasoning template also carries a text (`content`) answer, the text delta MUST be emitted **after** all reasoning chunks, preserving the existing reasoning→text ordering (`projects/game/fake-llm/service/handler.go:525-559`).
- **FR-006**: Chunking and intervals MUST apply **only to streaming** responses. A non-streaming response for a chunked template MUST return the full reasoning as a single value with no delays.
- **FR-007**: An existing template that declares a single reasoning string with no chunk/interval configuration MUST behave byte-for-byte identically to before this feature (all reasoning in the first delta, no delays) — full backward compatibility.

**Positionable permanent stall (US2)**

- **FR-008**: The permanent-stall behaviour (emit a frame, then block with the connection alive and no further data until the caller cancels the request, `handler.go:513-517`) MUST be positionable to begin **after a chosen reasoning chunk**, so a stall can occur mid-thinking rather than only after the very first delta.
- **FR-009**: A stalled stream MUST be unblockable only via caller (request-context) cancellation — identical to the existing `stall: true` contract; no timeout on the fake-llm side resolves it.
- **FR-010**: An existing template flagged with the simple `stall: true` and no chunked reasoning MUST behave exactly as before (stall after the first chunk) — backward compatible.

**Stall/fallback safety**

- **FR-011**: Any template that opts into a behaviour that can delay or hang a stream — i.e. declares a permanent stall, OR declares chunked reasoning with any inter-chunk interval — MUST be **excluded from the no-match random fallback pool**, so an unrelated turn can never stall or hang by accident. (Declaring chunked reasoning with **no** interval does not by itself exclude a template, since back-to-back chunks introduce no delay.) This generalizes the current Stall exclusion (`matcher.go:57-62`).

**Multi-message testdata files & reorganization (US3)**

- **FR-012**: The testdata loader MUST support a single file containing **multiple** message templates, in addition to the existing single-template-per-file shape, so test data can be grouped by scenario or module.
- **FR-013**: The loader MUST continue to support single-template files and the existing top-level `tools:` tool-config files unchanged — all three shapes (single message, multiple messages, tools) MUST coexist with unambiguous detection.
- **FR-014**: Template `name` uniqueness MUST be enforced across **all** files and **all** templates within multi-template files — a duplicate name, whether intra-file or inter-file, MUST fail startup validation (extends the current invariant, `projects/game/fake-llm/service/startup.go:18-39`).
- **FR-015**: The shipped testdata under `projects/game/fake-llm/service/testdata/` MUST be reorganized into scenario/module-based files. The merged template set (by Name, keywords, reasoning, text, tool_call, stall) after reorganization MUST be identical to the pre-reorganization set — reorganization changes file grouping only, not template content or matching behaviour.
- **FR-016**: The reorganization MUST NOT alter any template's `name`, and therefore MUST NOT alter the merged store's alphabetical sort or lowest-`Name`-first multi-match resolution (`matcher.go:34-41`).

**Validation**

- **FR-017**: Startup validation MUST reject malformed chunking/stall configuration — e.g. a declared chunk that would emit an empty reasoning delta, or a stall-after-chunk index that does not correspond to an actual chunk — so a broken template fails fast at startup rather than producing a degenerate stream.

**Observability (US1)**

- **FR-018**: The streaming handler MUST emit a structured log entry per reasoning chunk at emission time, carrying at least the chunk index and the configured delay applied before that chunk (or the stall position when the stream stalls), so test operators can verify the configured think-cadence / stall actually fired via signoz. This generalizes the existing `logSystemPrompts` observability pattern (`projects/game/fake-llm/service/handler.go`).

### Key Entities *(include if feature involves data)*

- **Reasoning Chunk**: one piece of a template's think content, declared explicitly as a list entry and emitted as a single `reasoning_content` SSE delta during streaming. A chunked template's reasoning is the ordered concatenation of its explicitly-listed chunks (FR-004). The legacy single-reasoning form remains a single string (one implicit chunk) for backward compatibility (FR-007).
- **Inter-Chunk Interval**: the configurable wall-clock delay the streaming handler waits between emitting two consecutive reasoning chunks. An interval above the consuming agent's idle timeout is what produces an observable think-interruption stall (US1.3).
- **Stall Position**: the point (after a chosen reasoning chunk) at which the permanent-stall behaviour begins. Generalizes the current "stall after the first chunk" to an arbitrary chunk within the thinking phase (US2).
- **Multi-Message File**: a testdata file carrying several message templates grouped by scenario/module, merged into the flat store alongside single-template and tools files (US3).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A chunked-reasoning template's streamed `reasoning_content` deltas concatenate to the exact full reasoning text in 100% of streamed responses (zero loss, duplication, or reordering).
- **SC-002**: The wall-clock gap between consecutive reasoning frames of a chunked template matches each configured interval in 100% of measured gaps (within normal scheduling tolerance), so consuming stall detectors observe the intended cadence.
- **SC-003**: With 045 deploy-config providing a controlled (short) agent idle timeout, a fake-llm template whose think interval exceeds that timeout triggers the agent's stream-stall detector during the thinking phase — the think-interruption scenario 044's large tests require becomes simulatable.
- **SC-004**: 100% of templates that do not opt into chunking/stall behave identically before and after this feature (byte-for-byte streaming and non-streaming responses), proving zero regressions for existing testdata and large tests.
- **SC-005**: After reorganization, the merged testdata template set is identical (by Name, keywords, reasoning, text, tool_call, stall) to the pre-reorganization set in 100% of templates — reorganization changes file grouping only.
- **SC-006**: A duplicate template `name` — whether within a multi-message file or across files — is rejected at startup in 100% of cases.
- **SC-007**: For 100% of streamed chunked-reasoning responses, fake-llm emits a structured log entry per chunk (index + configured delay / stall position), verifiable in signoz, so the configured think-cadence / stall is observable by test operators.

## Assumptions

- This feature is a **focused upgrade to fake-llm** (a test-only mock service). It does not touch the agent, the desktop, or production code paths; it only adds capabilities to the mock so large tests can drive them.
- Chunking applies to **reasoning (think) content only**. The user explicitly scoped it to "think 部分"; `content` (text) remains a single delta after all reasoning chunks (FR-005). Extending the mechanism to text is out of scope here.
- The consuming agent already accumulates incremental reasoning deltas correctly (`projects/game/agent/src/session-team.ts:855-865` joins `reasoningChunks`), so emitting multiple `reasoning_content` deltas is a fully protocol-valid wire shape — no agent change is needed for chunked reasoning to be parsed.
- The **045 deploy-config feature** ([`../045-deploy-config/spec.md`](../045-deploy-config/spec.md)) is the companion enabler that lets a large test deploy a controlled (short) agent idle timeout. This fake-llm feature provides the gap-producing mock that, combined with 045's controlled timeout, makes fast and reliable think-interruption large tests possible. The two features are independent but complementary.
- The chunked reasoning **content** is declared as an explicit list of chunk strings (Clarifications, Session 2026-08-13). The remaining configuration details — the exact field names, the **interval** model (FR-003 requires per-gap capability; whether a default/global interval is also offered), and the stall-position encoding — are `/speckit.plan` decisions. This spec fixes the **capabilities** and the backward-compatibility / safety invariants.
- The non-streaming response never chunks or delays (FR-006); chunking is meaningful only for the SSE streaming path. Large tests that need delays always use `stream: true`.
- The existing `tools:`-file detection (`message_store.go:163-180`) and single-message parsing (`message_store.go:132-147`) are preserved; the multi-message shape is added alongside them with unambiguous precedence (FR-013). Detection precedence among the three shapes is a plan-level detail.
- A test scenario that needs the permanent-stall shape continues to use the stall mechanism (now positionable, US2); a scenario that needs only a detectable gap uses a long-but-finite interval (US1.3). Both are first-class and the choice belongs to the test author.
- Reorganization (FR-015) is a pure file-regrouping with no template-content change; its correctness is verifiable by diffing the merged template set before vs. after. The concrete file grouping (which templates land in which scenario file) is a task-level decision guided by the existing in-file comments that already document each template's scenario.

## References *(mandatory per Constitution §I — Citation Provenance)*

### In-Repository Sources

- `projects/game/fake-llm/service/handler.go` — the streaming handler; `serveStreaming` (lines 484-521) emits the chunk sequence and blocks after the first chunk on `spec.Stall` (lines 513-517); `textStreamChunks` (lines 525-559) builds the role+reasoning / content / finish sequence this feature extends.
- `projects/game/fake-llm/service/message_types.go` — the `Message` struct (lines 30-37) carrying `Reasoning`, `Text`, `ToolCall`, `Stall`; the data model this feature extends with chunk/interval/stall-position fields.
- `projects/game/fake-llm/service/message_store.go` — `LoadFromFS` (lines 66-122) walks `testdata/` and parses each file as a single `Message` or a `tools:` file (`tryParseToolsFile`, lines 163-180); the loader this feature extends with multi-message files.
- `projects/game/fake-llm/service/startup.go` — `Validate` (lines 18-39) enforces the name-uniqueness / non-empty-keyword invariants; multi-message files must extend the same invariants.
- `projects/game/fake-llm/service/matcher.go` — `Match` (lines 26-72) excludes `Stall` templates from the random fallback pool (lines 57-62); FR-011 generalizes this to any hang-capable template.
- `projects/game/fake-llm/service/testdata/sample_stall.yaml` — the existing permanent-stall template (`stall: true`), the backward-compatibility anchor for FR-010.
- `projects/game/agent/src/session-team.ts:855-865` — the agent accumulates incremental `reasoning-delta` chunks (and joins `reasoningChunks`, line 1404), proving multi-chunk reasoning is a valid, already-supported wire shape.
- `projects/game/agent/src/llm.ts` — the agent's stream-stall idle timeout (`STREAM_IDLE_TIMEOUT_MS`); the consumer whose gap detection this feature's intervals are designed to exercise.

### Related Specifications

- [Feature 044 — LLM Stream Stall Recovery: Timeout Tuning & Partial Output Persistence](../044-llm-stall-recovery-fix/spec.md) — the stall-recovery feature whose large-test acceptance this unblocks; defines the idle-timeout default/min (FR-001) and the reasoning-model floor (FR-002/FR-003) that the chunked-think gaps must exercise.
- [`specs/044-llm-stall-recovery-fix/large-test-status.md`](../044-llm-stall-recovery-fix/large-test-status.md) — §2 (T012 blocker: no controlled idle window), §4 ("fake-llm has no recoverable-silence template"), §5 (resume plan: controlled config values + gap-producing fake-llm); the primary motivation.
- [Feature 045 — Deploy Config Support](../045-deploy-config/spec.md) — the companion enabler letting a large test deploy a controlled (short) agent idle timeout; combined with this feature enables fast think-interruption large tests.
- [Feature 043 — LLM Stream Stall Recovery](../043-llm-stream-stall-recovery/spec.md) — the original shipped stall mechanism fake-llm's `stall: true` was built for; this feature extends its simulation fidelity to the thinking phase.

### Official Documentation

- [OpenAI API Reference — Chat Completions (streaming)](https://platform.openai.com/docs/api-reference/chat/streaming) — the SSE streaming chunk shape fake-llm emulates, including incremental `delta` fields.
- [`reasoning_content` streaming (DeepSeek)](https://api-docs.deepseek.com/guides/reasoning_model) — documents a reasoning model streaming `reasoning_content` across multiple chunks before final `content`, the real-world behaviour chunked reasoning simulates.
