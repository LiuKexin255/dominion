/**
 * context-middleware.test.ts — Tests for the RefreshTeam short-term memory
 * clearing helpers (FR-018 / specs/031-team-template-mode/contracts/
 * team-graph-contract.md §5).
 *
 * Covers:
 *  - `clearChannel` produces a per-channel `RemoveMessage(REMOVE_ALL_MESSAGES)`
 *    update (per-channel independence, spike A1).
 *  - `refreshTeamChannels` on a REAL compiled team graph clears BOTH channels
 *    in the outer MemorySaver, clears the `pendingInstruction` slot (039
 *    US3, contract §7), and leaves the `gameEnded` control field untouched.
 *
 * The mechanism intentionally does NOT use a `beforeModel` middleware hook:
 * the player/planner createAgents carry no checkpointer (D14 A2), so their
 * middleware cannot reach the outer per-agent channels; the clear lands in
 * `graph.updateState` instead (see the module header note).
 */

import { describe, expect, it } from "vitest";
import { AIMessage, HumanMessage, RemoveMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import { tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { clearChannel, refreshTeamChannels } from "./context-middleware";
import {
  createEphemeralGameBuffer,
  createTeamSink,
} from "./team/team-sink";
import { buildTeamGraph } from "./team/graph";
import { FrozenMemorySnapshot } from "./team/memory-snapshot";
import type { MemoryClient } from "./memory-client";
import type { TeamStateValue } from "./team/state";
import type { StructuredToolInterface } from "@langchain/core/tools";

/**
 * 039 Phase 5 (T019): the memory data-plane deps the graph now requires —
 * DI fakes (a no-op MemoryClient + a fresh empty snapshot), mirroring the
 * production server.ts wiring (memory-client / per-session snapshot).
 */
function memoryDeps() {
  const memoryClient = {
    listMemories: async () => [],
  } as unknown as MemoryClient;
  return {
    memoryClient,
    frozenSnapshot: new FrozenMemorySnapshot(),
    template: "saolei",
    plannerTools: [] as StructuredToolInterface[],
  };
}

function makeState(): GameState {
  return {
    width: 3,
    height: 3,
    grid: Array.from({ length: 3 }, () =>
      Array.from({ length: 3 }, () => "0" as const),
    ),
  };
}

function buildGameEndingPlayerTool(buffer: ReturnType<typeof createEphemeralGameBuffer>) {
  const sink = createTeamSink(buffer);
  return tool(
    async ({ x, y }: { x: number; y: number }) => {
      await sink.onGameEnd(makeState(), "won");
      return `moved to (${x},${y}); game won`;
    },
    {
      name: "fake_saolei_move",
      description: "Fake saolei move that ends the game.",
      schema: z.object({ x: z.number(), y: z.number() }),
    },
  );
}

/** Run one full player→planner turn on a fresh graph (both channels fill). */
async function runOneGameTurn(sessionId: string) {
  const buffer = createEphemeralGameBuffer();
  const playerModel = fakeModel()
    .respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
    .respond(new AIMessage("won, stopping"))
    .respond(new AIMessage("idle"));
  const plannerModel = fakeModel()
    .respondWithTools([{ name: "instruct_player", args: { content: "保持节奏" } }])
    .respond(new AIMessage("instruction sent"));
  const { graph } = buildTeamGraph({
    playerModel,
    plannerModel,
    buffer,
    sessionId,
    playerTools: [buildGameEndingPlayerTool(buffer)],
    playerBasePrompt: "",
    plannerBasePrompt: "",
    ...memoryDeps(),
  });

  await graph.invoke(
    { playerMessages: [new HumanMessage("开始游戏")] },
    {
      configurable: {
        thread_id: sessionId,
        // R1 external buffer (contract §4) — the review's instruct_player
        // stages its content here.
        instructionBuffer: { content: null },
      },
      recursionLimit: 50,
    },
  );
  return { graph, sessionId };
}

describe("clearChannel", () => {
  it("builds a per-channel RemoveMessage(REMOVE_ALL_MESSAGES) update", () => {
    const update = clearChannel("playerMessages");
    expect(update).toHaveProperty("playerMessages");
    expect(update).not.toHaveProperty("plannerMessages");
    const msgs = update.playerMessages;
    if (!msgs) throw new Error("playerMessages update missing");
    expect(msgs).toHaveLength(1);
    expect(msgs[0]).toBeInstanceOf(RemoveMessage);
    // The sentinel string: "__remove_all__" (spike A1 — messagesStateReducer
    // drops all prior messages when a RemoveMessage with this id appears).
    expect((msgs[0] as { id?: string }).id).toBe("__remove_all__");
  });

  it("clears each channel independently (spike A1)", () => {
    expect(clearChannel("plannerMessages")).toHaveProperty("plannerMessages");
    expect(clearChannel("plannerMessages")).not.toHaveProperty("playerMessages");
  });
});

describe("refreshTeamChannels (FR-018)", () => {
  it("clears BOTH per-agent channels, keeps the pending-instruction slot cleared too, leaves gameEnded alone", async () => {
    const { graph, sessionId } = await runOneGameTurn("ctx-refresh-1");

    // Precondition: both channels carry history after the turn, and the
    // review sent a calibration instruction into the player channel
    // (instruct_player, FR-017).
    const before = (await graph.getState({
      configurable: { thread_id: sessionId },
    })) as { values: TeamStateValue };
    expect(before.values.playerMessages.length).toBeGreaterThan(0);
    expect(before.values.plannerMessages.length).toBeGreaterThan(0);
    expect(
      before.values.playerMessages.some(
        (m) => typeof m.content === "string" && m.content.includes("保持节奏"),
      ),
    ).toBe(true);

    await refreshTeamChannels(graph, sessionId);

    const after = (await graph.getState({
      configurable: { thread_id: sessionId },
    })) as { values: TeamStateValue };
    // Both short-term channels cleared (FR-018).
    expect(after.values.playerMessages).toEqual([]);
    expect(after.values.plannerMessages).toEqual([]);
    // gameEnded is untouched by RefreshTeam (null after the planner cleared
    // it in the turn — D6 step 6; RefreshTeam never writes it).
    expect(after.values.gameEnded).toBeNull();
  });

  it("clears the deferred pendingInstruction slot (039 US3, contract §7)", async () => {
    const buffer = createEphemeralGameBuffer();
    const { graph } = buildTeamGraph({
      playerModel: fakeModel().respond(new AIMessage("hi")),
      plannerModel: fakeModel().respond(new AIMessage("ok")),
      buffer,
      sessionId: "ctx-refresh-pending",
      playerTools: [],
      playerBasePrompt: "",
      plannerBasePrompt: "",
      ...memoryDeps(),
    });

    // A stale init/compact instruction in the slot (e.g. produced by the
    // async initInstruction turn, never consumed) must NOT survive the
    // refresh — it would otherwise be injected into the next activation.
    await graph.updateState(
      { configurable: { thread_id: "ctx-refresh-pending" } },
      { pendingInstruction: "过期的初始指令" },
    );
    await refreshTeamChannels(graph, "ctx-refresh-pending");

    const state = (await graph.getState({
      configurable: { thread_id: "ctx-refresh-pending" },
    })) as { values: TeamStateValue };
    expect(state.values.pendingInstruction).toBeNull();
    expect(state.values.playerMessages).toEqual([]);
    expect(state.values.plannerMessages).toEqual([]);
  });

  it("is a no-op-safe on a thread with no prior messages", async () => {
    const buffer = createEphemeralGameBuffer();
    const { graph } = buildTeamGraph({
      playerModel: fakeModel().respond(new AIMessage("hi")),
      plannerModel: fakeModel().respond(new AIMessage("ok")),
      buffer,
      sessionId: "ctx-refresh-empty",
      playerTools: [],
      playerBasePrompt: "",
      plannerBasePrompt: "",
      ...memoryDeps(),
    });

    await expect(refreshTeamChannels(graph, "ctx-refresh-empty")).resolves.toBeUndefined();
    const state = (await graph.getState({
      configurable: { thread_id: "ctx-refresh-empty" },
    })) as { values: TeamStateValue };
    expect(state.values.playerMessages).toEqual([]);
    expect(state.values.plannerMessages).toEqual([]);
  });
});
