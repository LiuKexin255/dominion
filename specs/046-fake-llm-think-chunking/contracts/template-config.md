# Contract: Fake-LLM Template Config Schema

**Feature**: [spec.md](../spec.md) | **Decisions**: [research.md](../research.md) | **Data model**: [data-model.md](../data-model.md)

This is the **author-facing contract** for fake-llm testdata templates (`projects/game/fake-llm/service/testdata/`). It fixes the field names, types, validation rules, and precedence decided in [research.md](../research.md). Test authors write these fields; the loader (`message_store.go`) and validation (`startup.go`) enforce this contract at startup.

## 1. File shapes

A testdata file (`.yaml`/`.yml`/`.json`) has exactly one of three shapes, detected by top-level keys ([research.md D4](../research.md#d4)):

| Shape | Top-level key | Contains | Example files |
|---|---|---|---|
| Single message | `name` (+ `keywords`, …) | one message template | (legacy form; preserved for backward compat) |
| Multi-message | `messages:` | a list of message templates | `chat.yaml`, `saolei.yaml`, … |
| Tools | `tools:` | a list of tool-result configs | `saolei_tools.yaml`, … |

**Detection precedence**: `tools:` and `messages:` are mutually exclusive — a file with both is **rejected** (validation error V6). A file with neither key is a single-message file.

## 2. Message template fields

```yaml
name: <string>              # required; unique across ALL files (sorted, matched)
keywords: [<string>, ...]   # required; ≥1 non-empty; case-insensitive substring match
reasoning: <string>         # OPTIONAL legacy single-string reasoning (default form)
text: <string>             # OPTIONAL content answer (emitted after reasoning)
tool_call:                 # OPTIONAL tool-call trigger (mutually exclusive with chunking)
  name: <string>
  arguments: <map>
stall: true                # OPTIONAL legacy permanent-stall shorthand (sugar for stall_after:0)
reasoning_chunks: [<string>, ...]  # OPTIONAL explicit reasoning pieces (chunked form)
chunk_delays: [<duration>, ...]    # OPTIONAL inter-chunk delays (Go ParseDuration strings)
stall_after: <int>         # OPTIONAL 0-based chunk index after which to permanently block
```

### Field rules (validation)

| Rule | Detail | FR / Decision |
|---|---|---|
| `name` | non-empty, globally unique | existing; FR-014 |
| `keywords` | ≥1, none empty | existing |
| `reasoning` ⊕ `reasoning_chunks` | exactly one (both set → error) | D6 |
| `reasoning_chunks` entries | every entry non-empty (no empty-delta chunks) | V1 / FR-017 |
| `chunk_delays` | length ≤ `len(reasoning_chunks) − 1`; each parses via `time.ParseDuration` | V2 / FR-017 |
| `chunk_delays[i]` | delay applied **before** emitting `reasoning_chunks[i+1]`; missing entries default to 0 | D2 / FR-002/FR-003 |
| `stall_after` | if set, `0 ≤ stall_after ≤ len(reasoning_chunks) − 1` (for legacy single-`reasoning`+`stall:true`, len is implicitly 1) | V3 / FR-017 |
| `tool_call` ⊕ chunking | `tool_call` MUST NOT coexist with `reasoning_chunks`/`chunk_delays`/`stall_after` | V5 / D5 |

### Effective-value computation (applies at serve time)

- **Reasoning streamed** = `reasoning_chunks` if non-empty, else `[reasoning]` if `reasoning != ""`, else `[]` (no reasoning).
- **Stall position** = `stall_after` if set, else `0` if `stall: true`, else *none*. When set, the stream blocks after the reasoning chunk at that index until the caller cancels.

### Duration strings

`chunk_delays` entries are [Go `time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) strings: `"500ms"`, `"2s"`, `"1.5s"`, `"90s"`, `"2m"`. A long finite value (e.g. `"90s"`) above the consuming agent's idle timeout produces a detectable think-interruption gap (US1.3); a permanent block uses `stall_after` instead (US2).

## 3. Worked examples

### 3.1 Legacy single-reasoning (unchanged, FR-007)

```yaml
name: greeting
keywords: [hello, hi]
reasoning: "The user is greeting me, I should respond warmly."
text: "Hello! How can I help you today?"
```

Behavior: byte-identical to before this feature (one reasoning delta, one content delta, finish).

### 3.2 Legacy stall (unchanged, FR-010)

```yaml
name: stall-mid-reasoning
keywords: [stall now]
reasoning: "The user asked me to simulate a stream stall..."
text: "This text must never arrive..."
stall: true
```

Behavior: emit role+reasoning delta, then block until caller cancels (sugar for `stall_after: 0`).

### 3.3 Think interruption via long finite gap (US1.3) — NEW

```yaml
name: think-interrupt-gap
keywords: [think interrupt gap]
reasoning_chunks:
  - "Analyzing the board state."
  - "Evaluating candidate moves."
  - "Finalizing the safest move."
chunk_delays: ["1s", "90s"]   # 1s before chunk 1; 90s before chunk 2 (> idle timeout → think interruption)
text: "Placing the flag at (3,4)."
```

Behavior: chunk 0 (role+reasoning) immediately → 1s gap → chunk 1 (reasoning) → 90s gap (agent's idle watchdog fires here) → chunk 2 → content → finish. Concatenating the three reasoning deltas yields the full reasoning (FR-004).

### 3.4 Think interruption via permanent stall (US2) — NEW

```yaml
name: think-interrupt-stall
keywords: [think interrupt stall]
reasoning_chunks:
  - "Starting to reason about the request."
  - "Going deeper into analysis."
chunk_delays: ["1s"]          # 1s before chunk 1
stall_after: 1                # permanently block after chunk 1 (mid-thinking)
text: "This answer never arrives."   # never emitted (stall before content)
```

Behavior: chunk 0 → 1s gap → chunk 1 → **permanent block** (no further data) until caller cancels. The content/finish never arrive.

### 3.5 Healthy reasoning cadence — no stall (US1 edge case) — NEW

```yaml
name: think-healthy-cadence
keywords: [think healthy]
reasoning_chunks:
  - "Step one."
  - "Step two."
  - "Step three."
chunk_delays: ["200ms", "200ms"]   # short gaps, all below any idle timeout
text: "Done."
```

Behavior: proves no false stall fires for a reasoning model streaming at a normal cadence.

### 3.6 Multi-message file (US3)

```yaml
# chat.yaml — basic chat responses grouped together
messages:
  - name: greeting
    keywords: [hello, hi]
    reasoning: "The user is greeting me, I should respond warmly."
    text: "Hello! How can I help you today?"
  - name: chat-only
    keywords: [chat, conversation]
    reasoning: "Responding with text only, no tools needed."
    text: "Sure, let's chat!"
  - name: farewell
    keywords: [bye, goodbye, see you]
    reasoning: "The user is saying goodbye."
    text: "Goodbye! Have a great day!"
```

All three are merged into the flat store exactly as if they had been three single-message files (FR-012/FR-014).

## 4. New demonstration templates (additive — D7)

The three NEW templates in §3.3/§3.4/§3.5 ship in `testdata/stall_recovery.yaml` alongside the existing `stall-mid-reasoning`. They raise the embedded message count from 14 → 17; the pin test (`TestNewMessageStore_LoadsEmbeddedSamples`) is updated accordingly. Their exact `name`/`keywords`/content are fixed in `tasks.md` T012 (`think-interrupt-gap`, `think-interrupt-stall`, `think-healthy-cadence`) but MUST follow the shapes above and MUST be excluded from the random fallback pool (FR-011) because they declare delays/stall.
