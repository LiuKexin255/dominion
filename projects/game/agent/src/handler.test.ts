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

import { FakeStrategyStore } from "./strategy-store";
import { Handler } from "./handler";
import { SessionTeam, SessionTeamStore } from "./session-team";
import { OperationBridge } from "./operation-bridge";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import { buildTeamGraph } from "./team/graph";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";

const FRAME_SENDER_USER = "FRAME_SENDER_USER";
const FRAME_SENDER_AGENT = "FRAME_SENDER_AGENT";
const FRAME_SENDER_SYSTEM = "FRAME_SENDER_SYSTEM";

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

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

function updateStrategyPlannerModel(content: string) {
  return fakeModel()
    .respondWithTools([{ name: "update_strategy", args: { content } }])
    .respond(new AIMessage("strategy updated"));
}

/** A store whose factory builds REAL SessionTeams (compiled graph). */
function createTeamStore(gate?: Gate): {
  store: SessionTeamStore;
  strategies: FakeStrategyStore;
} {
  const strategies = new FakeStrategyStore();
  const store = new SessionTeamStore(
    async (sessionId, template, _profileName) => {
      const buffer = createEphemeralGameBuffer();
      const handle = buildTeamGraph({
        playerModel: playOneGamePlayerModel(),
        plannerModel: updateStrategyPlannerModel("corner-first"),
        strategyStore: strategies,
        buffer,
        sessionId,
        playerTools: [buildGameEndingPlayerTool(buffer, gate)],
      });
      // Pre-built bridge/sink like the production factory (server.ts) — the
      // SessionTeam constructor no longer creates them internally.
      const bridge = new OperationBridge();
      const sink = createTeamSink(buffer);
      return new SessionTeam(handle, buffer, sessionId, template, bridge, sink);
    },
  );
  return { store, strategies };
}

/**
 * Explicitly create the session's team (CreateTeam is now the ONLY creation
 * point — the handler never creates teams implicitly).
 */
function createTestTeam(
  store: SessionTeamStore,
  sessionId: string,
): Promise<SessionTeam> {
  return store.create(sessionId, "saolei", "default");
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

/** Build an inbound user messageParts frame (TextPart, sender USER). The
 * gateway injects both template_id and session_id into inbound frames
 * (api-contract.md §2.2), so tests carry them like the real path. */
function userContentFrame(
  sessionId: string,
  text: string,
  agent?: string,
) {
  return {
    sessionId,
    templateId: "saolei",
    payload: "messageParts",
    messageParts: { parts: [{ text: { content: text } }] },
    sender: FRAME_SENDER_USER,
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
  it("returns the Team with the template schema's agents (player input=true, planner=false)", async () => {
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
    expect(response.agents).toEqual([
      { name: "player", acceptsUserInput: true },
      { name: "planner", acceptsUserInput: false },
    ]);
  });

  it("returns NOT_FOUND when the team was not created (CreateTeam is the only creation point)", async () => {
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
// CreateTeam (AIP-133 — the only Team creation point)
// ===========================================================================

describe("Handler.CreateTeam", () => {
  it("creates the team and returns the Team resource", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.CreateTeam(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-ct",
        profile: "templates/saolei/profiles/default",
      }),
      callback,
    );

    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.name).toBe(
      "templates/saolei/sessions/sess-ct/team",
    );
    expect(response.agents).toEqual([
      { name: "player", acceptsUserInput: true },
      { name: "planner", acceptsUserInput: false },
    ]);
    expect(store.get("sess-ct")).toBeDefined();
  });

  it("is idempotent for an already-created session with the SAME profile (per-session singleton)", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    // createTestTeam creates the session's team with profile "default" —
    // the CreateTeam below repeats that same profile.
    await createTestTeam(store, "sess-ct2");

    handler.CreateTeam(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-ct2",
        profile: "templates/saolei/profiles/default",
      }),
      callback,
    );

    const { error, response } = await promise;
    expect(error).toBeNull();
    expect(response.name).toBe("templates/saolei/sessions/sess-ct2/team");
    expect(store.get("sess-ct2")).toBeDefined();
  });

  it("returns ALREADY_EXISTS with the existing profile when re-created with a DIFFERENT profile", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();
    // createTestTeam creates the session's team with profile "default".
    await createTestTeam(store, "sess-ct-diff");

    handler.CreateTeam(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-ct-diff",
        profile: "templates/saolei/profiles/other",
      }),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.ALREADY_EXISTS);
    // The details carry the existing profile for diagnostics.
    expect(error?.details).toContain("default");
  });

  it("rejects a malformed parent with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.CreateTeam(
      createUnaryCall({
        parent: "sessions/sess-ct3",
        profile: "templates/saolei/profiles/default",
      }),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });

  it("rejects a malformed profile with INVALID_ARGUMENT", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.CreateTeam(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-ct4",
        profile: "default",
      }),
      callback,
    );

    const { error } = await promise;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
  });

  it("rejects a profile whose template does not match the parent's template", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const { callback, promise } = createCallback<any>();

    handler.CreateTeam(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-ct5",
        profile: "templates/other/profiles/default",
      }),
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

    handler.CreateTeam(
      createUnaryCall({
        parent: "templates/saolei/sessions/sess-ct6",
        profile: "templates/saolei/profiles/missing",
      }),
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
      expect((f as Record<string, unknown>).sender).toBe(FRAME_SENDER_AGENT);
      expect(["player", "planner"]).toContain(
        (f as Record<string, unknown>).agent,
      );
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

  it("ignores non-user frames", async () => {
    const { store } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-ignore",
      payload: "messageParts",
      messageParts: { parts: [{ text: { content: "echo" } }] },
      sender: FRAME_SENDER_AGENT,
      agent: "player",
    });
    await flush();

    expect(stream.written).toHaveLength(0);
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

    const team = (await store.create("sess-bridge", "saolei", "default")) as SessionTeam;
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
    const team = (await store.create("sess-abort", "saolei", "default")) as SessionTeam;
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
  it("clears short-term messages; strategy survives (FR-018)", async () => {
    const { store, strategies } = createTeamStore();
    const handler = createHandler(store);
    const stream = createFakeStream();
    await createTestTeam(store, "sess-ref");
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userContentFrame("templates/saolei/sessions/sess-ref", "开始游戏", "player"),
    );
    await flush();

    const team = (await store.create("sess-ref", "saolei", "default")) as SessionTeam;
    const before = await team.getTeamState();
    expect(before?.playerMessages.length ?? 0).toBeGreaterThan(0);
    // The planner wrote the strategy during the turn.
    expect(await strategies.get("sess-ref")).toBe("corner-first");

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
    expect(await strategies.get("sess-ref")).toBe("corner-first");
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
    const team = (await store.create("sess-busy", "saolei", "default")) as SessionTeam;
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
