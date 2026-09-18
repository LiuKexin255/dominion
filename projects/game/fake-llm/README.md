# fake-llm

An OpenAI-compatible mock LLM service (`kind: stateless`, see
`projects/game/fake-llm/service.yaml`) used as **test infrastructure** by the
game agent's large tests. It serves `POST /v1/chat/completions` (OpenAI Chat
Completions wire shape, streaming via SSE) from template responses embedded
into the binary, so tests can drive the real agent pipeline against
deterministic, scriptable model behavior without a live LLM endpoint. See
`specs/046-fake-llm-think-chunking/plan.md` (Summary) and
`specs/046-fake-llm-think-chunking/spec.md` for the feature definition, and
`projects/game/testplan/README.md` for how the agent's large tests consume it.

## Key capabilities

- **Keyword matching**: a request matches the message template where ANY
  of its `keywords` occurs (case-insensitive substring) in the last `user`
  message; unmatched requests fall back to a random non-hang-capable template.
- **Streaming / non-streaming**: `stream:true` emits OpenAI-style SSE frames
  (`reasoning_content` deltas → `content` delta → `finish_reason` → `[DONE]`);
  the non-streaming path returns a single JSON completion.
- **Tool dispatch**: a `tool_call` template triggers a `tool_calls` response,
  and `tools:` entries respond to the follow-up tool-result turn, driving
  multi-step model→tool→execution chains.
- **Chunked reasoning**: `reasoning_chunks` streams the think content as
  multiple `reasoning_content` deltas with configurable inter-chunk output
  intervals (`chunk_delays`), so a gap longer than a consumer's idle timeout
  simulates a **think interruption**.
- **Stall simulation**: `stall:true` (legacy shorthand) or `stall_after:K`
  permanently blocks the stream after a chosen reasoning chunk until the
  caller cancels.
- **Embedded testdata**: templates live under
  `projects/game/fake-llm/service/testdata/`, baked into the binary via
  `//go:embed`, and load as single-message, multi-message (`messages:`), or
  `tools:` files.

## Testdata configuration format

The template config schema (field names, types, validation rules, precedence)
is the **author-facing contract**:

- `specs/046-fake-llm-think-chunking/contracts/template-config.md` — message/tool
  template fields, file shapes, and validation rules.
- `specs/046-fake-llm-think-chunking/contracts/streaming-sequence.md` — the SSE
  chunk sequence emitted for chunked/stall templates.

Shipped templates and a runnable validation guide:
`specs/046-fake-llm-think-chunking/quickstart.md`.

## Large-test exemption (Constitution VI)

Per `.specify/memory/constitution.md` Principle VI, service code must be
covered by both unit tests and a large test, **unless** the service can be
exempted by documenting the reason in its README:

> "对于一些因特殊原因无法进行大型测试（如服务无法自举），可以在 README 中说明可豁免此条规则。"

`fake-llm` claims that exemption:

- **It is test infrastructure, not a delivered service under test** — a mock
  LLM consumed by other services' large tests. A standalone large test for
  `fake-llm` would be circular (deploying a mock to test the mock) and adds no
  signal beyond transport-level unit tests (rationale in
  `specs/046-fake-llm-think-chunking/plan.md` Constitution Check Gate 5).
- **Its correctness is covered by real-transport unit tests**: streaming,
  chunking, delays, stall, and log emission are exercised through a real
  `httptest.Server` + `*http.Client` transport in
  `projects/game/fake-llm/service/handler_test.go` (e.g.
  `TestServeHTTP_RealServerSmoke`, `TestServeHTTP_ChunkedReasoningDelays`,
  `TestServeHTTP_ChunkedReasoningStallAfter`, `TestServeHTTP_ChunkedReasoningLogs`).
- **Its end-to-end consumption flows through Feature 044's large-test
  resume** (`specs/044-llm-stall-recovery-fix/large-test-status.md` §5): once
  045 deploy-config lands a controlled agent idle timeout, 044 re-authors the
  `agent-stall` suite to point at a `think-interrupt-gap` template and runs
  `guitar run` to full green — see `specs/046-fake-llm-think-chunking/quickstart.md`
  §"Downstream consumption".
