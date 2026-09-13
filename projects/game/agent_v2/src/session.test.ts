import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentHandle, AgentStatus, CreateAgentOptions } from "@deepseek-ai/dsh-agent";
import type { PromptAssembly } from "@deepseek-ai/dsh-system-prompt";
import { createUserMessage, LlmError } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import { DEFAULT_MODEL, TeamSessionError, TeamSessions } from "./session.js";
import type { TeamView } from "./session.js";
import type { TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { GameEventRecord } from "@dominion/dsh-saolei-loop";

/** The text of a user message's first text block. */
function messageText(message: UserMessage): string {
  const block = message.content[0];
  return block !== undefined && block.type === "text" ? block.text : "";
}

/**
 * Unit tests for the team registry (specs/059-agent-v2-team-mode/contracts/
 * team-api.md §2–§4): per-session materialization/refresh with fail-fast
 * validation and full rollback (no half-materialized team), create_time
 * preservation, the team FIFO/cancel semantics through the orchestrator, the
 * team stream lifecycle (fan-out, queued-first on the accepting stream,
 * quiescence end, disconnect detach), and the GetTeam/List projections.
 *
 * The cordis Context is a hand-rolled fake whose `on` captures listeners;
 * tests drive the dsh event sequence (running → session events → idle) by
 * emitting into the captured listeners — no module interception
 * (style/javascript.md Mock convention).
 */

type Listener = (...args: never[]) => void;

const S1 = "templates/saolei/sessions/s1";
const P_PLAYER = "templates/saolei/presets/p-player";
const P_PLANNER = "templates/saolei/presets/p-planner";
const PLAYER_ID = `${S1}/player`;
const PLANNER_ID = `${S1}/planner`;

/** The bare model-id half of the default composite selector. */
const DEFAULT_BARE_MODEL = DEFAULT_MODEL.slice(DEFAULT_MODEL.indexOf("/") + 1);

interface MemberFake {
  readonly id: string;
  readonly agent: Agent;
  readonly ctx: {
    on: ReturnType<typeof vi.fn>;
    /** The member prompt assembly surface GetTeamMember reads (specs/059-agent-v2-team-mode/tasks.md T032). */
    systemPrompt: { assemble: ReturnType<typeof vi.fn> };
  };
  readonly handle: AgentHandle;
  readonly followups: UserMessage[];
  readonly injections: UserMessage[];
  readonly cancels: unknown[];
  readonly statusListeners: Array<(payload: { agent: Agent; status: AgentStatus }) => void>;
  readonly errorListeners: Array<(payload: { agent: Agent; error: unknown }) => void>;
  settle(): void;
  /**
   * Settle the in-flight turn as failed: emit `agent/error` (an
   * {@link LlmError} carrying the code) and then idle — the exact order the
   * official loop produces
   * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §1/§5).
   */
  failCurrentTurn(code: string): void;
}

interface Harness {
  readonly ctx: DshContext;
  readonly sessions: TeamSessions;
  readonly agentsCreate: ReturnType<typeof vi.fn>;
  readonly members: Map<string, MemberFake>;
  readonly listeners: Map<string, Listener[]>;
  readonly authoring: { compose: ReturnType<typeof vi.fn>; get: ReturnType<typeof vi.fn> };
  readonly listModels: ReturnType<typeof vi.fn>;
  readonly loadPlannerMemory: ReturnType<typeof vi.fn>;
  readonly mountPlayerRuntime: ReturnType<typeof vi.fn>;
  readonly teamQueues: Map<string, UserMessage[]>;
  readonly teamRegister: ReturnType<typeof vi.fn>;
  readonly fiberDispose: ReturnType<typeof vi.fn>;
  readonly loggerError: ReturnType<typeof vi.fn>;
  readonly game: { current: GameEventRecord | null };
}

function fakeMember(
  id: string,
  emitStatus: (member: MemberFake, status: AgentStatus) => void,
  emitError: (member: MemberFake, error: unknown) => void,
): MemberFake {
  const state = { status: "idle" as AgentStatus };
  const statusListeners: MemberFake["statusListeners"] = [];
  const errorListeners: MemberFake["errorListeners"] = [];
  const memberCtx: {
    on: ReturnType<typeof vi.fn>;
    systemPrompt: { assemble: ReturnType<typeof vi.fn> };
    agent?: Agent;
  } = {
    on: vi.fn((name: string, listener: Listener) => {
      if (name === "agent/error") {
        const typed = listener as (payload: { agent: Agent; error: unknown }) => void;
        errorListeners.push(typed);
        return () => {
          const index = errorListeners.indexOf(typed);
          if (index >= 0) {
            errorListeners.splice(index, 1);
          }
        };
      }
      const typed = listener as (payload: { agent: Agent; status: AgentStatus }) => void;
      statusListeners.push(typed);
      return () => {
        const index = statusListeners.indexOf(typed);
        if (index >= 0) {
          statusListeners.splice(index, 1);
        }
      };
    }),
    // The per-member prompt assembly double GetTeamMember reads: an injected
    // section carrying the member identity, rendered by the real renderPrompt
    // (the read face itself is covered by system-prompt.test.ts).
    systemPrompt: {
      assemble: vi.fn(async () => ({
        sections: [{ name: "test:member", text: `system prompt of ${id}` }],
        contexts: [],
        tools: [],
        variables: {},
      })),
    },
  };
  const agent = {
    id,
    session: { id, events: [] },
    get status(): AgentStatus {
      return state.status;
    },
    ctx: memberCtx,
    inject: vi.fn((message: UserMessage) => {
      member.injections.push(message);
    }),
    followup: vi.fn((message: UserMessage) => {
      member.followups.push(message);
      state.status = "running";
      emitStatus(member, "running");
    }),
    cancel: vi.fn((cause: unknown) => {
      member.cancels.push(cause);
      state.status = "idle";
      emitStatus(member, "idle");
    }),
    whenIdle: vi.fn(async () => {}),
  } as unknown as Agent;
  memberCtx.agent = agent;
  const member: MemberFake = {
    id,
    agent,
    ctx: memberCtx as MemberFake["ctx"],
    handle: { agent, dispose: vi.fn(async () => {}) },
    followups: [],
    injections: [],
    cancels: [],
    statusListeners,
    errorListeners,
    settle: () => {
      state.status = "idle";
      emitStatus(member, "idle");
    },
    failCurrentTurn: (code: string) => {
      // The official loop reports the failure at the turn's active boundary
      // and only then converges to idle, so both subscribers observe the
      // failure when the drive's idle await resolves
      // (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §1/§5).
      emitError(member, new LlmError(`injected ${code} failure`, code));
      member.settle();
    },
  };
  return member;
}

interface HarnessOptions {
  /** A composed `ctx.plannerMemory` face to resolve instead of the deps seam. */
  readonly composedPlannerMemory?: { load: ReturnType<typeof vi.fn> };
  /** Omit both the composed service and the deps seam (composition-error test). */
  readonly omitPlannerMemory?: boolean;
}

function createHarness(options: HarnessOptions = {}): Harness {
  const listeners = new Map<string, Listener[]>();
  const on = vi.fn((name: string, listener: Listener) => {
    const list = listeners.get(name) ?? [];
    list.push(listener);
    listeners.set(name, list);
    return () => {
      const current = listeners.get(name) ?? [];
      const index = current.indexOf(listener);
      if (index >= 0) current.splice(index, 1);
    };
  });
  const members = new Map<string, MemberFake>();
  const emitStatus = (member: MemberFake, status: AgentStatus): void => {
    for (const listener of [...member.statusListeners]) {
      listener({ agent: member.agent, status });
    }
    for (const listener of [...(listeners.get("agent/status") ?? [])]) {
      (listener as (...args: unknown[]) => void)({ agent: member.agent, status });
    }
  };
  // Both the orchestrator (member ctx) and the history collector (host ctx)
  // observe the failure, mirroring emitStatus's dual dispatch.
  const emitError = (member: MemberFake, error: unknown): void => {
    for (const listener of [...member.errorListeners]) {
      listener({ agent: member.agent, error });
    }
    for (const listener of [...(listeners.get("agent/error") ?? [])]) {
      (listener as (...args: unknown[]) => void)({ agent: member.agent, error });
    }
  };
  const agentsCreate = vi.fn(async (options: CreateAgentOptions): Promise<AgentHandle> => {
    const member = fakeMember(String(options.sessionId), emitStatus, emitError);
    members.set(member.id, member);
    await options.setup?.(member.ctx as never);
    return member.handle;
  });
  const authoring = {
    compose: vi.fn(async (presetId?: string) => ({
      agentPreset: presetId ?? "",
      setup: vi.fn(async () => {}),
    })),
    get: vi.fn(async (id: string) => ({
      id,
      template: id.endsWith("planner") ? "planner" : "player",
      role: id.endsWith("planner") ? "planner" : "player",
      persona: "",
      createTime: new Date(0),
      updateTime: new Date(0),
    })),
  };
  // One catalog per provider route: validation is provider-scoped after the
  // composite selector is split (contracts/model-selection.md §3).
  const listModels = vi.fn(async (provider: string) =>
    provider === "opencode-go"
      ? [{ id: "kimi-k3" }]
      : [{ id: "glm-5.3" }, { id: "glm-5.5" }],
  );
  const loadPlannerMemory = vi.fn(async () => {});
  const game = { current: null as GameEventRecord | null };
  const mountPlayerRuntime = vi.fn(() => ({ peekGameEvent: () => game.current }));
  const teamQueues = new Map<string, UserMessage[]>();
  const teamRegister = vi.fn((registration: { members: readonly unknown[] }) => {
    expect(registration.members).toHaveLength(2);
    return { dispose: vi.fn() };
  });
  const fiberDispose = vi.fn(async () => {});
  const loggerError = vi.fn();
  const ctx = {
    on,
    agents: { create: agentsCreate, get: vi.fn() },
    get: vi.fn((name: string) => {
      if (name === "presetAuthoring") {
        return authoring;
      }
      if (name === "plannerMemory") {
        return options.composedPlannerMemory;
      }
      return undefined;
    }),
    desktopBridge: {},
    fiber: { dispose: fiberDispose },
    llm: { listModels: vi.fn(async () => [{ id: "glm-5.3" }]) },
  } as unknown as DshContext;
  const sessions = new TeamSessions(ctx, {
    authoring,
    listModels,
    agents: { create: agentsCreate },
    team: {
      register: teamRegister,
      drain: (member: AgentHandle) => teamQueues.get(String(member.agent.id))?.splice(0) ?? [],
    },
    mountPlayerRuntime,
    logger: { error: loggerError },
    provider: "glm-responses",
    // The default harness binds the explicit test seam; the composed-service
    // cases leave it unset so the production resolution path runs.
    ...(options.omitPlannerMemory || options.composedPlannerMemory !== undefined
      ? {}
      : { loadPlannerMemory }),
  });
  return {
    ctx,
    sessions,
    agentsCreate,
    members,
    listeners,
    authoring,
    listModels,
    loadPlannerMemory,
    mountPlayerRuntime,
    teamQueues,
    teamRegister,
    fiberDispose,
    loggerError,
    game,
  };
}

function emit(h: Harness, name: string, ...args: unknown[]): void {
  for (const listener of [...(h.listeners.get(name) ?? [])]) {
    (listener as (...emitArgs: unknown[]) => void)(...args);
  }
}

interface StreamRecorder extends TurnStream {
  events: ChatEvent[];
  ended: boolean;
  failures: Array<{ code: string; message: string }>;
}

function fakeStream(): StreamRecorder {
  const recorder: StreamRecorder = {
    events: [],
    ended: false,
    failures: [],
    write: (event) => {
      recorder.events.push(event);
    },
    end: () => {
      recorder.ended = true;
    },
    fail: (error) => {
      recorder.failures.push(error);
    },
  };
  return recorder;
}

function payloadOf(event: ChatEvent): string {
  if (event.queued !== undefined) return "queued";
  if (event.turnStart !== undefined) return "turnStart";
  if (event.blockStart !== undefined) return "blockStart";
  if (event.delta !== undefined) return "delta";
  if (event.blockEnd !== undefined) return "blockEnd";
  if (event.turnEnd !== undefined) return "turnEnd";
  if (event.toolResult !== undefined) return "toolResult";
  if (event.teamMessage !== undefined) return "teamMessage";
  return "";
}

/** Drain the microtask queue so the pump and the quiescence watcher settle. */
async function flush(times = 40): Promise<void> {
  for (let i = 0; i < times; i++) {
    await Promise.resolve();
  }
}

function member(h: Harness, id: string): MemberFake {
  const found = h.members.get(id);
  if (found === undefined) {
    throw new Error(`member "${id}" is not materialized`);
  }
  return found;
}

function relay(sender: Agent, role: string, text: string): UserMessage {
  return createUserMessage({
    content: [{ type: "text", text }],
    source: { kind: "team-broadcast", role, senderSessionId: sender.session.id, form: "relay", messageId: `${text}-id` as never },
  });
}

/** The saolei scene's fixed two-member roster (one player + one planner). */
function defaultMembers() {
  return [
    { role: "player", preset: P_PLAYER },
    { role: "planner", preset: P_PLANNER },
  ];
}

async function materializeDefault(h: Harness): Promise<TeamView> {
  return h.sessions.materialize(S1, { members: defaultMembers() });
}

/** Emit one member text turn's dsh event sequence and settle it via idle. */
function driveTextTurn(h: Harness, target: MemberFake, text: string): void {
  emit(h, "session/event", target.agent.session, { type: "turn/start", data: { turn: 1 } });
  emit(h, "session/event", target.agent.session, {
    type: "assistant/chunk",
    data: { turn: 1, step: 1, chunk: { type: "block-start", index: 0, blockType: "text" } },
  });
  emit(h, "session/event", target.agent.session, {
    type: "assistant/chunk",
    data: { turn: 1, step: 1, chunk: { type: "text-delta", index: 0, text } },
  });
  emit(h, "session/event", target.agent.session, {
    type: "assistant/chunk",
    data: { turn: 1, step: 1, chunk: { type: "block-end", index: 0, block: { type: "text", text } } },
  });
  emit(h, "session/event", target.agent.session, {
    type: "assistant/message",
    data: { turn: 1, step: 1, message: { content: [{ type: "text", text }] } },
  });
  target.settle();
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("TeamSessions.materialize", () => {
  it("creates both members through the compose/setup chain and serves the team view", async () => {
    const h = createHarness();

    const view = await materializeDefault(h);

    expect(h.agentsCreate).toHaveBeenCalledTimes(2);
    expect(h.agentsCreate).toHaveBeenNthCalledWith(1, {
      sessionId: PLAYER_ID,
      meta: { cwd: process.cwd(), agentPreset: "p-player" },
      agentOptions: { provider: "glm-responses", model: DEFAULT_BARE_MODEL },
      setup: expect.any(Function),
    });
    expect(h.agentsCreate).toHaveBeenNthCalledWith(2, {
      sessionId: PLANNER_ID,
      meta: { cwd: process.cwd(), agentPreset: "p-planner" },
      agentOptions: { provider: "glm-responses", model: DEFAULT_BARE_MODEL },
      setup: expect.any(Function),
    });
    expect(h.mountPlayerRuntime).toHaveBeenCalledTimes(1);
    expect(h.loadPlannerMemory).toHaveBeenCalledTimes(1);
    expect(h.teamRegister).toHaveBeenCalledTimes(1);

    expect(view.name).toBe(`${S1}/team`);
    // The initial activation after materialization is planner (FR-008: the
    // single merged active-member value at rest).
    expect(view.activeMember).toBe("planner");
    expect(
      view.members.map((entry) => [entry.name, entry.role, entry.preset, entry.model]),
    ).toEqual([
      [`${S1}/team/members/player`, "player", P_PLAYER, DEFAULT_MODEL],
      [`${S1}/team/members/planner`, "planner", P_PLANNER, DEFAULT_MODEL],
    ]);
    expect(view.createTime).toBeInstanceOf(Date);
  });

  it("honors per-member composite models and validates them against the selected provider catalog", async () => {
    const h = createHarness();
    await h.sessions.materialize(S1, {
      members: [
        { role: "player", preset: P_PLAYER, model: "glm-responses/glm-5.5" },
        { role: "planner", preset: P_PLANNER, model: "glm-responses/glm-5.3" },
      ],
    });

    // agentOptions carries the split route: the provider from the selector's
    // prefix and the model as a BARE id — the composite form never reaches
    // the agent (contracts/model-selection.md §3; the system prompt
    // `{{model}}` renders the bare id, see system-prompt.test.ts).
    expect(h.agentsCreate).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        agentOptions: { provider: "glm-responses", model: "glm-5.5" },
      }),
    );
    expect(h.agentsCreate).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        agentOptions: { provider: "glm-responses", model: "glm-5.3" },
      }),
    );

    await expect(
      h.sessions.materialize("templates/saolei/sessions/s2", {
        members: [
          { role: "player", preset: P_PLAYER, model: "glm-responses/glm-9.9" },
          { role: "planner", preset: P_PLANNER },
        ],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("unknown model"),
    });
  });

  it("routes a member through the provider half of its composite selector", async () => {
    const h = createHarness();
    const view = await h.sessions.materialize(S1, {
      members: [
        { role: "player", preset: P_PLAYER, model: "opencode-go/kimi-k3" },
        { role: "planner", preset: P_PLANNER, model: DEFAULT_MODEL },
      ],
    });

    expect(h.agentsCreate).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        agentOptions: { provider: "opencode-go", model: "kimi-k3" },
      }),
    );
    expect(h.agentsCreate).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        agentOptions: { provider: "glm-responses", model: DEFAULT_BARE_MODEL },
      }),
    );
    // The member projections keep the composite original (selection-surface
    // contract); the provider catalogs queried are the two split routes.
    expect(view.members.map((entry) => entry.model)).toEqual([
      "opencode-go/kimi-k3",
      DEFAULT_MODEL,
    ]);
    expect(h.listModels).toHaveBeenCalledWith("opencode-go");
    expect(h.listModels).toHaveBeenCalledWith("glm-responses");
  });

  it("rejects malformed selectors (bare id / empty segment) before any member creation", async () => {
    const h = createHarness();

    for (const model of ["glm-5.3", "/glm-5.3", "glm-responses/"]) {
      await expect(
        h.sessions.materialize(S1, {
          members: [
            { role: "player", preset: P_PLAYER, model },
            { role: "planner", preset: P_PLANNER },
          ],
        }),
      ).rejects.toMatchObject({
        code: "INVALID_ARGUMENT",
        message: expect.stringContaining("provider/model-id"),
      });
    }
    expect(h.agentsCreate).not.toHaveBeenCalled();
  });

  it("rejects structure (scene-agnostic) and saolei-scene violations in the two validation layers", async () => {
    const h = createHarness();

    // Layer 1 — structure: members non-empty, role non-empty, preset a
    // resource name under the session template.
    await expect(h.sessions.materialize(S1, { members: [] })).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });
    await expect(
      h.sessions.materialize(S1, { members: [{ role: "", preset: P_PLAYER }] }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    await expect(
      h.sessions.materialize(S1, { members: [{ role: "player", preset: "nope" }] }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    await expect(
      h.sessions.materialize(S1, {
        members: [{ role: "player", preset: "templates/other/presets/p" }],
      }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });

    // Layer 2 — saolei scene: exactly two members with the {player, planner}
    // role set.
    await expect(
      h.sessions.materialize(S1, { members: [{ role: "player", preset: P_PLAYER }] }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    await expect(
      h.sessions.materialize(S1, {
        members: [
          { role: "player", preset: P_PLAYER },
          { role: "player", preset: P_PLAYER },
        ],
      }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    await expect(
      h.sessions.materialize(S1, {
        members: [
          { role: "player", preset: P_PLAYER },
          { role: "planner", preset: P_PLANNER },
          { role: "referee", preset: P_PLAYER },
        ],
      }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(h.agentsCreate).not.toHaveBeenCalled();
  });

  it("rejects a preset role mismatch and an unknown preset before any member creation", async () => {
    const h = createHarness();
    h.authoring.get.mockResolvedValueOnce({
      id: "p-player",
      template: "planner",
      role: "planner",
      persona: "",
      createTime: new Date(0),
      updateTime: new Date(0),
    });

    await expect(materializeDefault(h)).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(h.agentsCreate).not.toHaveBeenCalled();

    const missing = createHarness();
    missing.authoring.get.mockRejectedValueOnce(
      new PresetAuthoringError("NOT_FOUND", 'preset "p-player" not found'),
    );
    await expect(materializeDefault(missing)).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(missing.agentsCreate).not.toHaveBeenCalled();
  });

  it("propagates a preset store failure instead of rewriting it to unknown-preset", async () => {
    const h = createHarness();
    h.authoring.get.mockRejectedValueOnce(
      new PresetAuthoringError("INTERNAL", "preset store operation failed: mongo unreachable"),
    );

    const err = await materializeDefault(h).then(
      () => undefined,
      (failure: unknown) => failure,
    );

    // The store outage keeps its own code and cause chain; only a NOT_FOUND
    // lookup is the unknown-preset INVALID_ARGUMENT case.
    expect(err).toBeInstanceOf(PresetAuthoringError);
    expect((err as PresetAuthoringError).code).toBe("INTERNAL");
    expect(h.agentsCreate).not.toHaveBeenCalled();
  });

  it("rolls the whole materialization back when a member setup fails, then retries cleanly (no half-materialized team)", async () => {
    const h = createHarness();
    h.loadPlannerMemory.mockRejectedValueOnce(new Error("memory unavailable"));

    await expect(materializeDefault(h)).rejects.toThrow("memory unavailable");
    const player = member(h, PLAYER_ID);
    expect(player.handle.dispose).toHaveBeenCalledTimes(1);
    expect(() => h.sessions.getTeam(S1)).toThrow(TeamSessionError);
    try {
      h.sessions.getTeam(S1);
    } catch (err) {
      expect((err as TeamSessionError).code).toBe("NOT_FOUND");
    }

    // Retry with the seam repaired materializes from scratch and rests.
    await materializeDefault(h);
    expect(h.sessions.getTeam(S1).name).toBe(`${S1}/team`);
    expect(member(h, PLANNER_ID).followups).toHaveLength(0);
  });

  it("refreshes in place: old members released, history cleared, create_time preserved", async () => {
    const h = createHarness();
    const first = await materializeDefault(h);
    const oldPlanner = member(h, PLANNER_ID);
    const stream = fakeStream();
    h.sessions.send(S1, "first", stream);
    await flush();
    expect(oldPlanner.followups).toHaveLength(1);

    const second = await h.sessions.materialize(S1, { members: defaultMembers() });

    expect(oldPlanner.handle.dispose).toHaveBeenCalledTimes(1);
    expect(second.createTime).toBe(first.createTime);
    expect(second.updateTime.getTime()).toBeGreaterThanOrEqual(first.updateTime.getTime());
    // The old lifecycle's stream learned the ABORTED terminal frame and ended.
    expect(stream.events.some((event) => event.turnEnd?.status === "TURN_STATUS_ABORTED")).toBe(true);
    expect(stream.ended).toBe(true);
    // Fresh lifecycle: projections restart empty.
    expect(h.sessions.listTeamMessages(S1)).toEqual([]);
  });
});

describe("TeamSessions planner memory wiring (T021)", () => {
  it("prefetches the planner snapshot through the composed ctx.plannerMemory.load", async () => {
    const composedLoad = vi.fn(async () => {});
    const h = createHarness({ composedPlannerMemory: { load: composedLoad } });

    await materializeDefault(h);

    // Only the planner member prefetches; the binding passes the planner's
    // agent-scoped context and the (template, session ID) scope key — the ID
    // half, NOT the full session resource name (the memory client builds
    // `templates/{template}/sessions/{session}` from these halves; T023 memoryScope wiring (specs/059-agent-v2-team-mode/tasks.md)
    // caught the doubled-prefix wiring).
    expect(composedLoad).toHaveBeenCalledTimes(1);
    const planner = member(h, PLANNER_ID);
    expect(composedLoad.mock.calls[0]?.[0]).toBe(planner.ctx);
    expect(composedLoad.mock.calls[0]?.[1]).toEqual({
      template: "saolei",
      session: "s1",
    });
  });

  it("rolls the whole materialization back when the composed load rejects (no half-materialized team)", async () => {
    const composedLoad = vi.fn(async () => {
      throw new Error("memory service unreachable");
    });
    const h = createHarness({ composedPlannerMemory: { load: composedLoad } });

    await expect(materializeDefault(h)).rejects.toThrow("memory service unreachable");
    const player = member(h, PLAYER_ID);
    expect(player.handle.dispose).toHaveBeenCalledTimes(1);
    expect(() => h.sessions.getTeam(S1)).toThrow(TeamSessionError);
    // The team never registered: the rollback is complete.
    expect(h.teamRegister).not.toHaveBeenCalled();

    // Retry with the storage reachable materializes from scratch.
    composedLoad.mockImplementation(async () => {});
    await materializeDefault(h);
    expect(h.sessions.getTeam(S1).name).toBe(`${S1}/team`);
    expect(composedLoad).toHaveBeenCalledTimes(2);
  });

  it("fails loud when neither the composed service nor the test override is present", async () => {
    const h = createHarness({ omitPlannerMemory: true });

    await expect(materializeDefault(h)).rejects.toThrow(/ctx\.plannerMemory/);
    expect(h.agentsCreate).not.toHaveBeenCalled();
  });
});

describe("TeamSessions.send", () => {
  it("rejects an unmaterialized session with FAILED_PRECONDITION before any frame", () => {
    const h = createHarness();
    const stream = fakeStream();

    expect(() => h.sessions.send(S1, "hello", stream)).toThrow(TeamSessionError);
    try {
      h.sessions.send(S1, "hello", stream);
    } catch (err) {
      expect((err as TeamSessionError).code).toBe("FAILED_PRECONDITION");
    }
    expect(stream.events).toEqual([]);
    expect(h.agentsCreate).not.toHaveBeenCalled();
  });

  it("fixes the user message into the merged sequence, drives planner first, and ends the stream at team quiescence", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);
    const player = member(h, PLAYER_ID);

    const stream = fakeStream();
    h.sessions.send(S1, "请开始扫雷", stream);
    await flush();

    // Initial activation = planner; the user message is the drive input.
    expect(planner.followups.map(messageText)).toEqual(["请开始扫雷"]);
    // Acceptance frame: team_message{member=USER} with seq 1.
    const userFrame = stream.events.find((event) => event.teamMessage !== undefined);
    expect(userFrame?.teamMessage?.member).toBe("user");
    expect(userFrame?.teamMessage?.seq).toBe("1");
    expect(stream.ended).toBe(false);

    // Planner produces output and quiesces; the pump switches to the player,
    // whose relay set is empty — the team reaches its static point and the
    // stream ends (no synthesized input).
    driveTextTurn(h, planner, "开局策略：先点中心");
    await flush();

    expect(player.followups).toHaveLength(0);
    expect(stream.ended).toBe(true);
    const entries = h.sessions.listTeamMessages(S1);
    expect(entries.map((entry) => entry.member)).toEqual(["user", "planner"]);
    expect(entries[1]?.message.blocks[0]?.text?.content).toBe("开局策略：先点中心");
    // Every member event frame is member-labelled.
    for (const event of stream.events) {
      if (event.teamMessage === undefined && event.queued === undefined) {
        expect(event.member).toBe("planner");
      }
    }
  });

  it("queues a mid-turn send as this stream's first frame and fans the rest out to every active stream", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);

    const first = fakeStream();
    h.sessions.send(S1, "first", first);
    await flush();
    expect(planner.followups).toHaveLength(1);

    const second = fakeStream();
    h.sessions.send(S1, "second", second);
    await flush();

    expect(second.events.map(payloadOf)[0]).toBe("queued");
    expect(second.events[0]?.queued?.position).toBe(1);
    // The user entry frame reaches both streams.
    expect(first.events.some((event) => event.teamMessage?.seq === "2")).toBe(true);
    expect(second.events.some((event) => event.teamMessage?.seq === "2")).toBe(true);

    // Settling both turns drains the queue (queue digestion) and quiesces.
    driveTextTurn(h, planner, "reply one");
    await flush();
    driveTextTurn(h, planner, "reply two");
    await flush();

    expect(planner.followups).toHaveLength(2);
    expect(first.ended).toBe(true);
    expect(second.ended).toBe(true);
    expect(h.sessions.listTeamMessages(S1).map((entry) => entry.member)).toEqual([
      "user",
      "user",
      "planner",
      "planner",
    ]);
  });

  it("detaching a stream on client disconnect does not cancel the orchestration", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);
    const stream = fakeStream();

    const detach = h.sessions.send(S1, "hello", stream);
    await flush();
    expect(planner.followups).toHaveLength(1);

    detach();
    await driveTextTurn(h, planner, "reply");
    await flush();

    // The disconnecting subscriber saw no frames after detach, and no
    // agent.cancel was ever issued (cancel only happens via the Cancel RPC).
    expect(stream.ended).toBe(false);
    expect(planner.cancels).toHaveLength(0);
  });
});

describe("TeamSessions.cancel", () => {
  it("terminates the in-flight turn with CANCELED, preserves history, and ends streams at the static point", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);

    const stream = fakeStream();
    h.sessions.send(S1, "first", stream);
    await flush();

    h.sessions.cancel(S1);
    await flush();

    expect(planner.cancels).toEqual([{ kind: "user" }]);
    const end = stream.events.find((event) => event.turnEnd !== undefined);
    expect(end?.turnEnd?.status).toBe("TURN_STATUS_CANCELED");
    expect(stream.ended).toBe(true);
    // The message stays fixed in the merged sequence (enqueue-time fixation).
    expect(h.sessions.listTeamMessages(S1).map((entry) => entry.member)).toEqual(["user"]);

    // Idempotent no-op while paused.
    expect(() => h.sessions.cancel(S1)).not.toThrow();
    // A new send resumes the loop with the current activation.
    const resumed = fakeStream();
    h.sessions.send(S1, "resume", resumed);
    await flush();
    expect(planner.followups).toHaveLength(2);
    await driveTextTurn(h, planner, "reply");
    await flush();
    expect(resumed.ended).toBe(true);
  });

  it("rejects an unmaterialized session with FAILED_PRECONDITION", () => {
    const h = createHarness();
    expect(() => h.sessions.cancel(S1)).toThrow(TeamSessionError);
    expect(h.agentsCreate).not.toHaveBeenCalled();
  });
});

describe("TeamSessions active member projection (FR-008)", () => {
  it("serves planner at rest, the in-flight driving member, and the next input's owner", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);
    const player = member(h, PLAYER_ID);

    // Materialized and quiescent: the initial activation is planner.
    expect(h.sessions.getTeam(S1).activeMember).toBe("planner");

    // The first send drives the planner: the merged value is the driving
    // member (the activation equals it while a turn is in flight).
    const stream = fakeStream();
    h.sessions.send(S1, "请开始", stream);
    await flush();
    expect(h.sessions.getTeam(S1).activeMember).toBe("planner");

    // The planner relays a strategy; its quiescence structurally drives the
    // player and both the driving member and the activation follow.
    h.teamQueues.set(PLAYER_ID, [relay(planner.agent, "planner", "开局策略")]);
    driveTextTurn(h, planner, "开局策略");
    await flush();
    expect(h.sessions.getTeam(S1).activeMember).toBe("player");

    // The player quiesces without a terminal record: at rest the next input
    // still belongs to the player.
    driveTextTurn(h, player, "已点击 (0,0)");
    await flush();
    expect(stream.ended).toBe(true);
    expect(h.sessions.getTeam(S1).activeMember).toBe("player");
  });

  it("keeps the activation when the in-flight turn is canceled", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);
    const player = member(h, PLAYER_ID);

    // Cancel mid-planner-turn: the planner stays the next input's owner.
    h.sessions.send(S1, "first", fakeStream());
    await flush();
    h.sessions.cancel(S1);
    await flush();
    expect(h.sessions.getTeam(S1).activeMember).toBe("planner");

    // A resumed send drives the planner again; its relay moves the team to
    // the player, then a mid-player-turn cancel keeps the player activation.
    const resumed = fakeStream();
    h.sessions.send(S1, "resume", resumed);
    await flush();
    expect(planner.followups.map(messageText)).toEqual(["first", "resume"]);
    h.teamQueues.set(PLAYER_ID, [relay(planner.agent, "planner", "策略")]);
    driveTextTurn(h, planner, "策略");
    await flush();
    expect(h.sessions.getTeam(S1).activeMember).toBe("player");

    h.sessions.cancel(S1);
    await flush();
    expect(h.sessions.getTeam(S1).activeMember).toBe("player");

    // The resumed send is handled by the kept activation.
    h.sessions.send(S1, "继续", fakeStream());
    await flush();
    expect(player.followups.map(messageText)).toEqual(["策略", "继续"]);
  });
});

describe("TeamSessions orchestration failure", () => {
  it("breaks out of a paused, failed orchestration with a stream error instead of spinning or ending cleanly", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);

    // The first send starts a planner turn; two more sends queue behind it.
    const stream = fakeStream();
    h.sessions.send(S1, "first", stream);
    await flush();
    expect(planner.followups).toHaveLength(1);

    const second = fakeStream();
    h.sessions.send(S1, "second", second);
    const third = fakeStream();
    h.sessions.send(S1, "third", third);
    await flush();
    expect(second.events.map(payloadOf)[0]).toBe("queued");

    // The second drive fails while a third message still waits in the queue:
    // the orchestrator pauses with a non-empty queue, so the old watcher's
    // `whenQuiescent()` resolved immediately and spun the microtask queue.
    (planner.agent.followup as ReturnType<typeof vi.fn>).mockImplementationOnce(() => {
      throw new Error("drive exploded");
    });
    planner.settle();
    await flush();

    // The failure is surfaced, not disguised as a natural stop: every active
    // stream receives the INTERNAL failure and the injected logger records it.
    expect(stream.failures).toEqual([{ code: "INTERNAL", message: "drive exploded" }]);
    expect(second.failures).toEqual([{ code: "INTERNAL", message: "drive exploded" }]);
    expect(third.failures).toEqual([{ code: "INTERNAL", message: "drive exploded" }]);
    expect(stream.ended).toBe(false);
    expect(h.loggerError).toHaveBeenCalledTimes(1);
    expect(h.loggerError.mock.calls[0]?.[1]).toMatchObject({
      session: S1,
      member: "planner",
      error: "drive exploded",
    });

    // A later send lifts the pause and drains the held queue (orchestrator
    // retry semantics), then the team quiesces and the new stream ends.
    const resumed = fakeStream();
    h.sessions.send(S1, "resume", resumed);
    await flush();
    // The held "third" message retries first (the failed attempt consumed
    // nothing from the FIFO beyond its own shift).
    expect(planner.followups).toHaveLength(2);
    planner.settle();
    await flush();
    expect(planner.followups).toHaveLength(3);
    planner.settle();
    await flush();
    expect(resumed.failures).toEqual([]);
    expect(resumed.ended).toBe(true);
  });
});

describe("TeamSessions member-turn failure", () => {
  it("ends the stream cleanly with the turn_end ERROR frame and re-drives the retained member on the next send", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);

    // The first send starts the planner turn.
    const stream = fakeStream();
    h.sessions.send(S1, "请开始扫雷", stream);
    await flush();
    expect(planner.followups.map(messageText)).toEqual(["请开始扫雷"]);

    // The turn fails at its active boundary (agent/error) and then idles.
    planner.failCurrentTurn("SERVER");
    await flush();

    // A member-turn failure settles the stream cleanly — the turn_end ERROR
    // frame is on the stream (sunk by the member history collector before
    // the watcher's microtask) and no INTERNAL failure is raised
    // (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §2).
    expect(stream.failures).toEqual([]);
    expect(stream.ended).toBe(true);
    const ends = stream.events.filter((event) => event.turnEnd !== undefined);
    expect(ends).toHaveLength(1);
    expect(ends[0]?.turnEnd).toMatchObject({
      status: "TURN_STATUS_ERROR",
      error: { code: "SERVER", message: "injected SERVER failure" },
    });
    expect(stream.events[stream.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_ERROR");

    // FR-012: exactly one structured failure line with the full field set.
    expect(h.loggerError).toHaveBeenCalledTimes(1);
    expect(h.loggerError.mock.calls[0]?.[1]).toEqual({
      session: S1,
      phase: "planning",
      member: "planner",
      code: "SERVER",
      error: "injected SERVER failure",
    });
    // FR-009: the activation is retained — planner owns the next input.
    expect(h.sessions.getTeam(S1).activeMember).toBe("planner");

    // The next send re-drives the same member; the recovered turn answers
    // normally and the new stream ends at the team's static point.
    const resumed = fakeStream();
    h.sessions.send(S1, "重试", resumed);
    await flush();
    expect(planner.followups.map(messageText)).toEqual(["请开始扫雷", "重试"]);
    driveTextTurn(h, planner, "规划完成");
    await flush();
    expect(resumed.failures).toEqual([]);
    expect(resumed.ended).toBe(true);
    const completed = resumed.events.filter((event) => event.turnEnd !== undefined);
    expect(completed).toHaveLength(1);
    expect(completed[0]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
  });
});

describe("TeamSessions projections", () => {
  it("projects the member view with sender annotations and the team view with member states", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);

    // The planner's log records a relayed player broadcast and its own reply.
    // A `user/message` event stores the complete UserMessage as its data.
    emit(h, "session/event", planner.agent.session, {
      type: "user/message",
      data: {
        id: "m-relay",
        content: [{ type: "text", text: "[player] 已点击 (0,0)" }],
        source: { kind: "team-broadcast", role: "player" },
      },
    });
    driveTextTurn(h, planner, "复盘完成");
    await flush();

    const view = h.sessions.listMemberMessages(S1, "planner");
    expect(view.map((entry) => entry.sender)).toEqual(["player", "planner"]);
    expect(view[0]?.message.role).toBe("ROLE_USER");
    expect(view[1]?.message.role).toBe("ROLE_AGENT");

    const teamMember = await h.sessions.getTeamMember(S1, "player");
    expect(teamMember.role).toBe("player");
    expect(teamMember.model).toBe(DEFAULT_MODEL);
    expect(teamMember.preset).toBe(P_PLAYER);
    expect(teamMember.systemPrompt).toContain(`system prompt of ${PLAYER_ID}`);
    // The team snapshot member projection keeps system_prompt empty (the
    // field is served only by GetTeamMember).
    expect(h.sessions.getTeam(S1).members.every((entry) => entry.systemPrompt === "")).toBe(true);
    await expect(h.sessions.getTeamMember(S1, "robot")).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
  });

  it("reads system_prompt off each member instance's assembly surface and follows a refresh", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const oldPlayer = member(h, PLAYER_ID);

    const player = await h.sessions.getTeamMember(S1, "player");
    const planner = await h.sessions.getTeamMember(S1, "planner");
    expect(player.systemPrompt).toContain(`system prompt of ${PLAYER_ID}`);
    expect(planner.systemPrompt).toContain(`system prompt of ${PLANNER_ID}`);
    expect(oldPlayer.ctx.systemPrompt.assemble).toHaveBeenCalledTimes(1);

    // A refresh builds fresh member instances: the read goes through the new
    // instance's assembly surface (the old handle is never consulted), so the
    // served content follows the new configuration.
    await h.sessions.materialize(S1, { members: defaultMembers() });
    const newPlayer = member(h, PLAYER_ID);
    expect(newPlayer).not.toBe(oldPlayer);
    newPlayer.ctx.systemPrompt.assemble.mockResolvedValueOnce({
      sections: [{ name: "deployment:persona", text: "刷新后的 player persona" }],
      contexts: [],
      tools: [],
      variables: {},
    });

    const refreshed = await h.sessions.getTeamMember(S1, "player");
    expect(refreshed.systemPrompt).toContain("刷新后的 player persona");
    expect(oldPlayer.ctx.systemPrompt.assemble).toHaveBeenCalledTimes(1);
    expect(newPlayer.ctx.systemPrompt.assemble).toHaveBeenCalledTimes(1);
  });

  it("propagates a member assembly failure (mapped to INTERNAL at the RPC layer)", async () => {
    const h = createHarness();
    await materializeDefault(h);
    member(h, PLAYER_ID).ctx.systemPrompt.assemble.mockRejectedValueOnce(
      new Error("assembly exploded"),
    );

    await expect(h.sessions.getTeamMember(S1, "player")).rejects.toThrow("assembly exploded");
  });

  it("re-reads the new generation when a refresh lands mid-read (never a partial prompt)", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const oldPlayer = member(h, PLAYER_ID);

    // Hold the old instance's assembly in flight.
    let release!: (assembly: PromptAssembly) => void;
    oldPlayer.ctx.systemPrompt.assemble.mockReturnValueOnce(
      new Promise<PromptAssembly>((resolve) => {
        release = resolve;
      }),
    );
    const read = h.sessions.getTeamMember(S1, "player");
    await flush();

    // The refresh tears the old entry down and materializes the new instance
    // while the read is still awaiting the old assembly.
    await h.sessions.materialize(S1, { members: defaultMembers() });
    const newPlayer = member(h, PLAYER_ID);
    expect(newPlayer).not.toBe(oldPlayer);

    // The superseded instance resolves with a global-layers-only prompt: the
    // disposed scope dropped persona/team/guidance, so serving it would be a
    // partial result (neither the old nor the new content).
    release({
      sections: [{ name: "harness:identity", text: "GLOBAL LAYER ONLY" }],
      contexts: [],
      tools: [],
      variables: {},
    });

    const result = await read;
    expect(result.systemPrompt).toContain(`system prompt of ${PLAYER_ID}`);
    expect(result.systemPrompt).not.toContain("GLOBAL LAYER ONLY");
    // The served state also belongs to the new generation.
    expect(result.model).toBe(DEFAULT_MODEL);
    expect(oldPlayer.ctx.systemPrompt.assemble).toHaveBeenCalledTimes(1);
    expect(newPlayer.ctx.systemPrompt.assemble).toHaveBeenCalledTimes(1);
  });

  it("retries onto the current generation when the superseded instance's assembly rejects", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const oldPlayer = member(h, PLAYER_ID);

    // The old assembly rejects while a refresh replaces the entry: the
    // teardown failure belongs to the superseded generation.
    let rejectAssembly!: (err: Error) => void;
    oldPlayer.ctx.systemPrompt.assemble.mockReturnValueOnce(
      new Promise<PromptAssembly>((_resolve, reject) => {
        rejectAssembly = reject;
      }),
    );
    const read = h.sessions.getTeamMember(S1, "player");
    await flush();
    await h.sessions.materialize(S1, { members: defaultMembers() });
    const newPlayer = member(h, PLAYER_ID);

    rejectAssembly(new Error("scope unwound mid-assembly"));
    const result = await read;

    expect(result.systemPrompt).toContain(`system prompt of ${PLAYER_ID}`);
    expect(newPlayer.ctx.systemPrompt.assemble).toHaveBeenCalledTimes(1);
  });

  it("maps a mid-read teardown without replacement to NOT_FOUND", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const player = member(h, PLAYER_ID);

    let release!: (assembly: PromptAssembly) => void;
    player.ctx.systemPrompt.assemble.mockReturnValueOnce(
      new Promise<PromptAssembly>((resolve) => {
        release = resolve;
      }),
    );
    const read = h.sessions.getTeamMember(S1, "player");
    await flush();

    // The team is torn down with no replacement; the in-flight read must not
    // serve the (partial) superseded result.
    await h.sessions.shutdown();
    release({
      sections: [{ name: "harness:identity", text: "GLOBAL LAYER ONLY" }],
      contexts: [],
      tools: [],
      variables: {},
    });

    await expect(read).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("shutdown disposes every member and the composition fiber last", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);
    const order: string[] = [];
    (player.handle.dispose as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      order.push("player");
    });
    (planner.handle.dispose as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      order.push("planner");
    });
    h.fiberDispose.mockImplementation(async () => {
      order.push("fiber");
    });

    await h.sessions.shutdown();

    expect(order).toContain("player");
    expect(order).toContain("planner");
    expect(order[order.length - 1]).toBe("fiber");
    expect(() => h.sessions.getTeam(S1)).toThrow(TeamSessionError);
  });
});
