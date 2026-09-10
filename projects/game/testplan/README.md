# game testplan

This directory holds the large-test (`go_largetest`) targets that exercise the
game system end-to-end (gateway → proxy → agent-v2, plus the session and
memory faces) via the HTTP + WebSocket surface. The plan is orchestrated by
`guitar` through `system_test.yaml`.

The agent_v2 session face is the **team model**
(`specs/059-agent-v2-team-mode/contracts/team-api.md`): each session carries a
team singleton with a player and a planner member, the Send stream carries
member-labelled frames plus the merged `team_message` sequence, and the
orchestration drives the members in alternation (planner opening → player
game → planner review → structurally driven next game) with no synthesized
drive messages.

The team proto is a **scene-agnostic primitive**: `UpdateTeam` takes a
`members` list (one `{role, preset, model?}` per member) and the role/sender
labels are plain strings (reserved `"user"` for user input; scene vocabulary
`"player"`/`"planner"` for saolei members) — no enums on the wire. The saolei
scene host enforces the scene in two validation layers (structure, then
roster size + role set + preset existence/role equality + model catalog); the
configuration-face preset role is a string with the same vocabulary and the
list role filter validates it.

## 1. Deployment under test

The suites use two test deployments with the same service list —
`deploy_agent_v2.yaml` (the won topology) and `deploy_agent_v2_drop.yaml`
(the progressive + disconnect fault topology for the disconnect suite) —
whose fake-desktop env is the only difference:

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
  receipts. The two deployments differ only in this service's env — the
  won scenario on `desktop-e2e-won` vs the progressive scenario with the
  disconnect fault on `desktop-e2e-drop` — so each topology gets one
  dedicated executor under the same `fake-desktop` service name.
- `proxy`, `agent-v2-test`, `web`, `gateway` — the session face routes
  gateway → proxy (owner affinity) → agent_v2; the preset face and the web
  static hosting are direct.

The gateway is exposed at `https://game.liukexin.com`. `test`-type deployments
share that hostname with the production environment — requests reach a test
environment only when they carry the `env` header set to the full environment
name (e.g. `game.lt3x8q2`; deploy convention, `tools/release/deploy/README.md`).
Test binaries get this for free: `testtool.MustEndpoint` / `testtool.MustEnv`
read the guitar-injected variables and the shared helpers set the header on
every request (`helpers_test.go` `doHTTPTrace`).

## 2. Suites

Three suites over three deployment topologies
(specs/054-agent-v2-bugfixes/contracts/testplan.md §2;
specs/059-agent-v2-team-mode/tasks.md T023) — the six won-topology suites
of the pre-refactor plan share one deployment:

| suite | deploy | binaries | focus |
|---|---|---|---|
| game-system | deploy_agent_v2.yaml | `testplan_test`, `memory_test`, `web_test`, `agent_v2_conversation_test`, `agent_v2_preset_test`, `agent_v2_game_test`, `desktop_flow_test` | the configuration face (session / memory / web hosting) → the team conversation face (team stream, member views, queue/cancel/refresh windows, preset pools, materialization) → the team game face (won chain on the executor, terminal win/loss reviews with the review memory write, desktop-absent, multi-session isolation) → the desktop face (flow stream), cases serial in module order |
| game-disconnect | deploy_agent_v2_drop.yaml | `agent_v2_game_disconnect_test` | the team mid-game disconnect and recovery branch (progressive + disconnect fault topology) |
| game-memory-down | deploy_agent_v2_memory_down.yaml | `agent_v2_memory_down_test` | the team materialization fail-loud branch (no memory service: the planner memory prefetch rejects, UpdateTeam 5xx + GetTeam NOT_FOUND + retryable) |

`guitar run` executes suites and cases serially in YAML order and stops on
the first failure — the main suite runs first so a trunk regression surfaces
before the narrow branches. `guitar run` executes whole bazel targets as
suite cases without per-suite test-function filtering, which is why the
disconnect and memory-down branches have their own binaries
(specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §1.4).

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
- `system_keywords` — Responses-endpoint system-prompt condition: EVERY
  declared keyword must be a case-insensitive substring of the request's
  `instructions` text (the GLM adapter sends the assembled system prompt
  there). Declaring it makes the template multi-turn (priority 1, all
  conditions) and responses-only for the chat fallback gate. Mechanism:
  `fake-llm/service/responses.go` `matchResponsesMultiTurn` /
  `allSystemKeywordsHit` (specs/059-agent-v2-team-mode/tasks.md T008).
- `reasoning` — the thinking-frame content returned to the agent.
- `text` — the response content returned to the agent.

`agent_v2.yaml` serves the `/v1/responses` Responses endpoint consumed by the
agent-v2 conversation suite: `agent-v2-think` (think+text main path),
`agent-v2-plain` (zero reasoning), `agent-v2-slow` (3s inter-chunk delay —
the queued/cancel window), `agent-v2-fail` (response.failed injection with
no content — the recovery path), and `agent-v2-fail-mid` (think+text first,
then response.failed — the partial-content failure whose interrupted
history tail the conversation suite asserts).
`agent_v2_saolei.yaml` chains the game surface: a user
turn matching the saolei-start keyword returns a `saolei_init` tool_call,
`tools:` rules match the tool results (the "new game started" receipt, board
outcomes) to drive the operate batch, and the `game status: won` result
resolves to the final summary text. Every `agent_v2*` entry carries
`responses_only: true` so the chat-completions no-match fallback pool never
observes it; the expected reasoning/text pieces are pinned as the `agentV2*`
constants in `agent_v2_helpers_test.go`.

The team fixtures `team_planner.yaml` and `team_player.yaml` serve the
two-role chain (specs/059-agent-v2-team-mode/tasks.md T011/T018/T023): every
entry anchors on the member persona's identity opening (`system_keywords`),
the planner side emits the opening strategy / game-end review / queued-message
digest, and the player side opens the game when a strategy broadcast arrives,
optionally opens the next game, or stops. `team-planner-wait` is the
controllable long-running planner turn (4s inter-chunk delay) the
queue/cancel/refresh cases pivot on; queued user messages must carry one of
`暂停/稍等/等待/继续` and the first user message one of the opening anchors
(see the per-file comments). The expected texts are pinned as the `team*`
constants in `agent_v2_helpers_test.go`.

The T023 additions:

- `team-planner-review-stop` (the LOSS review) carries a `memory` tool_call
  with a fixed observation content; the `team-planner-review-stop-text` tool
  rule in `agent_v2_saolei_tools.yaml` matches the SUT's `memory added`
  result and continues with the review body. The team memory large test
  asserts the tool result and the entry persisted through
  `/api/v1/.../memories`.
- `team-planner-memory-snapshot` fires only when the planner's assembled
  system prompt carries the reloaded snapshot (header + the fixed
  observation): the memory test refreshes the team after the review and
  asserts the snapshot reply — the fresh planner's setup prefetch must have
  reached the model context.
- `team-player-role-lock` requires BOTH the player persona anchor and the
  saolei guidance heading in `system_keywords`, asserting the mounted player
  composition (preset persona + `saolei:guidance` section) end to end. The
  reverse absence assertions need the `GetTeamMember.system_prompt` read
  surface (T032/T034) and are deferred to T034.

The Responses endpoint derives each tool-call's wire identity from the request
input (`responsesWireIDs` in `responses.go`): deterministic for the same
request, distinct across a chain's steps. A constant call id would make two
tool calls in one member log indistinguishable to the team broadcast's
callId-anchored reference model (real providers mint unique call ids).

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
#
# --timeout is the OVERALL budget for the whole run (default 10m). Each
# suite pays deploy + a fixed 60s settle wait + tests + cleanup; the
# two-suite plan measures ~6-7 minutes end-to-end, so --timeout=15m
# leaves a conservative margin for deploy jitter. An undersized budget
# surfaces as "wait after deploy: context deadline exceeded" on a later
# suite.
guitar run projects/game/testplan/system_test.yaml --timeout=15m
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
   consumed by the suites are pinned as the `agentV2*` / `team*` constants in
   `agent_v2_helpers_test.go`. Update those constants whenever the testdata
   changes, and adjust any `strings.Contains` assertions that depend on them.
4. **The fake-llm unit test fails first.** `TestNewMessageStore_LoadsEmbeddedSamples`
   in `projects/game/fake-llm/service/message_store_test.go` pins the real
   embedded testdata. It is the single source of truth — if the testdata
   changes, that test breaks first and reminds you to update the pinned
   constants and assertions in lockstep.
