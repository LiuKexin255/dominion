/**
 * handler.test.ts — Tests for the TeamService handler implementations
 * (specs/031-team-template-mode/contracts/api-contract.md §2.2; tasks.md
 * T020).
 *
 * The handler is exercised against a REAL `SessionTeam` (compiled team graph
 * with fakeModel + fake player tool driving the sink — same pattern as
 * team/graph.test.ts), so GetTeam / Connect routing / ListMessages
 * partitions / RefreshTeam run end-to-end without an LLM or MCP server.
 *
 * Mock strategy (style/javascript.md §测试): the handler's only dependency is
 * the `SessionTeamStore`, whose factory is injected (DI seam) — no `vi.mock`.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import * as grpc from "@grpc/grpc-js";

import { AIMessage, HumanMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import { createAgent, tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { Handler } from "./handler";import { SessionTeam, SessionTeamStore } from "./session-team";
import type { SessionTeamRebuilder } from "./session-team";
import { OperationBridge } from "./operation-bridge";
import type { MemoryClient } from "./memory-client";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import { buildTeamGraph, type TeamGraphHandle } from "./team/graph";
import { FrozenMemorySnapshot } from "./team/memory-snapshot";
import type { MemorySaver } from "@langchain/langgraph";
import type { StructuredToolInterface } from "@langchain/core/tools";
import type { UserFrame } from "../game_types/projects/game/UserFrame";
import type { TeamFrame } from "../game_types/projects/game/TeamFrame";

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

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

/** A releasable gate so a player turn can be held in-flight. */
interface Gate {
  promise: Promise<void>;
  resolve: () => void;
}

function makeGate(): Gate {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function buildGameEndingPlayerTool(
  buffer: ReturnType<typeof createEphemeralGameBuffer>,
  gate?: Gate,
) {
  const sink = createTeamSink(buffer);
  return tool(
    async ({ x, y }: { x: number; y: number }) => {
      if (gate) await gate.promise;
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

function playOneGamePlayerModel() {
  return fakeModel()
    .respondWithTools([{ name: "fake_saolei_move", args: { x: 1, y: 1 } }])
    .respond(new AIMessage("won, stopping"))
    .respond(new AIMessage("idle, no new game"));
}

function instructPlayerPlannerModel(content: string) {
  return fakeModel()
    .respondWithTools([{ name: "instruct_player", args: { content } }])
    .respond(new AIMessage("instruction sent"));
}

/**
 * The store factory's planner model: the one-shot async initInstruction turn
 * (triggered at FIRST materialization — T029) consumes the first
 * tool+text pair, the review turn the second.
 */
function initThenReviewPlannerModel(initContent: string, reviewContent: string) {
  return fakeModel()
    .respondWithTools([{ name: "instruct_player", args: { content: initContent } }])
    .respond(new AIMessage("init done"))
    .respondWithTools([{ name: "instruct_player", args: { content: reviewContent } }])
    .respond(new AIMessage("review done"));
}

/**
 * A store whose factory builds REAL SessionTeams (compiled graph).
 *
 * @param gate Optional gate held by the fake player tool (turn in-flight).
 * @param rebuilder Optional US3 rebuild seam; defaults to a rebuilder with
 *   the factory's own model wiring (profile-agnostic in tests).
 */
function createTeamStore(
  gate?: Gate,
  rebuilder?: SessionTeamRebuilder,
): {
  store: SessionTeamStore;
} {
  const store = new SessionTeamStore(
    async (sessionId, template, _profileName) => {
      const buffer = createEphemeralGameBuffer();
      const handle = buildTeamGraph({
        playerModel: playOneGamePlayerModel(),
        plannerModel: initThenReviewPlannerModel("初始指令", "保持节奏"),
        buffer,
        sessionId,
        playerTools: [buildGameEndingPlayerTool(buffer, gate)],
        playerBasePrompt: "",
        plannerBasePrompt: "",
        ...memoryDeps(),
      });
      // Pre-built bridge/sink like the production factory (server.ts) — the
      // SessionTeam constructor no longer creates them internally. sessionId/
      // template are stamped on dispatched operation frames (FR-013).
      const bridge = new OperationBridge(sessionId, template);
      const sink = createTeamSink(buffer);
      return new SessionTeam(handle, buffer, sessionId, template, bridge, sink);
    },
    rebuilder ??
      (async (
        sessionId,
        _template,
        _profileName,
        existingCheckpointer,
      ) => {
        const buffer = createEphemeralGameBuffer();
        // Rebuild seam matching the factory's wiring: recompiles the graph
        // against the EXISTING checkpointer (never a new one — FR-005).
        return buildTeamGraph(
          {
            playerModel: playOneGamePlayerModel(),
            plannerModel: initThenReviewPlannerModel("初始指令", "保持节奏"),
            buffer,
            sessionId,
            playerTools: [buildGameEndingPlayerTool(buffer)],
            playerBasePrompt: "",
            plannerBasePrompt: "",
            ...memoryDeps(),
          },
          existingCheckpointer,
        );
      }),
  );
  return { store };
}

/**
 * Explicitly materialize the session's team (UpdateTeam(allow_missing=true)
 * is now the ONLY materialization point — the handler never materializes
 * teams implicitly).
 */
function createTestTeam(
  store: SessionTeamStore,
  sessionId: string,
): Promise<SessionTeam> {
  return store.update(sessionId, "saolei", "default", true);
}

/** The desktop's connect status probe — the first inbound frame on a stream
 * (specs/041-realtime-init-push/contracts/realtime-channel-contract.md §1.1:
 * it is what triggers the display-sink bind). */
function statusProbeFrame(sessionId: string): UserFrame {
  return {
    sessionId,
    templateId: "saolei",
    payload: "flowParts",
    flowParts: { parts: [{ status: {} }] },
  };
}

/** Poll until the held flag is set (the gated init invoke reached the gate). */
async function waitForHeld(
  held: { value: boolean },
  timeoutMs = 5000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!held.value) {
    if (Date.now() > deadline) {
      throw new Error("timed out waiting for the gated init invoke to hold");
    }
    await new Promise((r) => setTimeout(r, 10));
  }
}

/**
 * A store whose createAgent invokes are held on a gate (041 T009): the
 * one-shot async initInstruction turn (triggered fire-and-forget by
 * `store.update`, session-team.ts `triggerInitInstruction`) is a background
 * task OUTSIDE the TurnLoop (FR-003) — it cannot be held by the player-tool
 * gate `createTeamStore` uses. The `createAgentFn` DI seam (graph.ts:230-231)
 * wraps every agent invoke instead; the T009 tests never submit a user turn,
 * so only the init instruction node's invoke is ever held — the init turn is
 * in-flight exactly like the "user disconnects during the init turn" spec
 * edge case / specs/041-realtime-init-push/quickstart.md §B B7. `Object.create`
 * preserves the agent's prototype
 * surface (invoke/stream/…); only `invoke` is overridden — the player/planner/
 * instruction nodes all drive the agent via `invoke` (player.ts:219,
 * planner.ts:358, instruction-node.ts:208).
 */
function createGatedInitStore(
  gate: Gate,
): { store: SessionTeamStore; held: { value: boolean } } {
  const held = { value: false };
  const store = new SessionTeamStore(
    async (sessionId, template, _profileName) => {
      const buffer = createEphemeralGameBuffer();
      const handle = buildTeamGraph({
        playerModel: playOneGamePlayerModel(),
        plannerModel: initThenReviewPlannerModel("初始指令", "保持节奏"),
        buffer,
        sessionId,
        playerTools: [buildGameEndingPlayerTool(buffer)],
        playerBasePrompt: "",
        plannerBasePrompt: "",
        ...memoryDeps(),
        createAgentFn: (config) => {
          const agent = createAgent(config);
          // Object.create keeps the agent's prototype surface; only invoke
          // is overridden (the nodes drive the agent via invoke alone).
          const wrapped: { invoke: (input: unknown, cfg?: unknown) => Promise<unknown> } =
            Object.create(agent);
          wrapped.invoke = async (input, cfg) => {
            held.value = true;
            await gate.promise;
            return agent.invoke(input as never, cfg as never);
          };
          return wrapped;
        },
      });
      // Pre-built bridge/sink like the production factory (server.ts).
      const bridge = new OperationBridge(sessionId, template);
      const sink = createTeamSink(buffer);
      return new SessionTeam(handle, buffer, sessionId, template, bridge, sink);
    },
  );
  return { store, held };
}

function createUnaryCall<T>(request: T) {
  return { request } as grpc.ServerUnaryCall<T, unknown>;
}

function createCallback<T>(): {
  callback: grpc.sendUnaryData<T>;
  promise: Promise<{ error: grpc.ServiceError | null; response: T | null }>;
} {
  let resolve!: (value: {
    error: grpc.ServiceError | null;
    response: T | null;
  }) => void;
  const promise = new Promise<{
    error: grpc.ServiceError | null;
    response: T | null;
  }>((res) => {
    resolve = res;
  });
  const callback: grpc.sendUnaryData<T> = (error, value) => {
    const svcError =
      error && "code" in error ? (error as grpc.ServiceError) : null;
    resolve({ error: svcError, response: value ?? null });
  };
  return { callback, promise };
}

interface FakeStream {
  on(event: string, handler: (...args: unknown[]) => void): FakeStream;
  write(data: unknown): void;
  end(): void;
  emit(event: string, ...args: unknown[]): void;
  written: unknown[];
  ended: boolean;
}

function createFakeStream(): FakeStream {
  const written: unknown[] = [];
  let ended = false;
  const listeners: Record<string, Array<(...args: unknown[]) => void>> = {};
  const stream: FakeStream = {
    on(event, handler) {
      if (!listeners[event]) listeners[event] = [];
      listeners[event].push(handler);
      return stream;
    },
    write(data) {
      written.push(data);
    },
    end() {
      ended = true;
    },
    emit(event, ...args) {
      const handlers = listeners[event] ?? [];
      for (const handler of handlers) {
        handler(...args);
      }
    },
    written,
    get ended() {
      return ended;
    },
  };
  return stream;
}

/** Build an inbound user messageParts UserFrame (TextPart). The gateway
 * injects both template_id and session_id into inbound frames
 * (api-contract.md §2.2), so tests carry them like the real path. UserFrame
 * has no sender field — the inbound direction is naturally user-sent
 * (specs/035-proto-contract-refine/contracts/frame-split.md §2). */
function userContentFrame(
  sessionId: string,
  text: string,
  agent?: string,
): UserFrame {
  return {
    sessionId,
    templateId: "saolei",
    payload: "messageParts",
    messageParts: { parts: [{ text: { content: text } }] },
    ...(agent ? { agent } : {}),
  };
}

function flush(ms = 60): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function createHandler(store: SessionTeamStore): Handler {
  return new Handler(store);
}

/** Extract frames of a given payload kind written to the stream. */
function framesOfKind(stream: FakeStream, payload: string): unknown[] {
  return stream.written.filter(
    (f) => (f as Record<string, unknown>).payload === payload,
  );
}

function waitFrames(stream: FakeStream): unknown[] {
  return framesOfKind(stream, "flowParts").filter((f) => {
    const parts = (f as { flowParts?: { parts?: Record<string, unknown>[] } })
      .flowParts?.parts;
    return parts?.some((p) => "wait" in p);
  });
}

function warnFrames(stream: FakeStream): unknown[] {
  return framesOfKind(stream, "flowParts").filter((f) => {
    const parts = (f as { flowParts?: { parts?: Record<string, unknown>[] } })
      .flowParts?.parts;
    return parts?.some((p) => "warn" in p);
  });
}

// ===========================================================================
// GetTeam
// ===========================================================================

describe("Handler.GetTeam", () => {
  it("returns the Team with the template schema's agents and the current profile (FR-004)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    await createTestTeam(store, "sess-1");

    handler.GetTeam(
      createUnaryCall({ name: "templates/saolei/sessions/sess-1/team" }),
      callback,
    );

    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.name).toBe("templates/saolei/sessions/sess-1/team");
    // FR-004: the response carries the profile the team was materialized
    // with ("default" — createTestTeam).
    expect(response.profile).toBe("templates/saolei/profiles/default");
    expect(response.agents).toEqual([
      { name: "player", acceptsUserInput: true },
      { name: "planner", acceptsUserInput: false },
    ]);
  });

  it("returns NOT_FOUND when the team was not provisioned (UpdateTeam is the only materialization point)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.GetTeam(
      createUnaryCall({ name: "templates/saolei/sessions/sess-missing/team" }),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
  });

  it("rejects a malformed team name with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.GetTeam(createUnaryCall({ name: "sessions/sess-1/team" }), callback);

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });
});

// ===========================================================================
// UpdateTeam (AIP-134 create-or-update — the only Team materialization point)
// ===========================================================================

describe("Handler.UpdateTeam", () => {
  function updateRequest(
    name: string,
    profile: string,
    allowMissing: boolean,
  ) {
    return { team: { name, profile }, allowMissing };
  }

  it("materializes the team with allow_missing=true and returns the Team resource (FR-001/FR-004)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut/team",
          "templates/saolei/profiles/default",
          true,
        ),
      ),
      callback,
    );

    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.name).toBe(
      "templates/saolei/sessions/sess-ut/team",
    );
    // FR-004: the response carries the profile.
    expect(response.profile).toBe("templates/saolei/profiles/default");
    expect(response.agents).toEqual([
      { name: "player", acceptsUserInput: true },
      { name: "planner", acceptsUserInput: false },
    ]);
    expect(store.get("sess-ut")).toBeDefined();
    expect(store.getProfileName("sess-ut")).toBe("default");
  });

  it("is idempotent for an already-provisioned session with the SAME profile (FR-002)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    // createTestTeam materializes the session's team with profile "default"
    // — the UpdateTeam below repeats that same profile.
    await createTestTeam(store, "sess-ut2");

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut2/team",
          "templates/saolei/profiles/default",
          true,
        ),
      ),
      callback,
    );

    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.name).toBe("templates/saolei/sessions/sess-ut2/team");
    expect(response.profile).toBe("templates/saolei/profiles/default");
    expect(store.get("sess-ut2")).toBeDefined();
  });

  it("rebuilds the team graph for a DIFFERENT profile and returns the Team with the new profile (US3 FR-005)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    // createTestTeam materializes the session's team with profile "default"
    // — which also triggers the one-shot async initInstruction turn (T029,
    // R2). The profile-change rebuild below must wait for the init turn to
    // finish: the rebuild gate (`isBusy()`) includes initInFlight (Phase 6
    // review Issue #5), so a rebuild during the init is correctly rejected
    // FAILED_PRECONDITION.
    await createTestTeam(store, "sess-ut-diff");
    await flush(0);

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-diff/team",
          "templates/saolei/profiles/other",
          true,
        ),
      ),
      callback,
    );

    const { error, response } = await promise;
    // US3: the profile change REBUILDS the graph (no FAILED_PRECONDITION
    // anymore — FR-005/FR-007) and the response carries the NEW profile.
    expect(error).toBeNull();
    expect(response.name).toBe("templates/saolei/sessions/sess-ut-diff/team");
    expect(response.profile).toBe("templates/saolei/profiles/other");
    expect(response.agents).toEqual([
      { name: "player", acceptsUserInput: true },
      { name: "planner", acceptsUserInput: false },
    ]);
    // The store recorded the new profile (GetTeam reads it back, FR-004).
    expect(store.getProfileName("sess-ut-diff")).toBe("other");
    expect(store.get("sess-ut-diff")).toBeDefined();
  });

  it("rejects a profile change while a turn is in-flight with FAILED_PRECONDITION (FR-006)", async () => {
    const gate = makeGate();
    const { store } = createTeamStore(gate);
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-ut-busy");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame(
        "templates/saolei/sessions/sess-ut-busy",
        "开始游戏",
        "player",
      ),
    );
    await flush(0);

    // The player turn is held in-flight on the gate.
    const team = (await store.update(
      "sess-ut-busy",
      "saolei",
      "default",
      true,
    )) as SessionTeam;
    expect(team.isRunning()).toBe(true);

    const { callback, promise } = createCallback<any>();
    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-busy/team",
          "templates/saolei/profiles/other",
          true,
        ),
      ),
      callback,
    );
    const { error } = await promise;
    // FR-006: FAILED_PRECONDITION; the existing team and the in-flight turn
    // are untouched.
    expect(error?.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(store.getProfileName("sess-ut-busy")).toBe("default");
    expect(team.isRunning()).toBe(true);

    // Release the gate: the turn completes and a subsequent profile change
    // rebuild succeeds.
    gate.resolve();
    await flush();
    expect(team.isRunning()).toBe(false);
    const { callback: cb2, promise: p2 } = createCallback<any>();
    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-busy/team",
          "templates/saolei/profiles/other",
          true,
        ),
      ),
      cb2,
    );
    const { error: error2, response: response2 } = await p2;
    expect(error2).toBeNull();
    expect(response2.profile).toBe("templates/saolei/profiles/other");
  });

  it("rejects a profile change while the INIT turn is in-flight with FAILED_PRECONDITION (041 FR-007)", async () => {
    // The one-shot async initInstruction turn is triggered fire-and-forget at
    // FIRST materialization (session-team.ts:925, R2 — 物化即返回). It sets
    // "busy" but NOT "running" (session-team.ts:546-563), so a profile-change
    // rebuild must be rejected through the same `isBusy()` gate as a user
    // turn (handler.test.ts:477 is the user-turn case) —
    // specs/041-realtime-init-push/spec.md FR-007,
    // contracts/realtime-channel-contract.md §5.
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const team = await createTestTeam(store, "sess-ut-init-busy");
    expect(team.isRunning()).toBe(false);
    expect(team.isBusy()).toBe(true);

    const { callback, promise } = createCallback<any>();
    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-init-busy/team",
          "templates/saolei/profiles/other",
          true,
        ),
      ),
      callback,
    );
    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(store.getProfileName("sess-ut-init-busy")).toBe("default");

    // The init turn finishes (isBusy clears) and the same profile change now
    // rebuilds successfully.
    await flush(0);
    expect(team.isBusy()).toBe(false);
    const { callback: cb2, promise: p2 } = createCallback<any>();
    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-init-busy/team",
          "templates/saolei/profiles/other",
          true,
        ),
      ),
      cb2,
    );
    const { error: error2 } = await p2;
    expect(error2).toBeNull();
  });

  it("returns before the init turn completes (fire-and-forget retained — 041 FR-005)", async () => {
    // FR-005 (specs/041-realtime-init-push/spec.md): UpdateTeam materializes
    // the team and returns immediately — the init turn's real-time delivery
    // is a separate concern via the connection, never awaited by the RPC. The
    // probe-visible signal is `isRunning()` (excludes initInFlight), the
    // destructive-op gate is `isBusy()` (includes it) — contracts/
    // realtime-channel-contract.md §5.
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ff/team",
          "templates/saolei/profiles/default",
          true,
        ),
      ),
      callback,
    );

    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.name).toBe("templates/saolei/sessions/sess-ff/team");
    // The RPC already returned while the async init turn is still in-flight:
    // busy (gating destructive ops) but not running (status probe → IDLE).
    const team = store.get("sess-ff");
    expect(team).toBeDefined();
    expect(team?.isRunning()).toBe(false);
    expect(team?.isBusy()).toBe(true);

    // The init turn completes asynchronously after the RPC returned.
    await flush(0);
    expect(team?.isBusy()).toBe(false);
  });

  it("runs the NEXT turn on the NEW profile's model after a rebuild (US3)", async () => {
    // Rebuild seam whose player model answers with a distinctive text — the
    // next turn's streamed output must come from THIS model.
    const rebuilder: SessionTeamRebuilder = async (
      sessionId,
      _template,
      _profileName,
      existingCheckpointer,
    ) => {
      const buffer = createEphemeralGameBuffer();
      return buildTeamGraph(
        {
          playerModel: fakeModel().respond(
            new AIMessage("rebuild-model-answer"),
          ),
          plannerModel: fakeModel().respond(new AIMessage("ok")),
          buffer,
          sessionId,
          playerTools: [buildGameEndingPlayerTool(buffer)],
          playerBasePrompt: "",
          plannerBasePrompt: "",
          ...memoryDeps(),
        },
        existingCheckpointer,
      );
    };
    const { store } = createTeamStore(undefined, rebuilder);
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-ut-model");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // First turn with the ORIGINAL profile's model (playOneGamePlayerModel).
    stream.emit(
      "data",
      userContentFrame(
        "templates/saolei/sessions/sess-ut-model",
        "开始游戏",
        "player",
      ),
    );
    await flush();

    // Profile change → rebuild.
    const { callback, promise } = createCallback<any>();
    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-model/team",
          "templates/saolei/profiles/other",
          true,
        ),
      ),
      callback,
    );
    const { error } = await promise;
    expect(error).toBeNull();

    // The next turn runs on the NEW model — its answer is streamed out.
    stream.emit(
      "data",
      userContentFrame(
        "templates/saolei/sessions/sess-ut-model",
        "再来一局",
        "player",
      ),
    );
    await flush();
    const display = framesOfKind(stream, "messageParts");
    const texts = display.map((f) => {
      const fr = f as Record<string, unknown>;
      const parts = (
        fr.messageParts as { parts: { text?: { content?: string } }[] }
      ).parts;
      return parts.map((p) => p.text?.content ?? "").join("");
    });
    expect(texts.some((t) => t.includes("rebuild-model-answer"))).toBe(true);
  });

  it("returns NOT_FOUND for a missing team when allow_missing=false (AIP-134 standard Update)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut-missing/team",
          "templates/saolei/profiles/default",
          false,
        ),
      ),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
    // No implicit materialization on the standard-Update path.
    expect(store.get("sess-ut-missing")).toBeUndefined();
  });

  it("rejects a malformed team name with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "sessions/sess-ut3/team",
          "templates/saolei/profiles/default",
          true,
        ),
      ),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });

  it("rejects a malformed profile with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut4/team",
          "default",
          true,
        ),
      ),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });

  it("rejects a profile whose template does not match the team name's template (FR-008)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut5/team",
          "templates/other/profiles/default",
          true,
        ),
      ),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });

  it("propagates a downstream gRPC status (profile NOT_FOUND) unchanged", async () => {
    const store = new SessionTeamStore(async () => {
      throw Object.assign(new Error("profile not found"), {
        code: grpc.status.NOT_FOUND,
      }) as grpc.ServiceError;
    });
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.UpdateTeam(
      createUnaryCall(
        updateRequest(
          "templates/saolei/sessions/sess-ut6/team",
          "templates/saolei/profiles/missing",
          true,
        ),
      ),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
  });
});

// ===========================================================================
// Connect — user input routing (FR-032)
// ===========================================================================

describe("Handler.Connect user input routing", () => {
  it("routes a user content frame to the player (accepts_user_input) and streams the team turn + terminal wait", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-a");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-a", "开始游戏", "player"),
    );
    await flush();

    // The turn ran to completion: display frames (agent-tagged) + one wait.
    const display = framesOfKind(stream, "messageParts");
    expect(display.length).toBeGreaterThan(0);
    for (const f of display) {
      expect(["player", "planner"]).toContain(
        (f as Record<string, unknown>).agent,
      );
      // USER-role frames are HumanMessage-sourced: the planner's review
      // input (planner.ts) AND — since 041 T006 — the init instruction
      // node's request + player write-back frames
      // (instruction-node.ts, specs/041-realtime-init-push/contracts/
      // realtime-channel-contract.md §2.2: agent = producing agent so the
      // desktop routes each frame to the right tab, FR-006). AGENT frames
      // are model-produced.
      const role = (f as Record<string, unknown>).role;
      if (role === "MESSAGE_ROLE_USER") {
        expect(["player", "planner"]).toContain(
          (f as Record<string, unknown>).agent,
        );
      } else {
        expect(role).toBe("MESSAGE_ROLE_AGENT");
      }
    }
    expect(waitFrames(stream)).toHaveLength(1);
  });

  it("falls back to the accepts-user-input agent when the frame carries no agent", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-noagent");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-noagent", "开始游戏"));
    await flush();

    expect(framesOfKind(stream, "messageParts").length).toBeGreaterThan(0);
    expect(waitFrames(stream)).toHaveLength(1);
  });

  it("rejects a user content frame for a session whose team was not created (NOT_FOUND)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    const streamError: { error: grpc.ServiceError | null } = { error: null };
    stream.on("error", (err: unknown) => {
      streamError.error = err as grpc.ServiceError;
    });
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-no-team", "开始游戏", "player"));
    await flush();

    // No lazy creation: the frame is rejected with NOT_FOUND (delivered via
    // the stream error channel — grpc-js ServerDuplexStreamImpl maps it to
    // the final status; see handler.ts Connect).
    expect(streamError.error?.code).toBe(grpc.status.NOT_FOUND);
    expect(framesOfKind(stream, "messageParts")).toHaveLength(0);
    expect(store.get("sess-no-team")).toBeUndefined();
  });

  it("rejects a user content frame targeting the planner (observation view, FR-032)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit(
      "data",
      userContentFrame("sess-planner", "开始游戏", "planner"),
    );
    await flush();

    // Warn + wait emitted; NO display frames (the turn never ran).
    expect(framesOfKind(stream, "messageParts")).toHaveLength(0);
    expect(warnFrames(stream)).toHaveLength(1);
    expect(waitFrames(stream)).toHaveLength(1);
  });

  it("rejects a user content frame with an unknown agent name", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-unknown", "开始游戏", "ghost"));
    await flush();

    expect(framesOfKind(stream, "messageParts")).toHaveLength(0);
    expect(warnFrames(stream)).toHaveLength(1);
    expect(waitFrames(stream)).toHaveLength(1);
  });
});

// ===========================================================================
// Connect — flow_result routing + status probe
// ===========================================================================

describe("Handler.Connect flow result + status", () => {
  it("routes a flow_result control frame to the session's bridge", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-bridge");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // Establish the team, then send a flow_result whose tool_id is
    // registered by a dispatch; the bridge resolves it.
    stream.emit("data", userContentFrame("sess-bridge", "开始游戏", "player"));
    await flush();

    const team = (await store.update("sess-bridge", "saolei", "default", true)) as SessionTeam;
    // The handler registered the bridge sink onto the stream on the user
    // frame above, so dispatch writes its flowParts frame to the stream.
    const pendingResult = new Promise<unknown>((resolve) => {
      void team.getBridge().dispatch({ mouseMove: { toolId: "" } }).then(resolve);
    });
    await flush(0);

    // Find the dispatched flowParts frame written to the stream (the bridge
    // sink forwards it), grab the minted tool id, and reply with a
    // flow_result frame.
    const sent = framesOfKind(stream, "flowParts").find(
      (f) =>
        (f as { flowParts?: { parts?: { mouseMove?: unknown }[] } })
          .flowParts?.parts?.[0]?.mouseMove,
    ) as { flowParts: { parts: { mouseMove: { toolId?: string } }[] } };
    expect(sent).toBeDefined();
    const toolId = sent.flowParts.parts[0].mouseMove.toolId ?? "";

    stream.emit("data", {
      sessionId: "sess-bridge",
      payload: "flowParts",
      flowParts: {
        parts: [
          {
            flowResult: {
              toolId,
              status: "TOOL_RESULT_STATUS_SUCCEEDED",
              message: "ok",
            },
          },
        ],
      },
    });

    const result = (await pendingResult) as {
      status: string;
      message: string;
    };
    expect(result.status).toBe("TOOL_RESULT_STATUS_SUCCEEDED");
  });

  it("responds to a status probe with ACTIVE/IDLE derived from the session state", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // No team yet → UNSPECIFIED. The inbound frame carries the gateway-
    // injected template_id; the response must carry it back (api-contract.md
    // §2.2).
    stream.emit("data", {
      sessionId: "sess-status",
      templateId: "saolei",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
    });
    await flush(0);

    const statusFrames = framesOfKind(stream, "flowParts").filter(
      (f) =>
        (f as { flowParts?: { parts?: { status?: unknown }[] } })
          .flowParts?.parts?.[0]?.status !== undefined,
    );
    expect(statusFrames.length).toBeGreaterThan(0);
    const first = statusFrames[0] as { flowParts: { parts: { status: { status: string } }[] } };
    expect(
      ["STATUS_SIGNAL_STATUS_UNSPECIFIED", "STATUS_SIGNAL_STATUS_IDLE"].includes(
        first.flowParts.parts[0].status.status,
      ),
    ).toBe(true);
    expect((statusFrames[0] as { templateId?: string }).templateId).toBe("saolei");
    expect((statusFrames[0] as { sessionId?: string }).sessionId).toBe("sess-status");
  });

  it("responds IDLE to a status probe while ONLY the init turn is in flight (041 FR-003)", async () => {
    // FR-003 (specs/041-realtime-init-push/spec.md): the probe MUST report
    // IDLE (not ACTIVE) when only the background init turn is in flight — the
    // init emits no `wait`, so ACTIVE would stick the desktop's typing
    // indicator on (one-shot probe, research.md D6; handler.ts:409-437
    // derives from `isRunning()` alone, which excludes initInFlight,
    // session-team.ts:546-563; contracts/realtime-channel-contract.md §5).
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    // Materializing triggers the one-shot async init turn (fire-and-forget,
    // session-team.ts:925) — right after `update` resolves it is still
    // in-flight: running=false (probe → IDLE), busy=true (destructive-op
    // gate).
    const team = await createTestTeam(store, "sess-status-init");
    expect(team.isRunning()).toBe(false);
    expect(team.isBusy()).toBe(true);

    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit("data", {
      sessionId: "sess-status-init",
      templateId: "saolei",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
    });
    await flush(0);

    const statusFrames = framesOfKind(stream, "flowParts").filter(
      (f) =>
        (f as { flowParts?: { parts?: { status?: unknown }[] } })
          .flowParts?.parts?.[0]?.status !== undefined,
    );
    expect(statusFrames.length).toBeGreaterThan(0);
    // The init-only probe must be IDLE — UNSPECIFIED/ACTIVE would mis-drive
    // the typing indicator (specs/021-agent-session-resync/contracts/
    // agent-desktop-channel-contract.md §1 guarantees IDLE/UNSPECIFIED ⇒
    // indicator off, but IDLE is the materialized-team contract).
    const first = statusFrames[0] as {
      flowParts: { parts: { status: { status: string } }[] };
    };
    expect(first.flowParts.parts[0].status.status).toBe(
      "STATUS_SIGNAL_STATUS_IDLE",
    );
  });
});

// ===========================================================================
// Connect — stream display sink lifecycle (041 — specs/041-realtime-init-push/
// contracts/realtime-channel-contract.md §1.1/§1.3, FR-010)
// ===========================================================================

describe("Handler.Connect display sink lifecycle", () => {
  it("binds the display sink on the first inbound frame for a session and clears it on stream end", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    const team = await createTestTeam(store, "sess-sink");
    // Let the one-shot async initInstruction turn finish (T029) so it never
    // interferes with the lifecycle assertions below.
    await flush(0);
    const bindSpy = vi.spyOn(team, "bindStreamSink");
    const clearSpy = vi.spyOn(team, "clearStreamSink");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // First inbound frame for the session: the status probe (the desktop
    // sends it first at connect — app.go:1677-1685). The sink is bound with
    // the stream's safeWrite closure; the handle IS that closure
    // (compare-and-delete, operation-bridge.ts:77).
    stream.emit("data", {
      sessionId: "sess-sink",
      templateId: "saolei",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
    });
    expect(bindSpy).toHaveBeenCalledOnce();
    const [sink, handle] = bindSpy.mock.calls[0];
    expect(typeof sink).toBe("function");
    expect(handle).toBe(sink);

    // A second frame for the same session does NOT re-bind (bound once per
    // stream, specs/041-realtime-init-push/contracts/
    // realtime-channel-contract.md §1.1).
    stream.emit("data", {
      sessionId: "sess-sink",
      templateId: "saolei",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
    });
    expect(bindSpy).toHaveBeenCalledOnce();

    // Stream end → the display sink is cleared with THIS stream's handle
    // (specs/041-realtime-init-push/contracts/
    // realtime-channel-contract.md §1.3, FR-010 — pending background pushes
    // emit to null).
    stream.emit("end");
    expect(clearSpy).toHaveBeenCalledOnce();
    expect(clearSpy.mock.calls[0][0]).toBe(handle);
  });

  it("delivers a later user turn through the sink bound by the status probe (submit takes no emit)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-sink-turn");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // The real desktop sequence: status probe first (binds the sink), user
    // turn afterwards — the turn's frames must reach the stream through the
    // bound sink (specs/041-realtime-init-push/contracts/
    // realtime-channel-contract.md §1.2; submit no longer carries an emit
    // callback).
    stream.emit("data", {
      sessionId: "sess-sink-turn",
      templateId: "saolei",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
    });
    await flush(0);

    stream.emit(
      "data",
      userContentFrame("sess-sink-turn", "开始游戏", "player"),
    );
    await flush();

    expect(framesOfKind(stream, "messageParts").length).toBeGreaterThan(0);
    expect(waitFrames(stream)).toHaveLength(1);
  });

  it("clears the display sink on stream END so a still-running init turn emits to null (no write to the dead stream — 041 FR-010, contract §1.3)", async () => {
    const gate = makeGate();
    const { store, held } = createGatedInitStore(gate);
    const handler = createHandler(store);
    const stream = createFakeStream();
    // Materialize the team: the one-shot async initInstruction turn starts
    // fire-and-forget (session-team.ts triggerInitInstruction) and is held
    // in-flight on the gate — the "user disconnects during the init turn"
    // spec edge case, specs/041-realtime-init-push/quickstart.md §B B7.
    await createTestTeam(store, "sess-end-no-write");
    await waitForHeld(held);

    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    // The status probe binds the display sink
    // (specs/041-realtime-init-push/contracts/
    // realtime-channel-contract.md §1.1) and writes the
    // probe response.
    stream.emit("data", statusProbeFrame("sess-end-no-write"));
    const writtenAtProbe = stream.written.length;
    expect(writtenAtProbe).toBeGreaterThan(0);

    // The desktop disconnects mid-init: stream end → cleanupSinks →
    // clearStreamSink (handler.ts:562-566, specs/041-realtime-init-push/
    // contracts/realtime-channel-contract.md §1.3, FR-010 — the
    // pending background push callback is cleared).
    stream.emit("end");

    // Release the init turn: its three frames (specs/041-realtime-init-push/
    // contracts/realtime-channel-contract.md §2.2) now emit to a
    // null sink (session-team.ts streamSink) and are dropped — nothing is
    // written to the dead connection (specs/041-realtime-init-push/research.md
    // D9 best-effort).
    gate.resolve();
    await flush();

    expect(framesOfKind(stream, "messageParts")).toHaveLength(0);
    expect(stream.written.length).toBe(writtenAtProbe);
  });

  it("clears the display sink on stream ERROR so a still-running init turn emits to null (no write to the dead stream — 041 FR-010, contract §1.3)", async () => {
    const gate = makeGate();
    const { store, held } = createGatedInitStore(gate);
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-err-no-write");
    await waitForHeld(held);

    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit("data", statusProbeFrame("sess-err-no-write"));
    const writtenAtProbe = stream.written.length;
    expect(writtenAtProbe).toBeGreaterThan(0);

    // The connection dies with an error: same cleanup path as `end`
    // (handler.ts:551-560 — error → abortLoops + cleanupSinks).
    stream.emit("error", new Error("peer closed"));

    gate.resolve();
    await flush();

    expect(framesOfKind(stream, "messageParts")).toHaveLength(0);
    expect(stream.written.length).toBe(writtenAtProbe);
  });

  it("reconnect binds a fresh sink and a still-in-flight init pushes to the NEW stream (041 quickstart B7 — contract §1.1 compare-and-delete)", async () => {
    const gate = makeGate();
    const { store, held } = createGatedInitStore(gate);
    const handler = createHandler(store);
    await createTestTeam(store, "sess-reconnect");
    await waitForHeld(held);

    // Stream 1: probe binds sink1; the desktop then disconnects mid-init —
    // stream end clears sink1 with stream1's OWN handle
    // (specs/041-realtime-init-push/contracts/
    // realtime-channel-contract.md §1.3).
    const stream1 = createFakeStream();
    handler.Connect(stream1 as unknown as Parameters<typeof handler.Connect>[0]);
    stream1.emit("data", statusProbeFrame("sess-reconnect"));
    stream1.emit("end");

    // Stream 2: the reconnected desktop's first frame binds a FRESH sink
    // (specs/041-realtime-init-push/contracts/
    // realtime-channel-contract.md §1.1 — the per-stream boundDisplaySinks
    // map is per Connect,
    // handler.ts:293-301).
    const stream2 = createFakeStream();
    handler.Connect(stream2 as unknown as Parameters<typeof handler.Connect>[0]);
    stream2.emit("data", statusProbeFrame("sess-reconnect"));

    // The init completes after the reconnect: its frames push through the
    // NEW sink (spec edge case — "if still running, it is pushed on
    // completion through the new connection"), not the dead stream1.
    gate.resolve();
    await flush();

    expect(framesOfKind(stream1, "messageParts")).toHaveLength(0);
    const frames = framesOfKind(stream2, "messageParts") as TeamFrame[];
    // Exactly the three init frames (contract §2.2), in production order,
    // each tagged with the producing agent (FR-006).
    expect(frames.length).toBe(3);
    expect(frames[0].agent).toBe("planner");
    expect(frames[0].role).toBe("MESSAGE_ROLE_USER");
    expect(frames[1].agent).toBe("planner");
    expect(frames[1].role).toBe("MESSAGE_ROLE_AGENT");
    expect(frames[2].agent).toBe("player");
    expect(frames[2].role).toBe("MESSAGE_ROLE_USER");
  });
});

// ===========================================================================
// Connect — abort lifecycle
// ===========================================================================

describe("Handler.Connect abort lifecycle", () => {
  it("aborts session loops on stream end", async () => {
    const gate = makeGate();
    const { store } = createTeamStore(gate);
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-abort");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-abort", "开始游戏", "player"));
    await flush(0);

    // The player turn is held in-flight on the gate.
    const team = (await store.update("sess-abort", "saolei", "default", true)) as SessionTeam;
    expect(team.isRunning()).toBe(true);

    stream.emit("end");
    await flush();

    // Abort cleared the in-flight turn.
    expect(team.isRunning()).toBe(false);
    gate.resolve();
  });
});

// ===========================================================================
// ListMessages — per-agent partitions (FR-005)
// ===========================================================================

describe("Handler.ListMessages", () => {
  it("reconstructs the player partition from the checkpoint channel with agent tagging", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-lm");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-lm", "开始游戏", "player"),
    );
    await flush();

    const { callback, promise } = createCallback<any>();
    handler.ListMessages(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-lm/team/agents/player",
      }),
      callback,
    );
    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.messages.length).toBeGreaterThan(0);
    for (const m of response.messages) {
      expect(m.agent).toBe("player");
      expect(m.name).toMatch(
        /^templates\/saolei\/sessions\/sess-lm\/team\/agents\/player\/messages\//,
      );
    }
  });

  it("partitions planner messages separately (FR-005) and excludes system messages", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-lm2");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-lm2", "开始游戏", "player"),
    );
    await flush();

    // A game ended → the planner ran → its partition is non-empty.
    const { callback, promise } = createCallback<any>();
    handler.ListMessages(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-lm2/team/agents/planner",
      }),
      callback,
    );
    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.messages.length).toBeGreaterThan(0);
    for (const m of response.messages) {
      expect(m.agent).toBe("planner");
    }
  });

  it("returns NOT_FOUND for a session whose team was not created", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    handler.ListMessages(
      createUnaryCall({
        parent: "templates/saolei/sessions/ghost/team/agents/player",
      }),
      callback,
    );
    const { error, response } = await promise;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
    expect(response).toBeNull();
  });

  it("rejects an unknown agent partition with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    handler.ListMessages(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-x/team/agents/ghost",
      }),
      callback,
    );
    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });

  it("marks the interrupted tail block of a persisted partial AIMessage on the emitted part (044 FR-005/SC-003); a normal AIMessage stays unmarked", async () => {
    // The full production pipeline: persistPartialOutput writes the merged
    // partial AIMessage (tail block carrying additional_kwargs.interrupted)
    // via updateState → MemorySaver → ListMessages reconstruction propagates
    // the marker onto the emitted MessagePart (text → { text: { content,
    // interrupted } }, reasoning → { thinking: { content, interrupted } }).
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const team = await createTestTeam(store, "sess-lm-int");
    // Run one real turn so the thread has checkpoints (as in production the
    // partial is persisted on top of an existing history).
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-lm-int", "开始游戏", "player"),
    );
    await flush();

    const handle = (team as unknown as { graphHandle: TeamGraphHandle })
      .graphHandle;
    await handle.graph.updateState(
      { configurable: { thread_id: "sess-lm-int" } },
      {
        playerMessages: [
          new AIMessage({
            id: "partial-1",
            content: [
              // Completed reasoning block — NOT marked (FR-005: only the
              // mid-stream block is flagged).
              { type: "reasoning", reasoning: "deep thought" },
              // The interrupted tail text block (the shape mergePartialBlocks
              // produces on a stall).
              {
                type: "text",
                text: "cut off mid",
                additional_kwargs: { interrupted: true },
              },
            ],
          }),
          new AIMessage({
            id: "normal-1",
            content: [{ type: "text", text: "complete reply" }],
          }),
        ],
      },
      "player",
    );

    const { callback, promise } = createCallback<any>();
    handler.ListMessages(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-lm-int/team/agents/player",
      }),
      callback,
    );
    const { error, response } = await promise;
    expect(error).toBeNull();

    const partialMsg = response.messages.find(
      (m: { messageId: string }) => m.messageId === "partial-1",
    );
    expect(partialMsg).toBeDefined();
    const parts = partialMsg.content.parts;
    // The interrupted marker rides the lenient JSON channel — TextPart/
    // ThinkingPart have no proto field (desktop-rendering-contract.md §3).
    expect(parts.find((p: { thinking?: unknown }) => p.thinking)).toEqual({
      thinking: { content: "deep thought" },
    });
    expect(parts.find((p: { text?: unknown }) => p.text)).toEqual({
      text: { content: "cut off mid", interrupted: true },
    });

    const normalMsg = response.messages.find(
      (m: { messageId: string }) => m.messageId === "normal-1",
    );
    expect(normalMsg).toBeDefined();
    expect(normalMsg.content.parts).toEqual([
      { text: { content: "complete reply" } },
    ]);
  });

  it("propagates the interrupted marker of a reasoning-tail partial onto the thinking part (044 FR-005/SC-003 — reasoningPart path)", async () => {
    // The reasoning-tail counterpart: a partial whose ONLY content block is
    // reasoning (no text — the reasoning-only stall, contract §3) carrying
    // additional_kwargs.interrupted = true (the shape mergePartialBlocks
    // produces). Exercises the `reasoningPart` reconstruction path through
    // the real MemorySaver serde.
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const team = await createTestTeam(store, "sess-lm-reasoning");
    // Run one real turn so the thread has checkpoints (as in production the
    // partial is persisted on top of an existing history).
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame(
        "templates/saolei/sessions/sess-lm-reasoning",
        "开始游戏",
        "player",
      ),
    );
    await flush();

    const handle = (team as unknown as { graphHandle: TeamGraphHandle })
      .graphHandle;
    await handle.graph.updateState(
      { configurable: { thread_id: "sess-lm-reasoning" } },
      {
        playerMessages: [
          new AIMessage({
            id: "reasoning-partial-1",
            content: [
              {
                type: "reasoning",
                reasoning: "thinking cut off",
                additional_kwargs: { interrupted: true },
              },
            ],
          }),
        ],
      },
      "player",
    );

    const { callback, promise } = createCallback<any>();
    handler.ListMessages(
      createUnaryCall({
        parent:
          "templates/saolei/sessions/sess-lm-reasoning/team/agents/player",
      }),
      callback,
    );
    const { error, response } = await promise;
    expect(error).toBeNull();

    const partialMsg = response.messages.find(
      (m: { messageId: string }) => m.messageId === "reasoning-partial-1",
    );
    expect(partialMsg).toBeDefined();
    expect(partialMsg.content.parts).toEqual([
      { thinking: { content: "thinking cut off", interrupted: true } },
    ]);
  });
});

// ===========================================================================
// RefreshTeam (FR-018)
// ===========================================================================

describe("Handler.RefreshTeam", () => {
  it("clears short-term messages (FR-018)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-ref");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-ref", "开始游戏", "player"),
    );
    await flush();

    const team = (await store.update("sess-ref", "saolei", "default", true)) as SessionTeam;
    const before = await team.getTeamState();
    expect(before?.playerMessages.length ?? 0).toBeGreaterThan(0);

    const { callback, promise } = createCallback<any>();
    handler.RefreshTeam(
      createUnaryCall({ name: "templates/saolei/sessions/sess-ref/team" }),
      callback,
    );
    const { error } = await promise;
    expect(error).toBeNull();

    const after = await team.getTeamState();
    expect(after?.playerMessages).toEqual([]);
    expect(after?.plannerMessages).toEqual([]);
  });

  it("rejects RefreshTeam while a turn is in-flight (FAILED_PRECONDITION)", async () => {
    const gate = makeGate();
    const { store } = createTeamStore(gate);
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-busy");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-busy", "开始游戏", "player"),
    );
    await flush(0);

    // The player turn is held in-flight on the gate.
    const team = (await store.update("sess-busy", "saolei", "default", true)) as SessionTeam;
    expect(team.isRunning()).toBe(true);

    const { callback, promise } = createCallback<any>();
    handler.RefreshTeam(
      createUnaryCall({ name: "templates/saolei/sessions/sess-busy/team" }),
      callback,
    );
    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.FAILED_PRECONDITION);

    // Release the gate so the turn completes and the loop drains.
    gate.resolve();
    await flush();
    expect(team.isRunning()).toBe(false);
  });

  it("rejects RefreshTeam while the INIT turn is in-flight (FAILED_PRECONDITION — 041 FR-007)", async () => {
    // FR-007 (specs/041-realtime-init-push/spec.md): destructive operations
    // are rejected while the init turn is in flight — the same `isBusy()`
    // gate as the user-turn case above (handler.ts:249-265). isRunning()
    // excludes initInFlight (session-team.ts:546-563), so the probe stays
    // IDLE while the refresh gate still rejects — contracts/
    // realtime-channel-contract.md §5.
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const team = await createTestTeam(store, "sess-busy-init");
    expect(team.isRunning()).toBe(false);
    expect(team.isBusy()).toBe(true);

    const { callback, promise } = createCallback<any>();
    handler.RefreshTeam(
      createUnaryCall({ name: "templates/saolei/sessions/sess-busy-init/team" }),
      callback,
    );
    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.FAILED_PRECONDITION);

    // Once the init turn completes the busy gate clears and RefreshTeam
    // succeeds.
    await flush(0);
    expect(team.isBusy()).toBe(false);
    const { callback: cb2, promise: p2 } = createCallback<any>();
    handler.RefreshTeam(
      createUnaryCall({ name: "templates/saolei/sessions/sess-busy-init/team" }),
      cb2,
    );
    const { error: error2 } = await p2;
    expect(error2).toBeNull();
  });

  it("returns NOT_FOUND for a session whose team was not created", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    handler.RefreshTeam(
      createUnaryCall({ name: "templates/saolei/sessions/ghost/team" }),
      callback,
    );
    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
  });

  it("rejects a malformed name with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    handler.RefreshTeam(createUnaryCall({ name: "sessions/x/team" }), callback);
    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });
});
