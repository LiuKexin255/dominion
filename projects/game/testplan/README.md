# game agent testplan

This directory holds the large-test (`go_largetest`) targets that exercise the
real game agent end-to-end (gateway → session → proxy → `agent_test` →
`fake-llm`) via the WebSocket + HTTP surface. The plan is orchestrated by
`guitar` through `system_test.yaml`.

## 1. Why a standalone fake-llm service

`fake-llm` replaces the old in-process `FakeLlmAdapter`. It is a separate Go
HTTP service (`projects/game/fake-llm`) deployed alongside the `agent_test`
artifact inside the same test deployment (`deploy_agent.yaml`).

Running the fake as a standalone service — instead of a TypeScript adapter
injected into the agent process — means the **real** `AgentAdapterImpl` and
the full LangChain pipeline (`createAgent` → `streamEvents`) run unmodified in
tests. The agent talks to `fake-llm` over plain HTTP, exactly as it would talk
to a real OpenAI endpoint in production. This catches integration regressions
(provider wiring, streaming shape, reasoning extraction, frame ordering) that
an in-process adapter cannot.

## 2. How the agent_test resolver-aware provider reaches fake-llm

The `agent_test` test bootstrap does **not** hard-code a hostname. It uses the
CORE resolver to discover `fake-llm` at runtime:

1. The bootstrap imports `createResolver()` from `@dominion/common-js-resolver`
   (the CORE resolver, **not** the gRPC plugin). Production gRPC name
   resolution uses a different package; the test bootstrap only needs to turn
   a fixed target into a `host:port` string.
2. `buildResolverAwareChatModel(resolver)` resolves the fixed target
   `dominion:///game/fake-llm:8080` against the Dominion service registry and
   reads back one or more `host:port` endpoints.
3. It builds `http://<endpoint>/v1` as the `baseURL` for `ChatOpenAI` and
   throws if the resolver returns no endpoints.
4. The real pipeline (`AgentAdapterImpl` → `createAgent` → `streamEvents`)
   then calls `fake-llm` via plain HTTP, streaming `reasoning_content` then
   `content` exactly like an OpenAI-compatible provider.

Because discovery goes through the registry, no `OPENAI_BASE_URL` or
platform-reserved env is set on the `agent_test` artifact, and `fake-llm` is
internal-only (no `http:` ingress in `deploy_agent.yaml`).

## 3. Data file format

Sample messages live in `projects/game/fake-llm/service/testdata/` and are
embedded into the `fake-llm` binary via `//go:embed`. Each file is JSON or
YAML and decodes into:

```yaml
name: greeting
keywords:
  - hello
  - hi
  - greetings
reasoning: "The user is greeting me, I should respond warmly."
text: "Hello! How can I help you today?"
```

Fields:

- `name` — unique identifier; also the alphabetical tiebreaker when multiple
  messages match (see §4).
- `keywords` — case-insensitive substring triggers. At least one non-empty
  keyword is required; duplicates across files are allowed, an empty string
  element is rejected at startup.
- `reasoning` — the thinking-frame content returned to the agent.
- `text` — the response content returned to the agent.

The shipped samples are:

| file                      | name      | keywords                 | reasoning                                  | text                                |
|---------------------------|-----------|--------------------------|--------------------------------------------|-------------------------------------|
| `sample_chat.yaml`        | chat-only | chat, conversation       | "Responding with text only, no tools needed." | "Sure, let's chat!"              |
| `sample_compression_player.yaml` | compress-player-summary | 已玩局数、胜负记录 | — | "已玩 5 局，其中 4 局失败。策略：优先翻开角落与边缘格子，命中数字 1 时先标记周围雷。" |
| `sample_compression_planner.yaml` | compress-planner-summary | 已复盘局数 | — | "已复盘 5 局，策略更新正常，每局均按新策略执行。" |
| `sample_farewell.json`    | farewell  | bye, goodbye, see you    | "The user is saying goodbye."              | "Goodbye! Have a great day!"        |
| `sample_greeting.yaml`    | greeting  | hello, hi, greetings     | "The user is greeting me, I should respond warmly." | "Hello! How can I help you today?" |
| `sample_mouse_trigger.yaml` | mouse-trigger | move the mouse, position cursor | — | — (carries a `tool_call: mouse_move`) |
| `sample_planner_strategy.yaml` | planner-update-strategy | 本局游戏过程 | — | — (carries a `tool_call: update_strategy`) |
| `sample_saolei_start.yaml` | saolei-start | start saolei, play minesweeper | — | — (carries a `tool_call: saolei_init`) |

`compress-player-summary` / `compress-planner-summary` are the plain-text
responses for the team graph's COMPRESS node (specs/037-saolei-team-optimize
US2 / FR-008/FR-012): the compress node (agent/src/team/compress.ts
summarizeChannel) invokes the player/planner models DIRECTLY with its summary
prompts + the serialized channel messages. The keywords are substrings of
those prompts' instruction lines ("已玩局数、胜负记录" / "已复盘局数"), and
the names sort alphabetically BEFORE the configs the serialized channel text
would otherwise match (`saolei-start` for the player channel's user text,
`planner-update-strategy` for the planner channel's review prefix) — so the
summary calls resolve deterministically to a text summary, never to a
tool_call response (a tool_call carries empty content → compress.ts rejects
it as a blank summary → FR-013 abort). They carry NO `reasoning:` field on
purpose: a reasoning_content-bearing response is parsed by the LangChain
OpenAI adapter into content BLOCKS, which the compress node rejects as
"non-string content" — text-only responses yield the plain string the
`model.invoke` call requires (the same reason the compress node now
normalizes content-blocks via `extractTextContent`, compress.ts).

`mouse-trigger`, `planner-update-strategy` and `saolei-start` carry a
`tool_call` instead of text: a user turn matching their keyword makes
fake-LLM return a `tool_calls` response so the large tests drive the real
model→tool_call→dispatch chain (see §7). They are excluded from the random
no-match fallback (a random tool_call would nonsensically invoke a desktop
operation).

`planner-update-strategy` is the team-model fixture (spec
031-team-template-mode): the team graph's planner agent (planner.ts) ends
its model input with a HumanMessage rendered from the buffer's gameLog
whose text always starts with the fixed prefix "本局游戏过程" (planner.ts
buildReviewInput renders the numbered move lines "1. <tool>(coord) →
status" plus each board — specs/036-team-mode-bugfix/contracts/team-graph-
fix-contract.md §2.2) — matching that prefix makes fake-LLM return an
`update_strategy` tool_call deterministically, so the saolei_team suite
drives the planner→update_strategy→StrategyStore flow end-to-end
(FR-011/FR-012/D6). The follow-up response after the tool executes lives in
`sample_update_strategy_tools.yaml` (`update-strategy-success-text`).

The tool configs in `sample_tools.yaml` (mouse/keyboard) and
`sample_saolei_tools.yaml` (saolei init→click→update chaining) are matched
against the `role:"tool"` messages returned by LangChain after a tool
invocation, keyed by `tool_name` and the `match_result_contains` substrings.
`sample_saolei_tools.yaml` additionally carries `saolei-click-terminal-text`
(an empty-constraint config) so any `saolei_click` result that does not
match the coordinate-tagged configs — e.g. the pre-dispatch rejections on a
terminal board, whose bodies carry no "(x,y)" — terminates the tool loop
with text instead of falling into the random no-match fallback (whose pool
includes mouse tool_calls the team's player agent does not hold; FR-028).
See `style/large_test.md` for the test organization rules and
`fake-llm/service/message_store.go` for the loader contract.

## 4. Stateless matching model

`fake-llm` keeps **no** per-session state. For each `POST /v1/chat/completions`
request the handler:

1. Extracts the last `role:"user"` message text (string form, or the
   concatenated `type:"text"` parts of the array form).
2. Scans every loaded message for one whose `keywords` contains a
   case-insensitive substring of that text.
3. Among all matches, the message with the alphabetically-**lowest** `name`
   wins. With the shipped samples that means `farewell` sorts before
   `greeting`, so a prompt containing both "hello" and "goodbye" resolves to
   `farewell`.
4. If nothing matches, a uniformly-random message is returned and a `WARN` log
   line is emitted (`user_snippet`, `random_name`). The HTTP status is still
   `200`; the handler never surfaces a match failure as an error.

Because matching is stateless and keyword-driven, the large tests send prompts
that contain a **single** template's keyword to get a deterministic response,
and use **distinct** keywords per turn to prove FIFO ordering.

## 5. How to add or update messages (and keep assertions in sync)

1. **Edit the data.** Add or modify JSON/YAML files in
   `projects/game/fake-llm/service/testdata/`. The file is embedded at build
   time; no extra `data` wiring is needed (gazelle writes `embedsrcs` on the
   `go_library`).
2. **Mind the model-name rules.** The `ModelProviderCache` routes models whose
   names start with `claude`, `minimax-`, or `qwen3.` to the Anthropic
   platform. The `agent_test` test profiles must therefore use a **non-**
   Anthropic name — `gpt-4`, `gpt-4-turbo`, `gpt-4o`, `gpt-3.5-turbo`, or any
   custom name not caught by those prefix checks. `fake-llm` itself ignores
   the model field; only the agent-side routing cares.
3. **Update the large-test assertions.** The expected `reasoning`/`text`
   strings are pinned as constants in `helpers_test.go`
   (`expectedChatReasoning`, `expectedChatText`, `expectedGreetingReasoning`,
   `expectedGreetingText`, `expectedFarewellReasoning`,
   `expectedFarewellText`) with a comment marking them as needing sync with
   the testdata. Update those constants whenever the testdata changes, and
   adjust any `strings.Contains` assertions that depend on them.
4. **The fake-llm unit test fails first.** `TestNewMessageStore_LoadsEmbeddedSamples`
   in `projects/game/fake-llm/service/message_store_test.go` pins the real
   embedded testdata (chat-only before compress-planner-summary before
   compress-player-summary before farewell before greeting, with exact
   `Reasoning` / `Text` / `Keywords` values). It is the single source
   of truth — if the testdata changes, that test breaks first and reminds
   you to update the helpers constants and assertions in lockstep.

## 6. How to run the testplan

```bash
# Validate the plan (deployment topology, suite/case wiring, endpoint shape)
guitar validate projects/game/testplan/system_test.yaml

# Run the plan end-to-end: deploy agent_test + fake-llm + dependencies,
# run every suite's cases, then tear the deployment down.
guitar run projects/game/testplan/system_test.yaml
```

The deployment (`deploy_agent.yaml`) stands up `mongodb`, `session`, `proxy`,
`fake-llm`, `agent_test`, `prompt`, and `gateway`, and exposes the gateway at
`https://game.liukexin.com`. Test binaries read the endpoint and environment
via `testtool.MustEndpoint` / `testtool.MustEnv` (injected by `guitar`).

## 7. Tool-call / operation-history coverage

The large tests drive the real model→tool_call→dispatch chain through the
fake-LLM `tool_call` Message support (a user turn matching a keyword returns
a `tool_calls` response), producing `AIMessage`-with-`tool_calls` and
`ToolMessage` entries in the LangGraph checkpoint state:

- **Dispatch loop (post-031, saolei tools):** `agent_operation_test.go`
  drives a saolei_init/saolei_click dispatch loop through the team's player
  agent (the ONLY holder of the saolei MCP tools, FR-010/FR-028) and asserts
  the bridge-minted operation-channel id is decoupled from the conversation
  tool_call.id (spec 023 D10), plus the failed/no-screenshot recovery path
  (FR-017). The former mouse-tool dispatch tests were replaced by this suite
  (mouse tools no longer exist on the saolei template).
- **Saolei MCP init→click→update:** `agent_saolei_test.go` drives the
  saolei init→click→click flow with real board screenshots (spec 025
  FR-012/FR-013/FR-022).
- **Team strategy flow:** `saolei_team_test.go` drives a full team turn to
  a terminal won/lost board, the planner's `update_strategy` tool_call and
  the strategy persistence/RefreshTeam contracts (spec 031-team-template-mode
  FR-010..FR-018).
- **US4 (operation/operation_result history):** `handler.ts:ListMessages`
  reconstruction of `operation` / `operation_result` Messages is covered at
  the **unit** level in `projects/game/agent/src/handler.test.ts`
  ("emits operation Message for AIMessage with tool_calls",
  "emits operation_result Message for ToolMessage ...").

### How the large tests drive a real tool_call (the dispatch fix)

Originally fake-LLM's `Message` templates could only return text from a user
turn, so large tests could not make the model initiate a tool_call — they
injected a `ToolResultPart` directly, bypassing the model→tool_call→dispatch
chain. `Message` now carries an optional `tool_call`; when a user turn matches
its keyword, fake-LLM returns a `tool_calls` response (finish_reason
`"tool_calls"`), and the existing `ToolConfig` tool-result chaining drives the
follow-up calls. The `agent_operation` tests (`TestAgentOperationResultSuccess`
/ `Failed`) now send a `mouse_move` tool_call from a user turn, read the
dispatched `MouseMovePart` off the WebSocket, and reply with a
`ToolResultPart`; the `agent_saolei` test does the same for the saolei MCP F2
+ window-message-mouse Parts.

When updating mouse tool names or argument schemas, sync
`sample_tools.yaml`, `message_store_test.go`
(`TestNewMessageStore_LoadsEmbeddedTools`), and any mouse-tool assertions in
lockstep.

> **Spec 031 (team template mode) migration note**: the mouse tools
> (`mouse_move`/`mouse_click`) are no longer part of the saolei template's
> tool surface — the template fixes the player's tools to the saolei MCP
> tools (FR-028). The former `mouseSplitToolNames` profile wiring and the
> mouse-specific dispatch tests were replaced by the saolei-tool dispatch
> loop (`agent_operation_test.go`) and the saolei TEAM suite
> (`saolei_team_test.go`, `testplan_test` target). The mouse/keyboard
> `sample_tools.yaml` configs remain in the store only as inert fallback
> candidates for unmatched tool results.
