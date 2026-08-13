# Contract: Fake-LLM Streaming Chunk Sequence

**Feature**: [spec.md](../spec.md) | **Data model**: [data-model.md](../data-model.md) §3

This is the **wire-shape contract** for the SSE stream fake-llm emits for a chunked-reasoning / stall template. It fixes the exact frame sequence, role placement, delay semantics, and stall behavior so the consuming agent (`@langchain/openai` → `reasoning-delta` accumulation in `projects/game/agent/src/session-team.ts:855-865`) parses it as a normal reasoning-model turn. All sequences are OpenAI Chat Completions `chat.completion.chunk` SSE frames terminated by `data: [DONE]\n\n`.

## 1. Frame anatomy

Each data frame is a `completionResponse` with `object: "chat.completion.chunk"` and exactly one `choices[0]` carrying a `delta` (`assistantMessage`) and a `finish_reason` (`null` until the final frame). Only these `delta` field combinations occur:

| Delta kind | `role` | `reasoning_content` | `content` | `tool_calls` |
|---|---|---|---|---|
| First reasoning delta | `"assistant"` | `<chunk[0]>` | `""` (omitted) | — |
| Subsequent reasoning delta | `""` (omitted) | `<chunk[i]>` | `""` (omitted) | — |
| Content delta | `""` (omitted) | `""` (omitted) | `<text>` | — |
| Tool-call delta | `"assistant"` | — | — | `[…]` |
| Finish delta | `""` (omitted) | `""` (omitted) | `""` (omitted) | — |

(`role` appears ONLY on the first delta of a turn — required by the `@langchain/openai` parser; see existing `handler.go` frame-1 comment.)

## 2. Text-path sequence (chunked reasoning)

For a template with `reasoning_chunks = [c0, c1, …, c_{R-1}]`, `chunk_delays = [d0, …, d_{R-2}]` (missing ⇒ 0), optional `stall_after = K`, optional `text`:

```
[t=0]      frame: role="assistant", reasoning_content=c0        ; flush + log (FR-018)
           — if K == 0: BLOCK here until caller cancels; STOP —
[t=d0]     frame: reasoning_content=c1                           ; (slept d0, context-aware) flush + log
           — if K == 1: BLOCK here until caller cancels; STOP —
…
[t=d0+…+d_{R-2}] frame: reasoning_content=c_{R-1}                ; flush + log
           — if K == R-1: BLOCK here until caller cancels; STOP —
[next]     frame: content=text                                   ; (only if text != ""; see §4) flush + log
[next]     frame: finish_reason="stop"                           ; flush + log
           "data: [DONE]"
```

Properties:
- **FR-001/FR-004**: one `reasoning_content` delta per declared chunk, in order; concatenation `c0+c1+…+c_{R-1}` == full reasoning text.
- **FR-002/FR-003**: each gap `d_i` is an actual wall-clock sleep before `c_{i+1}`; gaps are independently configurable.
- **FR-005**: content delta follows all reasoning deltas.
- **FR-008/FR-009**: at `stall_after == K`, after flushing `c_K`, the handler blocks on `r.Context().Done()` only (no fake-llm timeout); no further frames are emitted.
- **Context-aware sleep**: if the caller cancels during a finite delay `d_i` (e.g. the agent's idle watchdog fires), the sleep aborts via `r.Context().Done()` and the handler returns — the half-stream is indistinguishable from an aborted client, which is the point.

## 3. Backward-compatible (non-chunked) text path — FR-007

For a template with single `reasoning` and no chunking/stall config, the sequence is the existing 3-frame `textStreamChunks` output, byte-for-byte:

```
frame: role="assistant", reasoning_content=<reasoning>
frame: content=<text>
frame: finish_reason="stop"
"data: [DONE]"
```

The new builder MUST produce identical bytes for this case (pinned by a byte-equality test, [data-model.md](../data-model.md) §3). The legacy `stall: true` (`stall_after` effective 0) blocks after the first frame, identical to `handler.go:513-517` today (FR-010).

## 4. Content-delta omission nuance (backward compat)

The current `textStreamChunks` emits the content delta **unconditionally**, even when `text == ""` (it emits `content: ""`). To guarantee byte-identical output for non-chunked templates, the new builder emits the content delta unconditionally on the **non-chunked** path. On the **chunked** path only, a reasoning-only template (`text == ""`) MAY omit the content delta (new behavior; a reasoning-only chunked template is new, so no regression). `/speckit.tasks` MUST pin both paths with byte-equality / frame-count assertions.

## 5. Tool-call path — unchanged

For a tool-call template (`spec.isToolCall()`), no reasoning/delays/stall apply. The sequence is the existing 2-frame `toolCallStreamChunks` (`handler.go:564-588`):

```
frame: role="assistant", tool_calls=[<call>]
frame: finish_reason="tool_calls"
"data: [DONE]"
```

Chunking fields on a tool-call template are rejected at validation ([template-config.md](template-config.md) §2, rule V5).

## 6. Non-streaming path — FR-006

Non-streaming (`stream: false`) is unaffected by chunking: one `chat.completion` JSON object whose `message.reasoning_content` is the **concatenation** of all reasoning chunks (joined into one string) and `message.content` is the `text`. No delays, no stall, no per-chunk logging.

## 7. Observability (FR-018 / SC-007)

For every streamed reasoning frame, the handler emits a structured `slog.Info` log entry carrying at minimum:

| Field | Value |
|---|---|
| `chunk_index` | 0-based reasoning-chunk index |
| `role_kind` | `"reasoning"` (also `"content"`/`"finish"`/`"tool_calls"` for those frames) |
| `delay_ms` | the delay applied before this frame (0 if none) |
| `stall_after` | the configured stall index, when the stream is about to stall |

These logs are verifiable in signoz (service `fake-llm`) and let a test operator confirm the configured cadence/stall actually fired — directly supporting [Feature 044](../../044-llm-stall-recovery-fix/large-test-status.md) §5 step 1's trace-inspection workflow.
