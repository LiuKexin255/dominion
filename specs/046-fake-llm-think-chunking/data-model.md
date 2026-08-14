# Data Model: Fake-LLM Think Chunking & Testdata Reorganization

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-13 | **Research**: [research.md](research.md) | **Contracts**: [contracts/](contracts/)

This document specifies the data-level changes to the fake-llm Go package. It is the authoritative source for `/speckit.tasks` implementation; the config-field contract and validation rules live in [contracts/template-config.md](contracts/template-config.md) and the wire-shape contract in [contracts/streaming-sequence.md](contracts/streaming-sequence.md). Field names are fixed here (decided in [research.md](research.md)).

All file references are repository-relative. Existing structs are shown with their current source.

---

## 1. `Message` — template config model

**Source**: `projects/game/fake-llm/service/message_types.go:30-37` (current).

### Current fields (unchanged)

| Field | Type | JSON / YAML key | Notes |
|---|---|---|---|
| `Name` | `string` | `name` | uniqueness key (sorted, matched) |
| `Keywords` | `[]string` | `keywords` | ≥1 non-empty (existing invariant) |
| `Reasoning` | `string` | `reasoning` | legacy single-string reasoning; default form |
| `Text` | `string` | `text` | the `content` answer |
| `ToolCall` | `*ToolCall` | `tool_call,omitempty` | tool-call trigger (mutually exclusive with streamed reasoning use) |
| `Stall` | `bool` | `stall,omitempty` | legacy permanent-stall shorthand (sugar for `stall_after:0`) |

### New fields (FR-001/FR-002/FR-003/FR-008)

| Field | Type | JSON / YAML key | Semantics |
|---|---|---|---|
| `ReasoningChunks` | `[]string` | `reasoning_chunks,omitempty` | Explicit ordered list of reasoning pieces. When non-empty, the streaming response emits one `reasoning_content` delta per entry (FR-001). Mutually exclusive with `Reasoning` (D6). |
| `ChunkDelays` | `[]string` | `chunk_delays,omitempty` | Duration strings (Go `time.ParseDuration`, e.g. `"2s"`, `"500ms"`). `ChunkDelays[i]` is the delay before emitting `ReasoningChunks[i+1]`. Length ≤ `len(ReasoningChunks)−1`; missing entries default to 0 (D2). |
| `StallAfter` | `*int` | `stall_after,omitempty` | 0-based index of the reasoning chunk after which to permanently block (FR-008). `nil` = no explicit position. |

### Effective values (computed, not stored)

```
// effectiveReasoning: the ordered reasoning pieces to stream.
effectiveReasoning(msg):
  if len(msg.ReasoningChunks) > 0: return msg.ReasoningChunks
  if msg.Reasoning != "":          return [msg.Reasoning]   // single delta (legacy)
  return []                                                 // no reasoning

// effectiveStallAfter: nil = no stall; else the chunk index after which to block.
effectiveStallAfter(msg):
  if msg.StallAfter != nil: return msg.StallAfter
  if msg.Stall:             return &zero                    // legacy sugar
  return nil
```

`ChunkDelays` is parsed to `[]time.Duration` by a helper `parseDelays([]string) ([]time.Duration, error)` used by both `Validate` (fail-fast) and the streaming builder. Parsing is deterministic and idempotent.

### Relationships

- `Reasoning` ⊕ `ReasoningChunks`: exactly one (D6). `Text` is independent (a chunked template may also carry a `text` answer, emitted after all reasoning deltas — FR-005; or no `text`, for reasoning-only responses).
- `ToolCall` ⊕ (`ReasoningChunks` | `ChunkDelays` | `StallAfter`): mutually exclusive (D5). A tool-call trigger has no streamed reasoning.
- `Stall` ⊕ `StallAfter`: may coexist (`StallAfter` wins, D3); normally only one is set.

---

## 2. `responseSpec` — handler-internal response description

**Source**: `projects/game/fake-llm/service/handler.go:173-193` (current struct + `specFromMessage`).

### Current → new

| Current field | New field | Change |
|---|---|---|
| `Reasoning string` | `Reasoning []string` | becomes the ordered reasoning pieces (effective list; legacy single string wrapped as a 1-element slice) |
| `Text string` | `Text string` | unchanged |
| `ToolCall *ToolCall` | `ToolCall *ToolCall` | unchanged |
| `Stall bool` | `StallAfter *int` | nil = no stall; else chunk index after which to block |
| — | `Delays []time.Duration` | parsed inter-chunk delays (len = len(Reasoning)−1, missing ⇒ 0) |

### Mapping (`specFromMessage`)

```
specFromMessage(msg):
  reasoning := effectiveReasoning(msg)        // []string
  delays    := parseDelays(msg.ChunkDelays)   // already validated at load
  stallAfter := effectiveStallAfter(msg)      // *int
  return responseSpec{
    Reasoning: reasoning, Delays: delays, Text: msg.Text,
    ToolCall: msg.ToolCall, StallAfter: stallAfter,
  }
```

`specFromTool` (tools branch) is unchanged in shape: it builds a `responseSpec` with `ToolCall`/`Text` and **no** reasoning/delays/stall (a tool-result response never streams reasoning). Its `Reasoning` is empty, `Delays` nil, `StallAfter` nil.

`isToolCall()` (`s.ToolCall != nil`) is unchanged.

---

## 3. Streaming chunk sequence & loop

**Source**: `projects/game/fake-llm/service/handler.go:484-588` (`serveStreaming`, `textStreamChunks`, `toolCallStreamChunks`).

### Chunk builders (text path)

The current fixed 3-frame `textStreamChunks` is replaced by a builder that produces the sequence described in [contracts/streaming-sequence.md](contracts/streaming-sequence.md):

```
buildTextChunks(respID, now, spec):
  frames := []
  reasoning := spec.Reasoning                      // []string, len R (R≥1 for text path)
  // frame 0: role + first reasoning piece
  frames += { Delta: {Role:"assistant", ReasoningContent: reasoning[0]} }
  // frames 1..R-1: subsequent reasoning pieces (role omitted)
  for i in 1..R-1:
    frames += { Delta: {ReasoningContent: reasoning[i]} }
  // content delta (only when spec.Text != "")
  if spec.Text != "": frames += { Delta: {Content: spec.Text} }
  // finish delta
  frames += { Delta: {}, FinishReason: "stop" }
  return frames
```

- When `R == 1` and `spec.Delays` empty and `spec.StallAfter == nil`, the frame list is byte-identical to today's `textStreamChunks` output (FR-007 backward compatibility).
- A reasoning-only template (`Text == ""`) omits the content delta but still emits finish (mirrors current behavior when `spec.Text` is empty — current code always emits the content delta with `Content: ""`; the new builder MUST preserve that exact shape for the no-chunking case, i.e. emit an empty content delta exactly as today). **Backward-compat note**: the current `textStreamChunks` emits the content delta unconditionally even when `spec.Text == ""`. To guarantee byte-identical output for non-chunked templates, the builder emits the content delta unconditionally for the **non-chunked** path, and only for the **chunked** path may omit it when `Text == ""` (a new chunked reasoning-only template is new behavior, not a regression). `/speckit.tasks` MUST pin this with a byte-equality test against pre-change output for the existing templates.

### `serveStreaming` loop (text + tool-call)

The loop generalizes from "write each frame, flush, stall-after-first-if-flagged" to a context-aware loop:

```
serveStreaming(w, r, spec, respID):
  ... headers, WriteHeader, flusher check (unchanged) ...
  frames := buildFrames(spec, respID, now)   // text or tool-call frames
  reasoningCount := len(spec.Reasoning)      // 0 for tool-call path
  stallAfter := spec.StallAfter              // *int, nil = no stall

  for i, frame := range frames:
    // (1) apply the configured delay BEFORE this frame when this frame is a
    //     reasoning chunk index >= 1 (delay[i-1] precedes reasoning chunk i)
    if isReasoningChunk(i) && reasoningIndex(i) >= 1:
      d := delayFor(reasoningIndex(i))       // 0 if out of range / not set
      if d > 0:
        logChunkDelay(reasoningIndex(i), d)  // FR-018 (before sleeping)
        select {
          case <-time.After(d):
          case <-r.Context().Done(): return    // caller abort mid-gap
        }
    // (2) write + flush the frame
    writeFrame(w, frame); flusher.Flush()
    // (3) FR-018: log the chunk emission (index, role-kind, delay applied)
    logChunkEmission(i, ...)
    // (4) permanent stall after the configured reasoning chunk
    if stallAfter != nil && isReasoningChunk(i) && reasoningIndex(i) == *stallAfter:
      <-r.Context().Done()                     // block until caller cancels (FR-009)
      return

  write "[DONE]"; flusher.Flush()
```

Key properties:
- **Indexing**: reasoning chunks occupy frame indices `0..R−1`; the content delta (if any) and finish delta follow. `reasoningIndex(frameIndex)` maps a frame index to its reasoning-chunk index for `0..R−1` and is undefined thereafter. The delay and stall checks only apply to reasoning frames.
- **Context-aware delay** (D2): a long finite delay during which the caller aborts unblocks promptly via `r.Context().Done()`.
- **Stall unblock** (FR-009): the permanent block waits ONLY on `r.Context().Done()`; no fake-llm-side timeout.
- **Tool-call path**: `reasoningCount == 0`, so no delays/stalls apply; the loop reduces to writing the 2 tool-call frames + `[DONE]` exactly as today (`toolCallStreamChunks`).

### Observability (FR-018)

`logChunkEmission` / `logChunkDelay` emit `slog.Info` entries with structured fields: `chunk_index`, `role_kind` (`"reasoning"`/`"content"`/`"finish"`/`"tool_calls"`), `delay_ms` (the delay applied before this chunk; 0 if none), and on stall `stall_after` (the index). This mirrors the existing `logSystemPrompts` pattern (`handler.go:265-273`) and is verifiable via signoz (SC-007).

---

## 4. Loader — multi-message file detection

**Source**: `projects/game/fake-llm/service/message_store.go:66-180` (`LoadFromFS`, `tryParseToolsFile`, `parseMessage`).

### Detection (D4)

`tryParseToolsFile` is generalized into `detectAndParse(data, path)` using the combined probe `fileShapeProbe{Tools, Messages}`:

```
detectAndParse(data, path) (messages []*Message, tools []*ToolConfig, err error):
  probe := decodeInto(fileShapeProbe, data, ext(path))
  if len(probe.Tools) != 0 && len(probe.Messages) != 0:
     return error("file %s declares both tools: and messages: — ambiguous")   // V6
  if len(probe.Tools) != 0:
     return nil, probe.Tools, nil                          // tools file (existing path)
  if len(probe.Messages) != 0:
     return probe.Messages, nil, nil                       // multi-message file (NEW)
  // neither key → single message
  msg := decodeInto(Message, data, ext(path))
  return [msg], nil, nil                                   // single-message file (existing)
```

`LoadFromFS` merges the returned `messages`/`tools` across all files exactly as today (flat slices, sorted by `Name`), then runs `Validate` + `ValidateTools`. Multi-message file entries are already decoded `*Message` values; each is subject to the same validation.

### Backward compatibility

- A current single-message file (`name:...` at top level) → probe yields both nil → single-message path, identical output.
- A current `tools:` file → `tools` non-empty → tools path, identical output.
- No existing file declares `messages:`, so no existing file changes shape.

---

## 5. Validation — startup invariants

**Source**: `projects/game/fake-llm/service/startup.go:18-72` (`Validate`, `ValidateTools`).

The existing `Validate` rules (≥1 message, ≥1 non-empty keyword per message, unique Names) are unchanged and run first. New rules V1–V5 (from [research.md](research.md) §validation summary) are appended per message. Rule V6 (no `tools:`+`messages:`) is enforced in the loader (`detectAndParse`) before merging. Validation errors include the offending `Name` and file path so a broken template is locatable.

---

## 6. Matcher — fallback-pool exclusion (FR-011)

**Source**: `projects/game/fake-llm/service/matcher.go:57-62` (current `Stall` exclusion).

The random-fallback pool filter changes from `if m.ToolCall == nil && !m.Stall` to exclude any template that can delay/hang:

```
isHangCapable(m):
  return m.Stall || m.StallAfter != nil || (len(m.ReasoningChunks) > 0 && hasNonZeroDelay(m))

pool := [m for m in messages if !m.ToolCall && !isHangCapable(m)]
```

- A chunked template with **no** delays is NOT excluded (back-to-back chunks introduce no delay — FR-011).
- `hasNonZeroDelay` checks whether any parsed `ChunkDelays` entry is > 0.
- Empty-pool fallback (`pool := messages` when all excluded) is retained to avoid `IntN(0)` panic, matching today's safety.

---

## 7. Testdata reorganization (FR-015) — target layout

Existing 17 `sample_*` files → 8 scenario/module-based files. **Content is preserved verbatim** (only file grouping changes); SC-005 verified by diff. New demonstration think-interrupt templates (D7) are additive and co-located in `stall_recovery.yaml`.

| Target file | Shape | Templates (current source) |
|---|---|---|
| `testdata/chat.yaml` | `messages:` | greeting, chat-only, farewell (was `sample_greeting.yaml`, `sample_chat.yaml`, `sample_farewell.json`) |
| `testdata/stall_recovery.yaml` | `messages:` | stall-mid-reasoning (was `sample_stall.yaml`) **+ new** think-interrupt-gap, think-interrupt-stall, think-healthy-cadence |
| `testdata/saolei.yaml` | `messages:` | saolei-start, saolei-remain, saolei-single-op, saolei-structural-stop |
| `testdata/saolei_tools.yaml` | `tools:` | saolei tool-result responses (was `sample_saolei_tools.yaml`, unchanged content) |
| `testdata/operation.yaml` | `messages:` | mouse-trigger (was `sample_mouse_trigger.yaml`) |
| `testdata/operation_tools.yaml` | `tools:` | mouse/keyboard tool-result responses (was `sample_tools.yaml`, unchanged content) |
| `testdata/planner.yaml` | `messages:` | planner-memory-add, init-instruction, compact-instruction, compress-planner-summary, compress-player-summary |
| `testdata/planner_tools.yaml` | `tools:` | planner tool-result responses (was `sample_planner_tools.yaml`, unchanged content) |

**Pin-test impact** (D7): the existing 14 messages + 13 tools remain → `TestNewMessageStore_LoadsEmbeddedSamples` (14) and `TestNewMessageStore_LoadsEmbeddedTools` (13) assertions for existing templates stay valid. Adding the 3 new demonstration templates raises the message count to 17; the pin test's `len(got) != 14` and sorted-name list MUST be updated to 17 (additive). The exact new-template names are fixed in `tasks.md` T012 (`think-interrupt-gap`, `think-interrupt-stall`, `think-healthy-cadence`); the field shapes are binding in [contracts/template-config.md](contracts/template-config.md) §3.3–§3.5.

**Sorting invariant** (FR-016): no template's `name` changes, so the merged store's alphabetical sort and lowest-`Name`-first multi-match are unchanged.

---

## 8. Non-streaming path (FR-006)

**Source**: `projects/game/fake-llm/service/handler.go:401-434` (`serveNonStreaming`).

Unchanged in mechanism. For a chunked-reasoning template, the non-streaming response's `message.reasoning_content` is the **concatenation** of `ReasoningChunks` (joined into one string), with no delays and no chunking. This is implemented by reading `effectiveReasoning(msg)` and joining (e.g. `strings.Join(spec.Reasoning, "")`) for the non-streaming `assistantMessage.ReasoningContent` field. No delays, no stall, no per-chunk logging on this path.
