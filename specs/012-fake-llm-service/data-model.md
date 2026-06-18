# Data Model: Fake LLM Service for Large-Test Integration

**Updated**: 2026-06-18

## Fake LLM Service

**Purpose**: Standalone HTTP service that accepts OpenAI Chat Completions requests and returns deterministic response templates for large tests.

**Fields / configuration**:

- `listen_address`: HTTP bind address/port for the service.
- `template_files`: JSON/YAML files loaded at startup.
- `messages`: the merged, alphabetically-sorted-by-name set of response messages loaded from all configured files.

**Validation rules**:

- Startup fails if a template file cannot be read or parsed.
- Startup fails if any file has empty or missing `match_keywords`.
- Startup fails if a `name` is duplicated across merged files.
- Startup fails if the merged set contains fewer than 1 message.
- API key and model values are accepted but ignored.
- Service holds no per-request mutable state. Only the read-only merged message set persists in memory.

## Data File

**Purpose**: External test-authored JSON/YAML configuration loaded once at fake-service startup.

**Fields**:

- `messages`: flat list of `ResponseMessage` entries. There is no `groups` wrapper.

**Validation rules**:

- File must parse as JSON or YAML based on extension or parser fallback.
- Multiple files are merged into one flat set of messages.
- The merged set must contain at least 1 message for the service to start.

## Response Message

**Purpose**: A single named message template returned by the fake service when its keywords match a request.

**Fields**:

- `name`: globally unique identifier for this message. Duplicate names across merged files cause startup failure.
- `match_keywords`: non-empty array of keyword strings. Empty or missing `match_keywords` causes startup failure.
- `reasoning`: reasoning/thinking text emitted as `delta.reasoning_content` in streaming mode. May be empty.
- `text`: assistant text emitted as `delta.content` in streaming mode and as message content in non-streaming mode. May be empty.

**Relationships**:

- Belongs to the merged flat set, sorted alphabetically by `name`.
- This sort order is the deterministic tiebreak when multiple messages match a single request.

**Validation rules**:

- After merge the set is sorted alphabetically by `name`. This is used as the within-request tiebreak.
- Matching is per-request stateless: a message matches if any of its `match_keywords` is a case-insensitive substring of the request's last user message text.

## Chat Completion Request

**Purpose**: OpenAI-compatible request consumed by fake service.

**Fields used**:

- `messages`: request messages; only the text of the last `role: "user"` message (case-insensitive role) is used for keyword matching. Historical/context messages are not considered.
- `stream`: selects SSE streaming mode when true; non-streaming JSON when false or omitted.
- `model`: accepted and ignored.

**Validation rules**:

- Missing or empty `messages` behaves like no match (triggers random fallback).
- Unknown fields are ignored.
- Authorization header is accepted but not validated.

## Match Selection (stateless)

Each request is matched independently. There is no cross-request state.

```text
For each request:
  1. Extract the text of the last `role: "user"` message (case-insensitive role).
  2. A message matches if ANY of its `match_keywords` is a case-insensitive substring of that text.
  3. If one or more messages match → return the message whose `name` sorts first alphabetically.
  4. If ZERO messages match → return a uniform random message from the full merged set.
     The service logs a WARN line with the unmatched snippet and chosen message name.
     Random fallback is valid only for test scenarios that do not assert on response content.
```

## Compatibility Invariants

- The fake service never changes agent request shape; it speaks only the OpenAI-compatible HTTP boundary.
- The agent service keeps using `createAgent`, middleware, `ChatOpenAI`, `streamEvents`, and checkpointing.
- Large tests assert externally visible frames and message history rather than internal fake-service state.
