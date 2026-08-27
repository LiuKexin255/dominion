# fake-llm (dsh-demo)

An OpenAI chat-completions compatible mock LLM service (`kind: stateless`,
see `experimental/dsh/demo/fake-llm/service.yaml`) used as **test
infrastructure** by the dsh chat demo (`specs/047-dsh-chat-demo/`). The
demo's agent service embeds the official `@deepseek-ai/dsh-llm-deepseek`
adapter, which points its `baseURL` at this service
(`dominion:///dsh-demo/fake-llm:8080`, resolved at runtime via Dominion
service discovery), so the whole demo chain runs against deterministic,
scriptable model behavior with zero external LLM/network dependency.

## Endpoints

- `POST /v1/chat/completions` — OpenAI Chat Completions wire shape.
  `stream:true` emits the SSE frame sequence the dsh adapter requires
  (role delta with explicit empty content → content delta carrying the
  template text in full → finish delta with `finish_reason:"stop"` +
  usage → `data: [DONE]`); `stream:false` returns a single JSON
  completion. The request's `model` field and the adapter's
  `authorization` / `x-deepseek-harness-*` / attribution headers are
  ignored (tolerated, never validated). Wire contract:
  `specs/047-dsh-chat-demo/contracts/fake-llm-wire.md`.
- `GET /health` — liveness probe, returns 200 "ok".

## Matching semantics

Templates match at the three priorities of
`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3:

- **Multi-turn condition templates** (declare `history_keywords` and/or
  `min_turn > 1`): win when every condition holds — the keyword
  condition on the LAST `user` message (vacuous for a multi-turn
  template declaring no keywords), EVERY history keyword hitting some
  message before the last user message, and the user-message count
  reaching `min_turn` (default 1). Conflicts resolve to the template
  declaring more conditions, then the lowest name.
- **Pure keyword templates**: ANY of a template's `keywords` occurs
  (case-insensitive substring) in the LAST `user` message; ties break
  by lowest template name.
- **Deterministic fallback**: an unmatched request returns the unique
  pure fallback template (`farewell`, empty keywords) directly; the
  same request always yields the same reply.

Template schema and authoring contract:
`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md`. Shipped
templates live under `experimental/dsh/demo/fake-llm/service/testdata/`,
baked into the binary via `//go:embed`.

## Large-test exemption (Constitution VI)

Per `.specify/memory/constitution.md` Principle VI, service code must be
covered by both unit tests and a large test, **unless** the service can
be exempted by documenting the reason in its README:

> "对于一些因特殊原因无法进行大型测试（如服务无法自举），可以在 README 中说明可豁免此条规则。"

`fake-llm` claims that exemption, following the precedent of
`projects/game/fake-llm/README.md` §Large-test exemption:

- **It is test infrastructure, not a delivered service under test** — a
  mock LLM consumed by the demo's large tests. A standalone large test
  for `fake-llm` itself would be circular (deploying a mock to test the
  mock) and adds no signal beyond transport-level unit tests.
- **Its correctness is covered by real-transport unit tests**: the SSE
  frame sequence, streaming/non-streaming shapes, header tolerance, and
  fallback determinism are exercised through a real `httptest.Server` +
  `*http.Client` transport in
  `experimental/dsh/demo/fake-llm/service/handler_test.go`
  (e.g. `TestServeHTTP_Streaming`, `TestServeHTTP_FallbackAndDeterminism`).
- **Its end-to-end consumption flows through the demo's testplan** —
  fake-llm is deployed as a dependency service of
  `experimental/dsh/demo/testplan/` and every chat round-trip assertion
  there (`specs/047-dsh-chat-demo/tasks.md` T018-T023) is transitively
  an assertion on this service.
