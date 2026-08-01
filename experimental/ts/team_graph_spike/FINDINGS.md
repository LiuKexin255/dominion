# FINDINGS — LangGraph team-graph spike (specs/031-team-template-mode D14)

**Spike**: `experimental/ts/team_graph_spike/` | **Date**: 2026-07-29
**Pinned runtime** (`pnpm-workspace.yaml` catalog, verified installed):
`@langchain/langgraph` 1.4.8 · `langchain` 1.5.4 · `@langchain/core` 1.2.3 ·
`@langchain/openai` 1.5.5 · `zod` 3.25.76.

Each hypothesis below is the empirical outcome of the spike
(`src/spike.test.ts` vitest for A1/A2/A3/A4/A6; `testplan/` Go interface test
for A5). Evidence cites the test names + observed values. Design-impact notes
reference the spec decision they validate/revise.

**Summary table**

| Hypothesis | Result | One-line evidence |
|---|---|---|
| A1 `REMOVE_ALL_MESSAGES` per-channel clear | **confirmed** | clears `playerMessages` to `[]`, `plannerMessages` untouched (`spike.test.ts` A1) |
| A2 createAgent embedded in outer graph node | **confirmed** | player createAgent tool-loop runs to LLM self-stop inside the node; outer reads shared state after (`spike.test.ts` A2) |
| A3 single TeamState + per-agent channels + MemorySaver | **confirmed (architecture i)** | `getState().values` returns BOTH channels after a full run; per-agent history reconstructed from one checkpointer (`spike.test.ts` A3) |
| A4 createAgent middleware hook surface | **confirmed** | `beforeAgent`/`beforeModel`/`wrapModelCall`/`afterModel`/`wrapToolCall`/`afterAgent`; middleware CAN return `REMOVE_ALL_MESSAGES` (`spike.test.ts` A4) |
| A5 ChatOpenAI → fake-llm end-to-end | **confirmed** | `guitar run` green: playerMsgs=4, plannerMsgs=4, strategy="corner-first approach" (`testplan/`) |
| A6 conditional edge on non-messages field | **confirmed** | routes player→planner iff `gameEnded` set; player→END when null (`spike.test.ts` A6) |

---

## A1 — `REMOVE_ALL_MESSAGES` full clear, per-channel independent — **CONFIRMED**

**Claim**: `@langchain/langgraph` ^1.4.8 exports `REMOVE_ALL_MESSAGES`; a
node/middleware returning `{ messages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })] }`
clears that `MessagesValue` channel; clearing is **per-channel**.

**Evidence**:
- The sentinel is real and exported:
  `projects/game/agent/node_modules/@langchain/langgraph/dist/graph/messages_reducer.js:9`
  `const REMOVE_ALL_MESSAGES = "__remove_all__";` and re-exported from the
  package index. Confirmed import in the spike.
- Behaviour is exactly survey §7.2:
  `messagesStateReducer` (`messages_reducer.js:59-61`) — when a `RemoveMessage`
  with `id === "__remove_all__"` appears in `right`, it returns
  `rightMessages.slice(removeAllIdx + 1)` (drops all `left`, keeps only messages
  after the marker).
- Per-channel independence is **structural**: each `MessagesValue` channel owns
  its own reducer instance operating on its own value. `spike.test.ts` A1 seeds
  two channels, returns `clearChannel("playerMessages")`, and asserts
  `playerMessages === []` while `plannerMessages` keeps its 2 seeded messages.

**Design impact**: D8 (RefreshTeam) holds as designed — `context-middleware`
emits one `RemoveMessage({id: REMOVE_ALL_MESSAGES})` per channel to clear all
short-term memory. No revision needed.

Source: `@langchain/langgraph` `messagesStateReducer`
([LangGraph JS — add memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory));
`experimental/ts/team_graph_spike/src/spike.test.ts` describe("A1").

---

## A2 — createAgent embedded as a node in an outer StateGraph — **CONFIRMED**

**Claim**: An outer `StateGraph` has `player`/`planner` node **functions** that
invoke a `createAgent`'s `.invoke()`, letting its internal tool loop run until
the LLM stops on its own; after it returns the outer graph reads shared state
(`gameEnded`) and routes.

**Evidence** (`spike.test.ts` A2):
- The player node calls `playerAgent.invoke({ messages: state.playerMessages })`
  with the createAgent carrying **no checkpointer** (stateless single run).
- The fakeModel drives a `make_move` tool_call then a stop message; the
  createAgent runs the full tool loop (tool fires → result → model → stop) and
  returns. The outer node then post-processes the sink buffer into
  `TeamState.gameEnded`.
- Observed: `playerMessages.length >= 3` (human + AI(tool_call) + tool result +
  AI(stop)) and the conditional edge routed to the planner
  (`plannerMessages.length >= 1`), proving the outer graph read `gameEnded`
  after the createAgent returned.
- `createAgent` runs fine **without a checkpointer** (`checkpointer?` is
  optional in `langchain/dist/agents/types.d.ts:599`); a single `.invoke()` runs
  one complete agent loop. The message history is owned by the OUTER graph's
  `MemorySaver`, not by the createAgent.

**Where player messages live**: in the **outer graph state** (`playerMessages`
channel on the outer `MemorySaver`). The createAgent is stateless per-invocation;
the node passes the channel's messages in and writes the result back. (The
alternative — createAgent with its own checkpointer + subgraph namespace — is
the LangGraph "subgraph" pattern documented at
[LangGraph JS — use subgraphs](https://docs.langchain.com/oss/javascript/langgraph/use-subgraphs);
it works too but is NOT what D5/D6 specify, and is heavier than needed here.)

**Design impact**: D6 ("player = createAgent full loop, runs until LLM stops")
is feasible exactly as written. The "createAgent invoked inside an outer node
function" pattern (not the subgraph-as-node pattern) is the right one — it lets
the outer graph own per-agent history in its own channels (A3).

Source: `experimental/ts/team_graph_spike/src/team-graph.ts` (`playerNode`);
`experimental/ts/team_graph_spike/src/spike.test.ts` describe("A2").

---

## A3 — single TeamState + per-agent channels + MemorySaver — **CONFIRMED (architecture i)**

**Claim (architecture i)**: a single outer `TeamState` with
`playerMessages`/`plannerMessages` (each a messages channel) + `gameEnded`,
serialized through ONE `MemorySaver`; `getState` reconstructs ALL channels; per-
agent history is rebuilt by reading the matching channel.

**Evidence** (`spike.test.ts` A3):
- One outer `MemorySaver` is bound to the compiled team graph. After a full
  player→planner run, `graph.getState({configurable:{thread_id}}).values`
  returns **both** `playerMessages` (non-empty) and `plannerMessages`
  (non-empty), plus `gameEnded === null` (cleared by the planner, D6 step 6).
- Strategy persisted to the (in-memory) `StrategyStore` survives, decoupled
  from the checkpointer (`store.get("a3") === "corner-first approach"`).
- This is the precise D5 "architecture (i)" — **no per-agent checkpointer
  needed**. Per-agent history reconstruction = `getState().values.playerMessages`
  / `.plannerMessages`.

**Architecture (ii)** (each agent its own createAgent + checkpointer, outer
graph only holds `gameEnded`) is ALSO feasible (it is the documented subgraph
pattern, A2 note), but is **not necessary** and is heavier. **Recommendation:
keep D5 architecture (i)** — single TeamState, per-agent channels on one
MemorySaver. It directly matches FR-005 (messages partitioned per agent) and
gives the simplest history-reconstruction path.

**Design impact**: D5 holds. `research.md` D5's "per-agent private message
channels in one TeamState" is the correct, verified choice. No need to split
into independent createAgents with separate checkpointers.

Source: `experimental/ts/team_graph_spike/src/spike.test.ts` describe("A3").

---

## A4 — createAgent middleware hook surface — **CONFIRMED**

**Claim**: enumerate the ^1.5.4 middleware hooks; is there an "after tool" hook;
can middleware return `REMOVE_ALL_MESSAGES`.

**Evidence** (`spike.test.ts` A4 + type defs):
- Full hook surface on `AgentMiddleware`
  (`langchain/dist/agents/middleware/types.ts`): **`beforeAgent`**,
  **`beforeModel`**, **`wrapModelCall`**, **`afterModel`**, **`wrapToolCall`**,
  **`afterAgent`**. `createMiddleware` accepts all six (spike constructs a
  `ProbeMiddleware` with every hook; compiles + the fns are callable).
- **No separate `afterTool` hook exists.** The "after tool" interception point
  is **`wrapToolCall`** — it wraps the whole tool call `(request, handler) →
  ToolMessage|Command`, so you run logic before calling `handler(request)` and
  post-process its result afterwards. This is strictly more powerful than a
  bare `afterTool`.
- **Middleware CAN return `REMOVE_ALL_MESSAGES`**: a `beforeModel` hook
  returning `{ messages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })] }`
  clears the checkpointed messages (spike asserts the seeded HumanMessage is
  gone from `getState().values.messages`). This mirrors the library's own
  `summarizationMiddleware`
  (`langchain/dist/agents/middleware/summarization.ts`).

**Design impact**:
- D8 RefreshTeam landing point is confirmed: `context-middleware`'s
  `beforeModel` (already present at `projects/game/agent/src/context-middleware.ts`)
  is the correct hook to emit `RemoveMessage({id: REMOVE_ALL_MESSAGES})` per
  channel. The plan's note about a possible "afterTool" hook should be revised
  to name `wrapToolCall` instead — but RefreshTeam does NOT need a tool hook;
  `beforeModel` is the right place (clears before the next model call).
- The player node post-process (D6 step 4, read sink buffer after createAgent
  returns) does NOT need a middleware hook at all — it is plain node-function
  code after `playerAgent.invoke()` returns (A2).

Source: `langchain` `AgentMiddleware`
([types.ts on GitHub](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain/src/agents/middleware/types.ts));
`experimental/ts/team_graph_spike/src/spike.test.ts` describe("A4").

---

## A5 — ChatOpenAI → fake-llm end-to-end — **CONFIRMED**

**Claim**: `ChatOpenAI` (`@langchain/openai`) pointed at the deployed game
fake-llm (OpenAI-compatible `/v1/chat/completions`) drives the team graph
end-to-end: player calls a tool then stops; planner calls `update_strategy`.

**Evidence** (`testplan/`, `guitar run interface_test.yaml` — full deploy→test→
cleanup loop, all green):
- `bazel run @pnpm`/`guitar` deployed `game/fake-llm` + `team-graph-spike`
  (app=game so the resolver's `validateServiceApp` matches; see note below).
- The Go interface test POSTed to the deployed endpoint; observed:
  `playerMsgs=4` (player tool loop ran), `plannerMsgs=4` (planner ran),
  `strategy="corner-first approach"` (planner called `update_strategy`),
  `baseURL="http://10.1.183.3:8080/v1"` (resolver resolved the fake-llm).
- Final `gameEnded=""` is **correct** (planner clears it per D6 step 6); the
  transient "won" is proven by the planner having run.

**Resolver constraint (important for deploy)**: `common/js/resolver`'s
`validateServiceApp` requires `target.app === SERVICE_APP`. The fake-llm is
`app: game` (in `projects/game/fake-llm/service.yaml`), so the spike service
MUST also be `app: game` (`experimental/ts/team_graph_spike/service.yaml`). This
is purely a resolver/deploy concern, not a graph concern.

**Fake-llm testdata (temporary)**: the spike added 3 throwaway testdata files
(`sample_team_player.yaml`, `sample_team_planner.yaml`, `sample_team_tools.yaml`)
+ embedsrcs entries to drive `make_move`/`update_strategy`. **Reverted at task
end** (the real feature will drive saolei MCP tools, not these stubs). Note the
loader requires ONE message per file (a YAML list breaks `parseMessage`).

**Design impact**: A5 confirms ChatOpenAI → fake-llm can drive a multi-agent
team graph. The fake-llm's keyword/tool-name matching is adequate to script
deterministic multi-step agent flows for large tests.

Source: `experimental/ts/team_graph_spike/testplan/` (deploy.yaml,
interface_test.yaml, interface_test.go); guitar run log.

---

## A6 — structured flag conditional routing — **CONFIRMED**

**Claim**: an outer conditional edge reads a NON-messages state field
(`gameEnded: "won"|"lost"|null`) after a createAgent node returns and routes
correctly.

**Evidence** (`spike.test.ts` A6, two cases):
- `gameEnded` becomes non-null (player sink set it) → edge routes to `planner`
  (planner ran → strategy written).
- `gameEnded` stays null (player LLM stopped without calling the tool) → edge
  routes to `END` (planner never ran → strategy store empty).

**Design impact**: D6 step 5 / contract §2.3 hold. Conditional routing on a
plain overwrite channel (not a messages channel) works as a standard LangGraph
feature.

Source: `experimental/ts/team_graph_spike/src/spike.test.ts` describe("A6").

---

## Cross-cutting notes for the planner

1. **`gameEnded` typing gotcha (revise contract §1)**: under the pinned
   `zod` 3.25.76 + `@langchain/langgraph` 1.4.8, a `new StateSchema({gameEnded:
   z.string().nullable().default(null)})` field does NOT compile — zod's
   `~standard` impl lacks the `jsonSchema` member that `StateSchemaField`'s
   `SerializableSchema` requires (TS2322). The working encoding is
   `Annotation<GameEnded>({reducer: overwrite, default: () => null})` (a native
   LangGraph value type, no zod). The message channels use
   `Annotation<BaseMessage[]>({reducer: messagesStateReducer, default: () => []})`
   — equivalent to `MessagesValue` but avoids a separate `StateSchema`/`Annotation`
   interop mismatch. **Recommend `tasks.md` Phase that implements `TeamState`
   use `Annotation.Root` (not `new StateSchema` with zod) for all channels.**
2. **Declaration emission (TS2883)**: exporting a typed `Annotation.Root` /
   `StateSchema` const or a `CompiledStateGraph`-typed return triggers TS2883
   ("inferred type cannot be named without a reference to .../web.cjs") under
   the langgraph dual-package CJS/ESM layout. The spike disables `declaration`
   (it is a leaf service). The production `projects/game/agent` package emits
   declarations today — if it adopts `Annotation.Root` for `TeamState`, either
   keep `TeamState` module-private and export `typeof TeamState.State`, or
   expect to annotate/cast at the export boundary.
3. **createAgent without checkpointer**: confirmed optional; a single `.invoke()`
   runs the full agent loop. The team graph relies on this so the outer
   `MemorySaver` is the single source of truth for per-agent history (A3).
4. **`gameEnded` final value**: after the planner runs it is `null` by design
   (D6 step 6). Tests that want to assert "a game ended" should check the
   planner RAN (plannerMessages non-empty) or read the value mid-run, not the
   final state value.

## Files

- Spike (NEW, the only permanent additions):
  `experimental/ts/team_graph_spike/` — `service.yaml`, `package.json`,
  `tsconfig.json`, `.swcrc`, `BUILD.bazel`, `src/{bootstrap,server,team-graph,
  spike.test}.ts`, `testplan/{deploy,interface_test}.yaml`,
  `testplan/{interface_test.go,BUILD.bazel}`, `FINDINGS.md` (this file).
- Build-infra (NEW, required to register the workspace package, mirrors
  `experimental/openai_llm/client`): `.bazelignore` +1 line.
- Temporary (REVERTED at task end): `projects/game/fake-llm/service/BUILD.bazel`
  (embedsrcs) + `testdata/sample_team_{player,planner,tools}.yaml`.
