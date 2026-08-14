# Research: Fake-LLM Think Chunking & Testdata Reorganization

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-13

This document resolves the configuration-shape decisions the spec explicitly deferred to `/speckit.plan` (spec Assumptions), and records the rationale so `/speckit.tasks` can implement without re-litigating. Each decision cites the spec FR it satisfies and the current code it changes.

## Decision index

- [D1 — Chunk content specification](#d1) → explicit list `reasoning_chunks`
- [D2 — Inter-chunk interval model](#d2) → per-gap list `chunk_delays` (len N−1), missing = 0
- [D3 — Stall-position encoding](#d3) → `stall_after *int`; legacy `stall bool` is sugar for `stall_after:0`
- [D4 — Loader file-shape detection](#d4) → `tools:` > `messages:` > single; reject `tools:`+`messages:`
- [D5 — Chunking on a tool-call template](#d5) → validation error (fail-fast)
- [D6 — `reasoning` + `reasoning_chunks` coexistence](#d6) → mutually exclusive (validation error if both)
- [D7 — Backward-compat strategy for the embedded pin test](#d7) → preserve all 14+13; update count only if demonstration templates are added
- [D8 — No new dependencies / no agent change](#d8) → confirmed

---

## D1 — Chunk content specification: explicit list

**Decision**: A chunked template declares its reasoning as an explicit ordered list of strings under field `reasoning_chunks` (YAML/JSON). One list entry → one `reasoning_content` SSE delta.

**Rationale** (spec Clarifications 2026-08-13, Q1 → Option A): explicit entries give exact, author-controlled chunk boundaries; each gap can carry its own delay (FR-003); the stall position (FR-008) is a clean 0-based index into the list. Auto-split (single string + count/delimiter) was rejected because implicit boundaries make precise mid-thinking stall positioning harder and can split mid-word.

**Backward compatibility**: the existing single `reasoning` string field is unchanged and remains the default when `reasoning_chunks` is absent (FR-007). See D6 for mutual exclusion.

**Wire shape**: only the first reasoning delta carries `role:"assistant"` (matches the current `textStreamChunks` frame-1 pattern, `handler.go:525-559`); subsequent reasoning deltas carry only `reasoning_content`. The text (`content`) delta follows all reasoning deltas (FR-005).

**Alternatives considered**:
- *Single string + count N, auto-split evenly* — implicit boundaries, mid-word risk, cannot place a stall at an exact semantic point. Rejected.
- *Single string + delimiter* — inline-encoded boundaries; less readable than a list and couples content to a sentinel. Rejected.

## D2 — Inter-chunk interval model: per-gap list

**Decision**: Delays are declared as a parallel list `chunk_delays` of duration strings (e.g. `"2s"`, `"500ms"`). For N reasoning chunks there are N−1 gaps; `chunk_delays[i]` is the delay applied **before** emitting chunk `i+1` (i.e. after chunk `i`). The first chunk (chunk 0) is emitted immediately, like today. A missing/shorter list defaults missing entries to **0** (no delay); a list longer than N−1 is a validation error (D-validation).

Durations parse via Go `time.ParseDuration` (supports `"300ms"`, `"1.5s"`, `"2m"`, etc.).

**Rationale**: FR-003 requires per-gap capability (different intervals for different gaps). A parallel per-gap list is the direct encoding. A separate "global default interval" field is **not** added: it is redundant (set every gap to the same value to get a global cadence) and would create ambiguity with per-gap overrides. Spec Assumptions left "whether a default/global interval is also offered" open; this decision is **no global default** — per-gap only.

**Long-but-finite vs permanent stall**: a finite `chunk_delays[i]` above the consumer's idle timeout produces a detectable think-interruption gap (US1.3) — the stream eventually resumes unless the caller aborts. A **permanent** stall (block until caller cancels) is a separate mechanism (D3), not encoded as a duration sentinel (a duration-or-"stall" union type would be messy and error-prone in a typed config).

**Context-aware sleep**: the delay sleep MUST respect request-context cancellation (`select { case <-time.After(d): case <-r.Context().Done(): return }`), so when the agent's idle-timeout abort cancels the request mid-gap, fake-llm unblocks promptly instead of sleeping on.

**Alternatives considered**:
- *Single global interval* — violates FR-003 (cannot vary per gap). Rejected.
- *Global default + per-gap overrides* — redundant; rejected (see Rationale).
- *Duration-or-"stall" sentinel list* — union typing, conflates delay and stall. Rejected (stall is D3).

## D3 — Stall-position encoding: `stall_after *int`, legacy `stall` is sugar

**Decision**: Add `stall_after *int` (nullable; 0-based index of the reasoning chunk after which to permanently block). Effective stall position is computed as:

```
effectiveStallAfter(msg):
  if msg.StallAfter != nil: return msg.StallAfter   // explicit position wins
  if msg.Stall:             return 0                 // legacy sugar (stall after first chunk)
  return nil                                        // no stall
```

When the effective position is set, the streaming handler emits reasoning chunks `0..pos`, then blocks (no further data) until `r.Context()` is cancelled — identical to today's `stall:true` contract (`handler.go:513-517`), generalized to any chunk index (FR-008/FR-009).

**Rationale**: FR-010 requires existing `stall:true` (no chunked reasoning) to behave exactly as before (stall after the first delta). `stall:true` ≡ `stall_after:0` for the single-reasoning case, and ≡ `stall_after:0` for a chunked case (stall after chunk 0). Keeping the legacy `Stall bool` field preserves `sample_stall.yaml` and its pin test unchanged. `StallAfter` is the canonical generalization; `Stall` is documented sugar.

**Precedence**: `StallAfter` wins when both are set (an explicit position is more specific than the boolean shorthand). Validation does NOT reject setting both — `Stall:true` + `StallAfter:2` simply means "stall after chunk 2" (the bool is redundant but harmless). This avoids a confusing reject-rule for a benign redundancy.

**`responseSpec` change**: the current `responseSpec.Stall bool` becomes `responseSpec.StallAfter *int` (nil = no stall). `serveStreaming`'s `if spec.Stall && i == 0` check becomes `if spec.StallAfter != nil && i == *spec.StallAfter` (positioned against the reasoning-chunk index).

**Alternatives considered**:
- *Replace `stall bool` with `stall_after int`* — breaks `sample_stall.yaml` and its pin test. Rejected (FR-010).
- *Duration sentinel "stall" in `chunk_delays`* — union typing; rejected (D2).
- *Reject both-set* — adds a reject-rule for a harmless redundancy. Rejected.

## D4 — Loader file-shape detection: `tools:` > `messages:` > single

**Decision**: `LoadFromFS` (`message_store.go`) detects each file's shape by probing for top-level `tools:` and `messages:` keys via one combined probe struct:

```go
type fileShapeProbe struct {
    Tools    []*ToolConfig `json:"tools"    yaml:"tools"`
    Messages []*Message    `json:"messages" yaml:"messages"`
}
```

Precedence (FR-013 unambiguous coexistence):
1. Both `tools` and `messages` non-empty → **error** (ambiguous shape).
2. `tools` non-empty → **tools file** (existing behavior, `tryParseToolsFile`).
3. `messages` non-empty → **multi-message file** (new); each entry is already a parsed `*Message`.
4. Neither → **single-message file** (existing behavior, `parseMessage`).

A single-message file (top-level `name`/`keywords`/`reasoning`/...) has neither key, so its probe yields both nil → single-message path. Tools files are unaffected. Detection is deterministic and order-independent.

**Merge**: single-message files contribute 1 message; multi-message files contribute N; all merged into one flat slice sorted by `Name`; tools merged separately. Uniqueness enforced across all (FR-014).

**Rationale**: a combined probe is the minimal, unambiguous discriminator. Rejecting `tools:`+`messages:` in one file prevents the only ambiguous case (FR-013 edge "tools: files and multi-message files coexisting"). Keeping single-message and tools paths unchanged guarantees backward compatibility for existing files.

**Alternatives considered**:
- *Filename convention* (`*_tools.yaml`, `*_multi.yaml`)* — couples shape to filename, fragile. Rejected.
- *Allow `tools:`+`messages:` in one file* — ambiguous, against FR-013. Rejected.

## D5 — Chunking on a tool-call template: validation error

**Decision**: A `Message` that carries `tool_call` (a tool-call trigger) MUST NOT also declare `reasoning_chunks`/`chunk_delays`/`stall_after`. Setting both is rejected at startup validation (FR-017).

**Rationale** (spec Edge Case "Chunked reasoning combined with a tool-call response"): a tool-call response has no streamed reasoning — its streaming shape is role+tool_calls → finish (`toolCallStreamChunks`, `handler.go:564-588`). Chunking is meaningless there. Fail-fast at validation (consistent with the existing `Validate`/`ValidateTools` philosophy) is chosen over a silent no-op because a tool-call template with chunking is a config mistake the author should hear about.

## D6 — `reasoning` + `reasoning_chunks` coexistence: mutually exclusive

**Decision**: A `Message` MUST declare reasoning via **exactly one** of `reasoning` (single string) or `reasoning_chunks` (list). Setting both is a validation error.

**Rationale**: allowing both creates ambiguity over which wins. Single-string is the legacy/default form; the list is the chunked form; they are mutually exclusive in intent. Fail-fast avoids silent precedence surprises. (Existing templates keep `reasoning`; new chunked templates use `reasoning_chunks`.)

## D7 — Backward-compat strategy for the embedded pin test

**Decision**: The reorganization (FR-015) is a **pure file-regrouping**: all 14 existing messages and 13 existing tools MUST remain byte-identical (by Name, keywords, reasoning, text, tool_call, stall) and MUST continue to satisfy `TestNewMessageStore_LoadsEmbeddedSamples` (14) and `TestNewMessageStore_LoadsEmbeddedTools` (13) **without** changing those tests' assertions. SC-005 is verified by diffing the merged template set before vs. after reorganization.

New **demonstration** think-interrupt templates (part of US1 delivery, see [contracts/template-config.md](contracts/template-config.md) examples) are **additive** to the reorganization: they augment the set and require updating the pin test count (14 → 14+N) and sorted-name list. This addition is a separate action from the reorganization and does NOT weaken SC-005 (which governs the existing set's preservation).

**Rationale**: `message_store_test.go` is the single-source-of-truth pin (`projects/game/testplan/README.md §5`). Keeping the reorganization content-neutral makes SC-005 mechanically verifiable and isolates risk. The new templates (and their pin-test update) are co-delivered because they validate the chunking capability in the real embedded store and directly serve 044's resume plan.

## D8 — No new dependencies / no agent change

**Decision**: No new third-party Go dependencies. `time.ParseDuration`, `time.After`, `select` on context, `log/slog`, and `embed`/`yaml.v3`/`encoding/json` already in use cover everything. No agent, desktop, proto, or testplan changes.

**Rationale**: chunked reasoning is already a protocol-valid wire shape the agent accumulates (`session-team.ts:855-865`). The feature is localized to the fake-llm Go package + its testdata.

## Validation rule summary (feeds [data-model.md](data-model.md) and [contracts/template-config.md](contracts/template-config.md))

For each `Message` (after the per-file and merged shape is built), `Validate` enforces in this order (existing rules unchanged, new rules appended):

| # | Rule | Source |
|---|------|--------|
| (existing) | ≥1 message; each has ≥1 non-empty keyword; unique Names | `startup.go:18-39` |
| V1 | If `reasoning_chunks` set: every chunk non-empty (FR-017 empty-chunk edge) | new |
| V2 | `chunk_delays` length ≤ `len(reasoning_chunks)−1`; each parses via `time.ParseDuration` (FR-017) | new |
| V3 | `stall_after` (if set) is a valid index `0..len(reasoning_chunks)−1` (FR-017 out-of-range edge). For the legacy single-`reasoning` + `stall:true` case, len is implicitly 1 so index 0 is valid. | new |
| V4 | Not both `reasoning` and `reasoning_chunks` (D6) | new |
| V5 | Not `tool_call` together with `reasoning_chunks`/`chunk_delays`/`stall_after` (D5) | new |
| V6 | A file MUST NOT carry both `tools:` and `messages:` (D4) | new (loader) |

(FR-011 fallback-pool exclusion is a runtime matcher rule, not a startup validation — see [data-model.md](data-model.md) §matcher.)
