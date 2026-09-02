# game testplan

This directory holds the large-test (`go_largetest`) targets that exercise the
game system end-to-end (gateway → proxy → agent-v2, plus the session and
memory faces) via the HTTP + WebSocket surface. The plan is orchestrated by
`guitar` through `system_test.yaml`.

## 1. Deployment under test

The suites share one test deployment, `deploy_agent_v2.yaml`:

- `mongodb`, `session`, `memory` — the persistence and the /api/v1 faces
  (session CRUD + memory CRUD through the gateway).
- `fake-llm` — a deterministic OpenAI-compatible LLM stand-in. The agent_v2
  artifact points `GLM_LLM_TARGET` at it (`dominion:///game/fake-llm:8080`,
  resolved by the bootstrap to `http://{endpoint}/v1`,
  specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md §4); fake-llm
  ignores credentials either way, so the zero-secret `agent-v2-test` artifact
  needs no token prerequisite.
- `fake-desktop` — the deterministic desktop executor
  (specs/051-agent-v2-dsh-migration/research.md D15): it connects to the
  gateway's /api/v2 flow WebSocket for its configured session and answers
  FlowPart operations with recognizable board screenshots + SUCCEEDED
  receipts. `deploy_agent_v2_drop.yaml` is the same topology with the
  progressive + disconnect fault env for the mid-game disconnect suite.
- `proxy`, `agent-v2-test`, `web`, `gateway` — the session face routes
  gateway → proxy (owner affinity) → agent_v2; the preset face and the web
  static hosting are direct.

The gateway is exposed at `https://game.liukexin.com`. Test binaries read the
endpoint and environment via `testtool.MustEndpoint` / `testtool.MustEnv`
(injected by `guitar`).

## 2. Suites

| suite | deploy | binaries | focus |
|---|---|---|---|
| session | deploy_agent_v2.yaml | `testplan_test` | session CRUD + ListSessions pagination (/api/v1) |
| memory | deploy_agent_v2.yaml | `memory_test` | MemoryService CRUD + AIP-158 pagination through the gateway (/api/v1) |
| agent-v2-conversation | deploy_agent_v2.yaml | `agent_v2_conversation_test`, `web_test` | the /api/v2 NDJSON conversation surface + web hosting |
| agent-v2-preset | deploy_agent_v2.yaml | `agent_v2_preset_test` | preset CRUD/materialization closed loop (/api/v2) |
| agent-v2-game | deploy_agent_v2.yaml | `agent_v2_game_test` | US1 game loop with the fake game chain + fake-desktop (won topology) |
| agent-v2-game-disconnect | deploy_agent_v2_drop.yaml | `agent_v2_game_disconnect_test` | mid-game disconnect and recovery branch |
| desktop-flow | deploy_agent_v2.yaml | `desktop_flow_test` | the flow stream from the desktop's side |

`guitar run` executes whole bazel targets as suite cases without per-suite
test-function filtering, which is why the disconnect branch has its own
binary (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
§1.4).

## 3. fake-llm data file format

Sample messages live in `projects/game/fake-llm/service/testdata/` and are
embedded into the `fake-llm` binary via `//go:embed`. Each file is JSON or
YAML and decodes into:

```yaml
name: greeting
keywords:
  - hello
  - greetings
reasoning: "The user is greeting me, I should respond warmly."
text: "Hello! How can I help you today?"
```

Fields:

- `name` — unique identifier; also the alphabetical tiebreaker when multiple
  messages match (see §4).
- `keywords` — case-insensitive substring triggers. At least one non-empty
  keyword is required; duplicates across files are allowed, an empty string
  element is rejected at startup. Keep keywords long enough that no keyword is
  a substring of another template's trigger text: the former 2-char "hi"
  matched "t(hi)nk", so `greeting` hijacked every `think-*` trigger via the
  alphabetical tie-break (specs/044-llm-stall-recovery-fix/tasks.md T021).
- `reasoning` — the thinking-frame content returned to the agent.
- `text` — the response content returned to the agent.

`agent_v2.yaml` serves the `/v1/responses` Responses endpoint consumed by the
agent-v2 conversation suite: `agent-v2-think` (think+text main path),
`agent-v2-plain` (zero reasoning), `agent-v2-slow` (3s inter-chunk delay —
the queued-turn window), and `agent-v2-fail` (response.failed injection —
the recovery path). `agent_v2_saolei.yaml` chains the game surface: a user
turn matching the saolei-start keyword returns a `saolei_init` tool_call,
`tools:` rules match the tool results (the "new game started" receipt, board
outcomes) to drive the operate batch, and the `game status: won` result
resolves to the final summary text. Every `agent_v2*` entry carries
`responses_only: true` so the chat-completions no-match fallback pool never
observes it; the expected reasoning/text pieces are pinned as the `agentV2*`
constants in `agent_v2_helpers_test.go`.

The chat-completions fixtures (`sample_*.yaml`/`sample_*.json`) match
`POST /v1/chat/completions` requests; they remain loaded as fallback
candidates, and the tool-result configs in `sample_saolei_tools.yaml`
(`match_result_contains` chaining) document the deterministic
tool-call→result→follow-up mechanism the game templates build on.

## 4. Stateless matching model

`fake-llm` keeps **no** per-session state. For each request the handler:

1. Extracts the last user message text (string form, or the concatenated
   `type:"text"` parts of the array form).
2. Scans every loaded message for one whose `keywords` contains a
   case-insensitive substring of that text.
3. Among all matches, the message with the alphabetically-**lowest** `name`
   wins.
4. If nothing matches, a uniformly-random eligible message is returned and a
   `WARN` log line is emitted (`user_snippet`, `random_name`). The HTTP
   status is still `200`; the handler never surfaces a match failure as an
   error.

Because matching is stateless and keyword-driven, the large tests send prompts
that contain a **single** template's keyword to get a deterministic response,
and keep each trigger out of unrelated turns' texts.

## 5. How to run the testplan

```bash
# Install the tools (first time)
bazel run //:deploy_install
bazel run //:guitar_install

# Validate the plan (deployment topology, suite/case wiring, endpoint shape)
guitar validate projects/game/testplan/system_test.yaml

# Run the plan end-to-end: deploy the SUT, run every suite's cases, then
# tear the deployment down. --suite <name> runs a single suite.
guitar run projects/game/testplan/system_test.yaml
```

## 6. How to add or update fake-llm templates

1. **Edit the data.** Add or modify JSON/YAML files in
   `projects/game/fake-llm/service/testdata/`. The file is embedded at build
   time; no extra `data` wiring is needed (gazelle writes `embedsrcs` on the
   `go_library`).
2. **Mind the model-name rules.** The `ModelProviderCache` routes models whose
   names start with `claude`, `minimax-`, or `qwen3.` to the Anthropic
   platform. Test profiles must therefore use a non-Anthropic name (`gpt-4`
   or similar). `fake-llm` itself ignores the model field; only the
   agent-side routing cares.
3. **Update the large-test assertions.** The expected reasoning/text pieces
   consumed by the suites are pinned as the `agentV2*` constants in
   `agent_v2_helpers_test.go`. Update those constants whenever the testdata
   changes, and adjust any `strings.Contains` assertions that depend on them.
4. **The fake-llm unit test fails first.** `TestNewMessageStore_LoadsEmbeddedSamples`
   in `projects/game/fake-llm/service/message_store_test.go` pins the real
   embedded testdata. It is the single source of truth — if the testdata
   changes, that test breaks first and reminds you to update the pinned
   constants and assertions in lockstep.
