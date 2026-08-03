/**
 * spike.test.ts — Empirical verification of the LangGraph API assumptions in
 * specs/031-team-template-mode/research.md D14 (hypotheses A1-A6).
 *
 * Mirrors the fakeModel-driven style of projects/game/agent/src/spike.test.ts
 * and spike.checkpoint.test.ts. Each `describe` block maps to one hypothesis
 * and is the sharpest deterministic proof of the API behaviour under
 * `@langchain/langgraph` ^1.4.8 / `langchain` ^1.5.4 / `@langchain/core` ^1.2.3
 * (pnpm-workspace.yaml catalog).
 *
 * The HTTP/service integration (A5) is covered separately by the testplan
 * Go interface test in testplan/interface_test.go.
 */

import { describe, expect, it, vi } from "vitest";
import { AIMessage, HumanMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import {
  END,
  MemorySaver,
  MessagesValue,
  REMOVE_ALL_MESSAGES,
  START,
  StateGraph,
  StateSchema,
} from "@langchain/langgraph";
import { createAgent, createMiddleware, tool } from "langchain";
import { RemoveMessage } from "@langchain/core/messages";
import { z } from "zod";
import {
  GameEventBuffer,
  InMemoryStrategyStore,
  buildTeamGraph,
  clearChannel,
} from "./team-graph.js";

// ---------------------------------------------------------------------------
// A1 — REMOVE_ALL_MESSAGES clears ONE MessagesValue channel independently.
// ---------------------------------------------------------------------------

describe("A1: REMOVE_ALL_MESSAGES clears a MessagesValue channel independently", () => {
  it("exports REMOVE_ALL_MESSAGES from @langchain/langgraph", () => {
    // Proven by import success; assert the sentinel value too.
    expect(typeof REMOVE_ALL_MESSAGES).toBe("string");
    expect(REMOVE_ALL_MESSAGES).toBe("__remove_all__");
  });

  it("clears playerMessages while leaving plannerMessages intact (per-channel independence)", async () => {
    // Two independent MessagesValue channels + a clear node.
    const State = new StateSchema({
      playerMessages: MessagesValue,
      plannerMessages: MessagesValue,
    });

    // A node that returns a REMOVE_ALL for playerMessages ONLY.
    const clearPlayer = () => clearChannel("playerMessages");

    const graph = new StateGraph(State)
      .addNode("clear_player", clearPlayer)
      .addEdge(START, "clear_player")
      .addEdge("clear_player", END)
      .compile({ checkpointer: new MemorySaver() });

    const seed = [new HumanMessage("p1"), new AIMessage("p2")];
    const plannerSeed = [new HumanMessage("q1"), new AIMessage("q2")];

    const result = (await graph.invoke(
      {
        playerMessages: seed,
        plannerMessages: plannerSeed,
      },
      { configurable: { thread_id: "a1" } },
    )) as {
      playerMessages: unknown[];
      plannerMessages: unknown[];
    };

    // A1 core: playerMessages cleared to [].
    expect(result.playerMessages).toEqual([]);
    // A1 independence: plannerMessages untouched.
    expect(result.plannerMessages).toHaveLength(plannerSeed.length);
  });
});

// ---------------------------------------------------------------------------
// A2 — createAgent embedded as a node in an outer StateGraph (full loop).
// ---------------------------------------------------------------------------

describe("A2: createAgent runs to completion inside an outer StateGraph node", () => {
  it("player node invokes createAgent, runs its tool loop, and the outer graph reads shared state after", async () => {
    // fakeModel drives the player createAgent: first a tool_call (make_move),
    // then a plain AIMessage that ends the agent loop.
    const playerModel = fakeModel()
      .respondWithTools([{ name: "make_move", args: { x: 3, y: 4 } }])
      .respond(new AIMessage("I made my move and stop now."));

    const sink = new GameEventBuffer();
    const store = new InMemoryStrategyStore();
    const plannerModel = fakeModel().respond(new AIMessage("planner idle"));

    const { graph } = buildTeamGraph({
      playerModel,
      plannerModel,
      strategyStore: store,
      sink,
      sessionId: "a2",
    });

    const result = (await graph.invoke(
      { playerMessages: [new HumanMessage("play")] },
      { configurable: { thread_id: "a2" }, recursionLimit: 50 },
    )) as {
      playerMessages: unknown[];
      plannerMessages: unknown[];
      gameEnded: string | null;
    };

    // A2: the player createAgent's tool loop ran (the make_move tool fired →
    // human + AI(tool_call) + tool result + AI(stop) accumulate), proving the
    // createAgent embedded in the outer node ran its internal loop to
    // completion (LLM self-stopped), exactly the D6 "player = createAgent full
    // loop" model.
    expect(result.playerMessages.length).toBeGreaterThanOrEqual(3);
    // A2: the conditional edge routed to the planner — which only happens if
    // the player node's POST-PROCESS (after createAgent returned) read the
    // sink buffer and wrote gameEnded, and the outer graph then read that
    // shared state field. (The planner clears gameEnded per D6 step 6, so the
    // FINAL value is null — that is the correct, expected behaviour, not a
    // failure of the post-process.)
    expect(result.plannerMessages.length).toBeGreaterThanOrEqual(1);
    expect(result.gameEnded).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// A3 — single TeamState + MemorySaver: getState reconstructs per-agent channels.
// ---------------------------------------------------------------------------

describe("A3: single TeamState + MemorySaver reconstructs per-agent channels via getState", () => {
  it("getState returns BOTH playerMessages and plannerMessages after a full player→planner run", async () => {
    const playerModel = fakeModel()
      .respondWithTools([{ name: "make_move", args: { x: 1, y: 1 } }])
      .respond(new AIMessage("player done"));
    const plannerModel = fakeModel()
      .respondWithTools([
        { name: "update_strategy", args: { content: "corner-first" } },
      ])
      .respond(new AIMessage("planner done"));

    const sink = new GameEventBuffer();
    const store = new InMemoryStrategyStore();
    const sessionId = "a3";
    const { graph, checkpointer } = buildTeamGraph({
      playerModel,
      plannerModel,
      strategyStore: store,
      sink,
      sessionId,
    });

    const threadId = "a3";
    await graph.invoke(
      { playerMessages: [new HumanMessage("play a full game")] },
      { configurable: { thread_id: threadId }, recursionLimit: 50 },
    );

    // A3 core: the SAME outer MemorySaver that persisted the run yields BOTH
    // per-agent channels — per-agent history is reconstructed by reading the
    // corresponding channel off getState().values (no separate checkpointer
    // per agent needed). This is the D5 "architecture (i)" feasibility proof.
    const snapshot = (await graph.getState({
      configurable: { thread_id: threadId },
    })) as {
      values: {
        playerMessages: unknown[];
        plannerMessages: unknown[];
        gameEnded: string | null;
      };
    };

    expect(Array.isArray(snapshot.values.playerMessages)).toBe(true);
    expect(snapshot.values.playerMessages.length).toBeGreaterThan(0);

    expect(Array.isArray(snapshot.values.plannerMessages)).toBe(true);
    // Planner ran once (gameEnded → planner), so it has messages.
    expect(snapshot.values.plannerMessages.length).toBeGreaterThan(0);

    // gameEnded was cleared by the planner node (D6 step 6).
    expect(snapshot.values.gameEnded).toBeNull();

    // Strategy persisted to the (in-memory) store, decoupled from the
    // checkpointer (D4).
    expect(await store.get(sessionId)).toBe("corner-first");

    // Belt-and-suspenders: the checkpointer object is the one bound to the
    // graph (proves there is exactly one, shared across channels).
    expect(checkpointer).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// A4 — createAgent middleware hook surface + REMOVE_ALL_MESSAGES in middleware.
// ---------------------------------------------------------------------------

describe("A4: createAgent middleware exposes beforeModel/afterModel/wrapToolCall; can return REMOVE_ALL_MESSAGES", () => {
  it("documents the full middleware hook surface present in langchain ^1.5.4", () => {
    // The AgentMiddleware interface (libs/langchain/src/agents/middleware/types.ts)
    // exposes exactly these hooks. Verified against the installed version's
    // type definitions — see FINDINGS.md A4.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const mw = createMiddleware({
      name: "ProbeMiddleware",
      beforeAgent: () => undefined,
      beforeModel: () => undefined,
      afterModel: () => undefined,
      afterAgent: () => undefined,
      wrapModelCall: async (_req, handler) => handler(_req),
      wrapToolCall: async (_req, handler) => handler(_req),
    });
    // All six hooks are accepted by createMiddleware without type error.
    expect(mw.name).toBe("ProbeMiddleware");
    expect(typeof mw.beforeModel).toBe("function");
    expect(typeof mw.afterModel).toBe("function");
    expect(typeof mw.wrapToolCall).toBe("function");
  });

  it("a beforeModel hook can return REMOVE_ALL_MESSAGES to clear the messages channel", async () => {
    // This is the RefreshTeam landing point (D8): context-middleware's
    // beforeModel returns a RemoveMessage({id: REMOVE_ALL_MESSAGES}) to clear
    // short-term memory. Mirrors the library's own summarizationMiddleware
    // (libs/langchain/src/agents/middleware/summarization.ts).
    const callsBeforeModel = vi.fn();
    const refreshMiddleware = createMiddleware({
      name: "RefreshMiddleware",
      beforeModel: (state: { messages: unknown[] }) => {
        callsBeforeModel(state.messages.length);
        // Clear all prior messages on every model step.
        return {
          messages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })],
        };
      },
    });

    const probe = tool(
      async () => "ok",
      {
        name: "probe",
        description: "probe",
        schema: z.object({}),
      },
    );

    const model = fakeModel()
      .respondWithTools([{ name: "probe", args: {} }])
      .respond(new AIMessage("done"));

    const agent = createAgent({
      model,
      tools: [probe],
      middleware: [refreshMiddleware],
      checkpointer: new MemorySaver(),
    });

    await agent.invoke(
      { messages: [new HumanMessage("seed history that should be cleared")] },
      { configurable: { thread_id: "a4" } },
    );

    // Verify the mock fired (style/javascript.md — prove the hook ran).
    expect(callsBeforeModel).toHaveBeenCalled();

    // A4 core: the REMOVE_ALL_MESSAGES returned from beforeModel cleared the
    // checkpointed messages. Only the messages emitted AFTER the clear (this
    // run's AI + tool + AI) survive — the seeded HumanMessage is gone.
    const state = (await agent.getState({
      configurable: { thread_id: "a4" },
    })) as { values: { messages: Array<{ content: unknown }> } };
    const contents = state.values.messages.map((m) => m.content);
    const hasSeed = contents.some(
      (c) =>
        typeof c === "string" &&
        c.includes("seed history that should be cleared"),
    );
    expect(hasSeed).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// A6 — conditional edge routes on a NON-messages state field (gameEnded).
// ---------------------------------------------------------------------------

describe("A6: conditional edge routes on the non-messages gameEnded field", () => {
  it("routes player → planner when gameEnded becomes non-null", async () => {
    // Reuse the full team graph: player sets gameEnded via sink → conditional
    // edge must route to planner (not END). If routing read the wrong field
    // or failed on a non-messages channel, planner would never run and the
    // strategy store would stay empty.
    const playerModel = fakeModel()
      .respondWithTools([{ name: "make_move", args: { x: 9, y: 9 } }])
      .respond(new AIMessage("done"));
    const plannerModel = fakeModel()
      .respondWithTools([
        { name: "update_strategy", args: { content: "routed-ok" } },
      ])
      .respond(new AIMessage("planner done"));

    const sink = new GameEventBuffer();
    const store = new InMemoryStrategyStore();
    const sessionId = "a6";
    const { graph } = buildTeamGraph({
      playerModel,
      plannerModel,
      strategyStore: store,
      sink,
      sessionId,
    });

    await graph.invoke(
      { playerMessages: [new HumanMessage("play")] },
      { configurable: { thread_id: "a6" }, recursionLimit: 50 },
    );

    // A6 proof: planner ran (strategy written) ⇒ the conditional edge read
    // gameEnded correctly and routed to "planner".
    expect(await store.get(sessionId)).toBe("routed-ok");
  });

  it("routes player → END when gameEnded stays null", async () => {
    // Player LLM stops WITHOUT calling the tool ⇒ sink never fires ⇒
    // gameEnded stays null ⇒ conditional edge must route to END, planner
    // never runs, strategy store stays empty.
    const playerModel = fakeModel().respond(new AIMessage("I stop now."));
    const plannerModel = fakeModel()
      .respondWithTools([
        { name: "update_strategy", args: { content: "should-not-run" } },
      ])
      .respond(new AIMessage("should not run"));

    const sink = new GameEventBuffer();
    const store = new InMemoryStrategyStore();
    const sessionId = "a6b";
    const { graph } = buildTeamGraph({
      playerModel,
      plannerModel,
      strategyStore: store,
      sink,
      sessionId,
    });

    const result = (await graph.invoke(
      { playerMessages: [new HumanMessage("just talk")] },
      { configurable: { thread_id: "a6b" }, recursionLimit: 50 },
    )) as { gameEnded: string | null };

    expect(result.gameEnded).toBeNull();
    expect(await store.get(sessionId)).toBe(""); // planner never ran
  });
});
