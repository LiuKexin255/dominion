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
import { tool } from "langchain";
import { z } from "zod";
import type { GameState } from "@dominion/game-saolei-board";

import { Handler } from "./handler";import { SessionTeam, SessionTeamStore } from "./session-team";
import type { SessionTeamRebuilder } from "./session-team";
import { OperationBridge } from "./operation-bridge";
import type { MemoryClient } from "./memory-client";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import { buildTeamGraph } from "./team/graph";
import { FrozenMemorySnapshot } from "./team/memory-snapshot";
import type { MemorySaver } from "@langchain/langgraph";
import type { StructuredToolInterface } from "@langchain/core/tools";
import type { UserFrame } from "../game_types/projects/game/UserFrame";

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
      // All agent-produced frames are AGENT; the planner's review input is a
      // HumanMessage and carries USER so the live frame renders identically
      // to the reloaded ListMessages entry (multi-line game board preserved —
      // specs/037-saolei-team-optimize US1 format fix).
      const role = (f as Record<string, unknown>).role;
      if (role === "MESSAGE_ROLE_USER") {
        expect((f as Record<string, unknown>).agent).toBe("planner");
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
