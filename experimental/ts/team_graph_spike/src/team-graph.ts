/**
 * team-graph.ts — Minimal LangGraph team graph (player + planner).
 *
 * This is a throwaway spike verifying the API assumptions in
 * specs/031-team-template-mode/research.md D14, mirrored against the
 * graph contract in specs/031-team-template-mode/contracts/team-graph-contract.md.
 *
 * Architecture under test (research.md D5/D6, "architecture (i)"):
 *   - A SINGLE outer `TeamState` carries per-agent `MessagesValue` channels
 *     (`playerMessages`, `plannerMessages`) plus a structured `gameEnded`
 *     control field.
 *   - The `player` and `planner` nodes are node FUNCTIONS that wrap a
 *     `createAgent` (from `langchain`) and invoke it to completion, letting
 *     createAgent's internal tool loop run until the LLM stops on its own
 *     (D6: player = createAgent full loop). The createAgents carry NO
 *     checkpointer of their own — message history lives in the outer graph's
 *     `MemorySaver`, reconstructed from the per-agent channels (A2/A3).
 *   - A conditional edge reads the non-messages `gameEnded` field and routes
 *     player → planner when a game ended (A6).
 *
 * The model is injected so the same builder serves both the deterministic
 * vitest suite (fakeModel) and the HTTP service (ChatOpenAI → fake-llm).
 */

import {
  Annotation,
  END,
  MemorySaver,
  REMOVE_ALL_MESSAGES,
  START,
  StateGraph,
  messagesStateReducer,
} from "@langchain/langgraph";
import { HumanMessage, RemoveMessage, type BaseMessage } from "@langchain/core/messages";
import { createAgent, tool } from "langchain";
import { z } from "zod";

// ---------------------------------------------------------------------------
// TeamState — per-agent message channels + structured gameEnded (D5/D6).
// ---------------------------------------------------------------------------

export type GameEnded = "won" | "lost" | null;

/**
 * Mirrors specs/031-team-template-mode/contracts/team-graph-contract.md §1.
 *
 * Defined via `Annotation.Root` with explicit reducers (NOT `new StateSchema`
 * with zod fields): under the pinned combo `@langchain/langgraph` ^1.4.8 +
 * `zod` ^3.25.76 a plain `z.string()` does NOT satisfy `StateSchemaField`'s
 * `SerializableSchema` constraint (zod's `~standard` impl lacks the
 * `jsonSchema` member). `Annotation<T>({reducer, default})` is a native
 * LangGraph value type and avoids the zod interop entirely. `messagesStateReducer`
 * (the reducer behind `MessagesValue`) drives the two message channels so they
 * still support `REMOVE_ALL_MESSAGES` (A1). See FINDINGS.md.
 *
 * Kept module-private: exporting the inferred `Annotation.Root` const triggers
 * TS2883 ("cannot be named without a reference to AnnotationRoot/StateDefinition
 * from .../web.cjs"). The graph builder exposes the state type via
 * `typeof TeamState.State` instead.
 */
const TeamState = Annotation.Root({
  playerMessages: Annotation<BaseMessage[]>({
    reducer: messagesStateReducer,
    default: () => [],
  }),
  plannerMessages: Annotation<BaseMessage[]>({
    reducer: messagesStateReducer,
    default: () => [],
  }),
  gameEnded: Annotation<GameEnded>({
    // Overwrite (last-write-wins) control field; conditional edge reads it (A6).
    reducer: (_prev: GameEnded, next: GameEnded) => next,
    default: () => null,
  }),
});

export type TeamStateValue = typeof TeamState.State;


// ---------------------------------------------------------------------------
// StrategyStore — long-term strategy memory (D4, in-memory for the spike).
// ---------------------------------------------------------------------------

/**
 * In-memory StrategyStore stand-in. The real one is mongo-backed (D4); the
 * spike only needs get/put semantics to verify the planner writes strategy
 * and the value survives across turns independently of the checkpointer.
 */
export class InMemoryStrategyStore {
  private readonly map = new Map<string, string>();

  async get(sessionId: string): Promise<string> {
    return this.map.get(sessionId) ?? "";
  }

  async put(sessionId: string, content: string): Promise<void> {
    this.map.set(sessionId, content);
  }
}

// ---------------------------------------------------------------------------
// GameEventSink + ephemeral buffer (D7, D6 steps 2-4).
// ---------------------------------------------------------------------------

/**
 * Structured sink callback surface — mirrors D9 `SaoleiEventSink.onGameEnd`.
 * The player tool calls `sink.onGameEnd(status)` to signal game end WITHOUT
 * parsing tool result text (D6 / survey §5.3). The buffer is process-local
 * and ephemeral (D7): the player node post-process reads it after the
 * createAgent loop returns.
 */
export interface GameEventSink {
  onGameEnd(status: "won" | "lost"): void;
}

/**
 * Per-session ephemeral buffer holding the latest game-end event. The player
 * tool writes via onGameEnd; the player node consumes (reads + clears) it
 * once after createAgent returns (D6 step 4).
 */
export class GameEventBuffer implements GameEventSink {
  private gameEnded: GameEnded = null;
  private consumed = true;

  onGameEnd(status: "won" | "lost"): void {
    this.gameEnded = status;
    this.consumed = false;
  }

  /** Read + mark consumed (D6 step 4); returns null when already consumed. */
  consumeGameEnded(): GameEnded {
    if (this.consumed) return null;
    this.consumed = true;
    return this.gameEnded;
  }

  /** Peek without consuming (planner reads gameState here in the real graph). */
  peekGameEnded(): GameEnded {
    return this.gameEnded;
  }
}

// ---------------------------------------------------------------------------
// Build the team graph.
// ---------------------------------------------------------------------------

export interface TeamGraphDeps {
  playerModel: Parameters<typeof createAgent>[0]["model"];
  plannerModel: Parameters<typeof createAgent>[0]["model"];
  strategyStore: InMemoryStrategyStore;
  sink: GameEventBuffer;
  /** Session id used as the StrategyStore key (D4). */
  sessionId: string;
  /**
   * Optional DI seam for `createAgent` so tests can spy on its options
   * (style/javascript.md §测试 — DI over vi.mock). Defaults to the real one.
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  createAgentFn?: (config: any) => any;
}

/**
 * Build the player tool. It calls `sink.onGameEnd` to signal a structured
 * game-end event (D6 step 2). For the spike the tool is a stub "make_move"
 * that ends the game on every call — enough to drive the player createAgent
 * loop and exercise the sink → buffer → node-post-process → conditional-edge
 * path. The real saolei tools live in projects/game/agent/src/mcp/saolei/.
 */
function buildPlayerTool(sink: GameEventBuffer) {
  return tool(
    async ({ x, y }) => {
      // D6 step 2: structured signal, no text parsing.
      sink.onGameEnd("won");
      return `moved to (${x},${y}); game won`;
    },
    {
      name: "make_move",
      description: "Make a move on the board.",
      schema: z.object({ x: z.number(), y: z.number() }),
    },
  );
}

/**
 * Build the planner-only `update_strategy` tool (D4 / contract §2.4). Writes
 * to the StrategyStore (long-term, mongo in production); the spike uses the
 * injected in-memory store.
 */
function buildUpdateStrategyTool(store: InMemoryStrategyStore, sessionId: string) {
  return tool(
    async ({ content }) => {
      await store.put(sessionId, content);
      return { ok: true };
    },
    {
      name: "update_strategy",
      description: "Update the long-term strategy.",
      schema: z.object({ content: z.string() }),
    },
  );
}

/**
 * Build and compile the team StateGraph (architecture i — single TeamState).
 *
 * Returns the compiled graph plus a fresh `MemorySaver` so callers can run
 * `getState` to verify per-channel reconstruction (A3). The return type is
 * inferred (not annotated with `CompiledStateGraph<...>`) to keep the file
 * declaration-free under the langgraph dual-package typing.
 */
export function buildTeamGraph(deps: TeamGraphDeps) {
  const { playerModel, plannerModel, strategyStore, sink, sessionId } = deps;
  const createAgentFn = deps.createAgentFn ?? createAgent;

  // createAgents carry NO checkpointer: each .invoke() runs one full agent
  // loop statelessly (A2/A3 — history is owned by the outer MemorySaver).
  const playerAgent = createAgentFn({
    model: playerModel,
    tools: [buildPlayerTool(sink)],
    systemPrompt: "You are the player. Call make_move then stop.",
  });
  const plannerAgent = createAgentFn({
    model: plannerModel,
    tools: [buildUpdateStrategyTool(strategyStore, sessionId)],
    systemPrompt: "You are the planner. Call update_strategy then stop.",
  });

  /**
   * Player node (contract §2.1). Invokes the player createAgent to
   * completion (D6: full loop, runs until the LLM stops). Post-processes by
   * reading the ephemeral sink buffer once and writing `gameEnded` (D6 step 4).
   */
  const playerNode = async (state: TeamStateValue) => {
    const result = (await playerAgent.invoke({
      messages: state.playerMessages,
    })) as { messages: BaseMessage[] };

    const gameEnded = sink.consumeGameEnded();
    return {
      // Writing the full message array back is idempotent: MessagesValue's
      // messagesStateReducer dedups by id, so input messages (already in the
      // channel) are replaced and only genuinely new messages are appended.
      playerMessages: result.messages,
      ...(gameEnded ? { gameEnded } : {}),
    };
  };

  /**
   * Planner node (contract §2.2). Invokes the planner createAgent to
   * completion (update_strategy retries are inside this node per D6). After
   * it returns, the graph unconditionally clears `gameEnded` (D6 step 6) so
   * the planner fires at most once per game end.
   */
  const plannerNode = async (state: TeamStateValue) => {
    // The planner is reached only after a game ends (plannerMessages starts
    // empty). Seed a review request so the (stateless) fake-llm has a user
    // message to act on; in production the planner systemPrompt + injected
    // gameState (contract §2.2) play this role. When plannerMessages already
    // carries history (multi-turn), replay it verbatim.
    const input =
      state.plannerMessages.length > 0
        ? state.plannerMessages
        : [new HumanMessage("Review the game and update the strategy.")];
    const result = (await plannerAgent.invoke({
      messages: input,
    })) as { messages: BaseMessage[] };
    return {
      plannerMessages: result.messages,
      gameEnded: null, // D6 step 6: clear unconditionally after planner.
    };
  };

  /**
   * Conditional edge (contract §2.3) — routes on the NON-messages `gameEnded`
   * field (A6). Non-null after a player run ⇒ planner; null ⇒ END (FR-009:
   * continuing is driven by the player LLM / user, not a forced loop).
   */
  const routeAfterPlayer = (state: TeamStateValue): "planner" | typeof END => {
    return state.gameEnded ? "planner" : END;
  };

  const checkpointer = new MemorySaver();
  const graph = new StateGraph(TeamState)
    .addNode("player", playerNode)
    .addNode("planner", plannerNode)
    .addEdge(START, "player")
    .addConditionalEdges("player", routeAfterPlayer)
    .addEdge("planner", END)
    .compile({ checkpointer });

  return { graph, checkpointer };
}

// ---------------------------------------------------------------------------
// RefreshTeam helper (D8) — clears both per-agent channels independently.
// ---------------------------------------------------------------------------

/**
 * Emit a state update that clears ONE MessagesValue channel via the
 * `REMOVE_ALL_MESSAGES` sentinel (D8). Returned from a node/middleware to
 * drop all prior messages in that channel. Per-channel independence (A1) is
 * structural: each MessagesValue has its own reducer, so clearing
 * `playerMessages` leaves `plannerMessages` intact.
 */
export function clearChannel(channel: "playerMessages" | "plannerMessages") {
  return {
    [channel]: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })],
  } as Record<"playerMessages" | "plannerMessages", BaseMessage[]>;
}
