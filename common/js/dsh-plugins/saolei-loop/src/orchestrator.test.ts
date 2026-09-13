/**
 * TeamOrchestrator tests: materialization (compose → create → team
 * registration, then resting until the first user send), the
 * alternating-activation state machine (first send → planner; planner
 * quiescence → player; gameEnded → planner review; reviewing → player),
 * queue-first digestion before a switch, cancel / pause / resume,
 * materialization rollback, disposal, failure reporting (logger + snapshot
 * failure state, review retry after a failed drive), and the
 * no-synthesized-input rule (every drive carries only team relays and user
 * messages). The state graph under test is
 * specs/059-agent-v2-team-mode/data-model.md §5; the behavior contract is
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §2.
 *
 * Pattern (style/javascript.md Mock convention): every collaborator is a
 * `vi.fn()`/typed fake injected through {@link TeamOrchestratorDeps} — no
 * module interception. The fake member drives the agent lifecycle by hand
 * (`settle()` emits `agent/status` idle), so the tests read as the exact
 * event order the official loop would produce.
 */

import { Context } from "@deepseek-ai/cordis";
import type {
  Agent,
  AgentHandle,
  AgentStatus,
  CreateAgentOptions,
} from "@deepseek-ai/dsh-agent";
import { createUserMessage, LlmError, MessageId } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import type {
  TeamHandle,
  TeamRegistration,
} from "@dominion/dsh-team";
import { describe, expect, it, vi } from "vitest";

import {
  DEFAULT_MEMBER_SUMMARIES,
  OrchestratorStateError,
  TeamOrchestrator,
} from "./orchestrator.js";
import type {
  ComposedPreset,
  GameEventSource,
  TeamMaterializeOptions,
  TeamOrchestratorDeps,
} from "./orchestrator.js";
import type { GameEventRecord } from "./game/runtime.js";

const SESSION = "templates/saolei/sessions/s1";
/** The business session ID half of the memory scope key (NOT the full name). */
const SESSION_ID = "s1";
const PLAYER_ID = `${SESSION}/player`;
const PLANNER_ID = `${SESSION}/planner`;

/** Flush every pending microtask (one macrotask boundary). */
function tick(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

function messageText(message: UserMessage): string {
  const block = message.content[0];
  return block !== undefined && block.type === "text" ? block.text : "";
}

/**
 * A drained team relay as the member log would carry it: a `team-broadcast`
 * sourced user message (the orchestrator only forwards it). `senderSessionId`
 * is the producing member's durable id.
 */
function relay(
  senderSessionId: Agent["id"],
  role: string,
  text: string,
): UserMessage {
  return createUserMessage({
    content: [{ type: "text", text }],
    source: {
      kind: "team-broadcast",
      role,
      senderSessionId,
      messageId: MessageId(`m-${text}`),
      form: "relay",
    },
  });
}

/** The `agent/status` payload the official loop emits. */
type StatusListener = (payload: { agent: Agent; status: AgentStatus }) => void;

/** The `agent/error` payload the official loop emits at a failed turn's boundary. */
interface AgentErrorPayload {
  agent: Agent;
  turn: number;
  step: number;
  error: unknown;
}

type ErrorListener = (payload: AgentErrorPayload) => void;

interface FakeMember {
  readonly id: string;
  readonly agent: Agent;
  readonly ctx: Context;
  readonly handle: AgentHandle;
  readonly followups: UserMessage[];
  readonly injections: UserMessage[];
  readonly cancels: Array<{ cause: unknown; options: unknown }>;
  readonly statusListeners: StatusListener[];
  readonly errorListeners: ErrorListener[];
  settle(): void;
  /**
   * Settle the in-flight turn as failed: emit `agent/error` (an
   * {@link LlmError} carrying the code) and then idle — the exact order the
   * official loop produces
   * (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §1/§5).
   */
  failCurrentTurn(code: string): void;
  /** Make the next followup throw (one-shot drive-failure injection). */
  failNextFollowup(): void;
}

/**
 * One agent double: `followup` records + flips running, `inject` records,
 * `cancel` records + (like the real loop's converging abort) emits idle, and
 * `settle()` completes a turn.
 */
function createFakeMember(name: string): FakeMember {
  const followups: UserMessage[] = [];
  const injections: UserMessage[] = [];
  const cancels: Array<{ cause: unknown; options: unknown }> = [];
  const statusListeners: StatusListener[] = [];
  const errorListeners: ErrorListener[] = [];
  const state = { status: "idle" as AgentStatus };
  let failFollowup = false;
  const off = <T>(list: T[], listener: T): (() => void) => () => {
    const index = list.indexOf(listener);
    if (index >= 0) {
      list.splice(index, 1);
    }
  };
  const ctx = {
    on: vi.fn((event: string, listener: StatusListener | ErrorListener) => {
      if (event === "agent/error") {
        const typed = listener as ErrorListener;
        errorListeners.push(typed);
        return off(errorListeners, typed);
      }
      const typed = listener as StatusListener;
      statusListeners.push(typed);
      return off(statusListeners, typed);
    }),
  } as { on: unknown; agent?: Agent };

  const id = name as Agent["id"];
  const emitIdle = (): void => {
    state.status = "idle";
    for (const listener of [...statusListeners]) {
      listener({ agent, status: "idle" });
    }
  };
  const agent = {
    id,
    session: { id, events: [] },
    get status(): AgentStatus {
      return state.status;
    },
    ctx,
    inject: vi.fn((message: UserMessage) => {
      injections.push(message);
    }),
    followup: vi.fn((message: UserMessage) => {
      if (failFollowup) {
        failFollowup = false;
        throw new Error("followup rejected");
      }
      followups.push(message);
      state.status = "running";
    }),
    cancel: vi.fn((cause: unknown, options?: unknown) => {
      cancels.push({ cause, options });
      emitIdle();
    }),
    whenIdle: vi.fn(async () => {}),
  } as unknown as Agent;
  ctx.agent = agent;

  return {
    id: name,
    agent,
    ctx: ctx as unknown as Context,
    handle: { agent, dispose: vi.fn(async () => {}) },
    followups,
    injections,
    cancels,
    statusListeners,
    errorListeners,
    settle: emitIdle,
    failCurrentTurn: (code: string) => {
      // The official loop reports the failure at the turn's active boundary
      // and only then converges to idle, so the marker is visible when the
      // drive's idle await resolves
      // (specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md §1/§5).
      const error = new LlmError(`injected ${code} failure`, code);
      for (const listener of [...errorListeners]) {
        listener({ agent, turn: 1, step: 1, error });
      }
      emitIdle();
    },
    failNextFollowup: () => {
      failFollowup = true;
    },
  };
}

function fakeAgents(registry: Map<string, FakeMember>): {
  create: ReturnType<typeof vi.fn>;
  calls: CreateAgentOptions[];
} {
  const calls: CreateAgentOptions[] = [];
  return {
    calls,
    create: vi.fn(async (options: CreateAgentOptions): Promise<AgentHandle> => {
      calls.push(options);
      const member = createFakeMember(String(options.sessionId));
      registry.set(member.id, member);
      await options.setup?.(member.ctx);
      return member.handle;
    }),
  };
}

interface FakeTeam {
  readonly seam: {
    register: ReturnType<typeof vi.fn>;
    drain: ReturnType<typeof vi.fn>;
  };
  readonly queues: Map<string, UserMessage[]>;
  readonly registrations: TeamRegistration[];
  readonly registrationDispose: ReturnType<typeof vi.fn>;
}

function fakeTeam(): FakeTeam {
  const queues = new Map<string, UserMessage[]>();
  const registrations: TeamRegistration[] = [];
  const registrationDispose = vi.fn();
  return {
    queues,
    registrations,
    registrationDispose,
    seam: {
      register: vi.fn((registration: TeamRegistration): TeamHandle => {
        registrations.push(registration);
        return { dispose: registrationDispose };
      }),
      drain: vi.fn((member: AgentHandle): UserMessage[] => {
        return queues.get(String(member.agent.id))?.splice(0) ?? [];
      }),
    },
  };
}

interface Harness {
  readonly orchestrator: TeamOrchestrator;
  readonly members: Map<string, FakeMember>;
  readonly agents: ReturnType<typeof fakeAgents>;
  readonly team: FakeTeam;
  readonly compose: ReturnType<typeof vi.fn>;
  readonly setups: Array<ReturnType<typeof vi.fn>>;
  readonly mountPlayerRuntime: ReturnType<typeof vi.fn>;
  readonly loadPlannerMemory: ReturnType<typeof vi.fn>;
  readonly game: { current: GameEventRecord | null; peeks: number };
}

function createHarness(
  overrides: Partial<TeamOrchestratorDeps> = {},
): Harness {
  const members = new Map<string, FakeMember>();
  const agents = fakeAgents(members);
  const team = fakeTeam();
  const setups: Array<ReturnType<typeof vi.fn>> = [];
  const compose = vi.fn(async (preset: string): Promise<ComposedPreset> => {
    const setup = vi.fn(async () => {});
    setups.push(setup);
    return { agentPreset: `${preset}-resolved`, setup };
  });
  const game = { current: null as GameEventRecord | null, peeks: 0 };
  const mountPlayerRuntime = vi.fn(
    (): GameEventSource => ({
      peekGameEvent: () => {
        game.peeks += 1;
        return game.current;
      },
    }),
  );
  const loadPlannerMemory = vi.fn(async () => {});
  const orchestrator = new TeamOrchestrator(new Context(), {
    compose,
    agents,
    team: team.seam,
    mountPlayerRuntime,
    loadPlannerMemory,
    ...overrides,
  });
  return {
    orchestrator,
    members,
    agents,
    team,
    compose,
    setups,
    mountPlayerRuntime,
    loadPlannerMemory,
    game,
  };
}

/** The created fake member for a session id (materialization has run). */
function member(h: Harness, id: string): FakeMember {
  const found = h.members.get(id);
  if (found === undefined) {
    throw new Error(`fake member "${id}" is not materialized`);
  }
  return found;
}

/** Every message the orchestrator delivered to a member turn. */
function deliveredMessages(h: Harness): UserMessage[] {
  return [...h.members.values()].flatMap((fake) => [
    ...fake.injections,
    ...fake.followups,
  ]);
}

/** FR-010: a drive message can only be a team relay or a user send. */
function expectRelayOrUserSources(h: Harness): void {
  for (const message of deliveredMessages(h)) {
    expect(["team-broadcast", "user"]).toContain(message.source.kind);
  }
}

function teamOptions(
  overrides: Partial<TeamMaterializeOptions> = {},
): TeamMaterializeOptions {
  return {
    session: SESSION,
    memoryScope: { template: "saolei", session: SESSION_ID },
    goal: "协作提高扫雷胜率",
    player: { preset: "player-a", model: "model-p" },
    planner: { preset: "planner-a", model: "model-q" },
    ...overrides,
  };
}

function winner(): GameEventRecord {
  return {
    status: "won",
    stats: { operationCount: 5, correctFlags: 3, avgOpsPerMine: 1.67 },
    endedAt: 1_000,
  };
}

describe("TeamOrchestrator.materialize", () => {
  it("creates both members through the compose/setup chain and rests until the first user send", async () => {
    const h = createHarness();

    await h.orchestrator.materialize(teamOptions());

    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);
    expect(h.compose).toHaveBeenNthCalledWith(1, "player-a");
    expect(h.compose).toHaveBeenNthCalledWith(2, "planner-a");
    // Creation meta and model routing (agentOptions) per member.
    expect(h.agents.calls[0]?.meta?.agentPreset).toBe("player-a-resolved");
    expect(h.agents.calls[0]?.agentOptions).toEqual({
      provider: "glm-responses",
      model: "model-p",
    });
    expect(h.agents.calls[1]?.meta?.agentPreset).toBe("planner-a-resolved");
    expect(h.agents.calls[1]?.agentOptions).toEqual({
      provider: "glm-responses",
      model: "model-q",
    });
    // Setup chain: roster mount for both; player mounts the game runtime
    // (bound to the GAME session resource name — the desktop-bridge key, not
    // the namespaced member session id), planner prefetches the memory
    // snapshot with the (template, session) key.
    expect(h.setups[0]).toHaveBeenCalledWith(player.ctx);
    expect(h.setups[1]).toHaveBeenCalledWith(planner.ctx);
    expect(h.mountPlayerRuntime).toHaveBeenCalledWith(player.agent, SESSION);
    expect(h.mountPlayerRuntime).not.toHaveBeenCalledWith(planner.agent, SESSION);
    expect(h.loadPlannerMemory).toHaveBeenCalledWith(planner.ctx, {
      template: "saolei",
      // The ID half, not the full session resource name: the memory plugin
      // builds `templates/{template}/sessions/{session}` from these halves
      // (T023 wiring fix).
      session: SESSION_ID,
    });
    // Team registration: goal + the two-member roster with defaults.
    expect(h.team.registrations).toHaveLength(1);
    const registration = h.team.registrations[0] as TeamRegistration;
    expect(registration.goal).toBe("协作提高扫雷胜率");
    expect(
      registration.members.map((registered) => [
        registered.role,
        String(registered.agent.agent.id),
        registered.summary,
      ]),
    ).toEqual([
      ["player", PLAYER_ID, DEFAULT_MEMBER_SUMMARIES.player],
      ["planner", PLANNER_ID, DEFAULT_MEMBER_SUMMARIES.planner],
    ]);

    // Materialization rests in the planning activation: nothing is driven
    // and no drive message is synthesized (FR-010).
    expect(h.orchestrator.snapshot()).toEqual({
      materialized: true,
      phase: "planning",
      active: null,
      activation: "planner",
      paused: false,
      queued: 0,
      failed: false,
      lastError: null,
    });
    expect(planner.followups).toHaveLength(0);
    expect(planner.injections).toHaveLength(0);
    expect(player.followups).toHaveLength(0);
    expect(player.injections).toHaveLength(0);

    // The first user send starts the first turn: the initial activation
    // (planner) consumes it together with its (empty) relay set.
    const submitted = h.orchestrator.submit("请开始扫雷工作");
    expect(submitted.queued).toBe(false);
    expect(planner.followups.map(messageText)).toEqual(["请开始扫雷工作"]);
    expect(planner.followups[0]?.source).toMatchObject({ kind: "user" });
    expect(planner.injections).toHaveLength(0);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "planning",
      active: "planner",
      activation: "planner",
    });

    planner.settle();
    await h.orchestrator.whenQuiescent();
    // The planner turn quiesced: the activation switches to the player.
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: null,
      activation: "player",
    });
    expectRelayOrUserSources(h);
  });

  it("routes each member through its own provider, falling back to the team provider", async () => {
    const h = createHarness({ provider: "team-route" });

    await h.orchestrator.materialize(
      teamOptions({
        player: { preset: "player-a", provider: "opencode-go", model: "kimi-k3" },
        planner: { preset: "planner-a", model: "model-q" },
      }),
    );

    // The member's own provider wins; a member without one falls back to the
    // team-level provider (specs/063-llm-reliability-opencode-go/contracts/
    // model-selection.md §3). The model stays the bare id either way.
    expect(h.agents.calls[0]?.agentOptions).toEqual({
      provider: "opencode-go",
      model: "kimi-k3",
    });
    expect(h.agents.calls[1]?.agentOptions).toEqual({
      provider: "team-route",
      model: "model-q",
    });
  });

  it("rejects a second materialization and submit after disposal", async () => {
    const h = createHarness();
    expect(() => h.orchestrator.submit("x")).toThrow(OrchestratorStateError);
    expect(() => h.orchestrator.cancel()).toThrow(OrchestratorStateError);

    await h.orchestrator.materialize(teamOptions());
    await expect(h.orchestrator.materialize(teamOptions())).rejects.toMatchObject({
      code: "ALREADY_MATERIALIZED",
    });

    await h.orchestrator.dispose();
    expect(() => h.orchestrator.submit("x")).toThrow(/disposed/);
    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });
  });
});

describe("TeamOrchestrator transitions", () => {
  it("drives the player with the whole drained relay set in one turn after the first planner turn quiesces", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first user send drives the planner; its output relays to the player.
    h.orchestrator.submit("给我开局策略");
    h.team.queues.set(PLAYER_ID, [
      relay(planner.agent.id, "planner", "策略一"),
      relay(planner.agent.id, "planner", "策略二"),
    ]);

    planner.settle();
    await tick();

    // The batch enters one turn: next-step input (inject) plus the queued
    // turn (followup last) — survey/deepseek-harness-team-mode.md §4.4 note 3.
    expect(player.injections.map(messageText)).toEqual(["策略一"]);
    expect(player.followups.map(messageText)).toEqual(["策略二"]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: "player",
      activation: "player",
    });

    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: null,
      activation: "player",
    });
    expectRelayOrUserSources(h);
  });

  it("drives the planner review on an unreviewed gameEnded record and consumes it once", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // First user send → planner; the planner turn relays the strategy → player.
    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(1);

    // The player's turn ends with a terminal record: review the planner.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "本局对局记录")]);
    player.settle();
    await tick();
    expect(planner.followups.map(messageText)).toEqual(["请开始", "本局对局记录"]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      active: "planner",
      activation: "planner",
    });

    // Review quiesces; without new player output the loop rests, and the pump
    // transition leaves the next input to the player (activation).
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(player.followups).toHaveLength(1);
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: null,
      activation: "player",
    });

    // A later player turn still observing the same record must not re-review.
    h.orchestrator.submit("继续");
    await tick();
    expect(player.followups).toHaveLength(2);
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: "player",
      activation: "player",
    });
    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(planner.followups).toHaveLength(2);
    // Evaluated after both player turns; the second evaluation saw the same
    // record and did not re-trigger a review.
    expect(h.game.peeks).toBe(2);
    expectRelayOrUserSources(h);
  });

  it("digests a queued user message with the current member before switching", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first user send drives the planner; a second one queues behind it.
    h.orchestrator.submit("首条");
    const submitted = h.orchestrator.submit("先消化这条");
    expect(submitted.queued).toBe(true);
    expect(submitted.position).toBe(1);
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "策略")]);

    planner.settle();
    await tick();
    // The planner consumed the queue first; the player drive is deferred.
    expect(planner.followups.map(messageText)).toEqual(["首条", "先消化这条"]);
    expect(planner.followups[1]?.source).toMatchObject({ kind: "user" });
    expect(player.followups).toHaveLength(0);

    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(1);
    player.settle();
    await h.orchestrator.whenQuiescent();
    expectRelayOrUserSources(h);
  });

  it("drives only with team relays and user messages (no synthesized input)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("请开始扫雷工作");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "策略")]);
    planner.settle();
    await tick();
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "对局记录")]);
    player.settle();
    await tick();
    planner.settle();
    await h.orchestrator.whenQuiescent();

    // One user send + one relay per member turn: no plugin-sourced message.
    expect(player.followups.map(messageText)).toEqual(["策略"]);
    expect(planner.followups.map(messageText)).toEqual([
      "请开始扫雷工作",
      "对局记录",
    ]);
    expect(deliveredMessages(h)).toHaveLength(3);
    for (const message of deliveredMessages(h)) {
      expect(["team-broadcast", "user"]).toContain(message.source.kind);
    }
  });
});

describe("TeamOrchestrator cancel and dispose", () => {
  it("cancel terminates the in-flight turn, pauses continuation, and the next send resumes", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first user send drives the planner; cancel lands mid-turn.
    h.orchestrator.submit("首条");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "策略")]);

    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });
    expect(planner.cancels).toHaveLength(1);
    expect(planner.cancels[0]?.cause).toEqual({ kind: "user" });
    await h.orchestrator.whenQuiescent();
    expect(player.followups).toHaveLength(0);
    // Cancel pauses without changing the activation: the next input still
    // belongs to the planner (the in-flight turn's member).
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      active: null,
      activation: "planner",
    });

    // Idempotent while already paused.
    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });

    // A new send lifts the pause and is handled by the current activation.
    const resumed = h.orchestrator.submit("继续");
    expect(resumed.queued).toBe(false);
    expect(planner.followups.map(messageText)).toEqual(["首条", "继续"]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: "planner",
      activation: "planner",
    });
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(1);
    player.settle();
    await h.orchestrator.whenQuiescent();
    expectRelayOrUserSources(h);
  });

  it("cancel voids the queued FIFO so those messages never drive a turn", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // First user send → planner; the planner relay puts the player in flight.
    h.orchestrator.submit("首条");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "策略")]);
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(1);

    expect(h.orchestrator.submit("排队一").queued).toBe(true);
    expect(h.orchestrator.submit("排队二").queued).toBe(true);
    expect(h.orchestrator.snapshot().queued).toBe(2);

    const result = h.orchestrator.cancel();
    expect(result.dropped.map(messageText)).toEqual(["排队一", "排队二"]);
    expect(player.cancels).toHaveLength(1);
    await h.orchestrator.whenQuiescent();
    expect(player.followups).toHaveLength(1);
    // The player turn was in flight: cancel keeps the player as the next
    // input's owner (activation does not fall back to the last finished turn).
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      active: null,
      activation: "player",
    });

    h.orchestrator.submit("新消息");
    await tick();
    expect(player.followups.map(messageText)).toEqual(["策略", "新消息"]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: "player",
      activation: "player",
    });
    player.settle();
    await h.orchestrator.whenQuiescent();
    expectRelayOrUserSources(h);
  });

  it("dispose terminates the in-flight turn, releases members, and is idempotent", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);
    h.orchestrator.submit("首条");
    const playerHandle = h.orchestrator.member("player");
    const plannerHandle = h.orchestrator.member("planner");

    await h.orchestrator.dispose();

    expect(planner.cancels[0]?.cause).toEqual({ kind: "disposed" });
    expect(h.team.registrationDispose).toHaveBeenCalledOnce();
    expect(playerHandle?.dispose).toHaveBeenCalledOnce();
    expect(plannerHandle?.dispose).toHaveBeenCalledOnce();
    expect(h.orchestrator.snapshot().materialized).toBe(false);
    expect(h.orchestrator.member("player")).toBeUndefined();
    expect(h.orchestrator.member("planner")).toBeUndefined();

    await h.orchestrator.dispose();
    expect(playerHandle?.dispose).toHaveBeenCalledOnce();
  });
});

describe("TeamOrchestrator failure reporting", () => {
  it("logs and exposes a failed decision step, then recovers on the next send", async () => {
    const logger = { error: vi.fn() };
    const h = createHarness({ logger });
    await h.orchestrator.materialize(teamOptions());
    const planner = member(h, PLANNER_ID);

    // A fail-loud relay read fails the decision step before any drive.
    h.team.seam.drain.mockImplementationOnce(() => {
      throw new Error("drain read failed");
    });
    h.orchestrator.submit("首条");
    await tick();

    // M1: the failure is logged with its session/phase context and exposed;
    // a non-LlmError step failure reports the UNKNOWN code.
    expect(logger.error).toHaveBeenCalledOnce();
    expect(logger.error.mock.calls[0]?.[0]).toMatch(/orchestration step failed/);
    expect(logger.error.mock.calls[0]?.[1]).toEqual({
      session: SESSION,
      phase: "planning",
      member: null,
      code: "UNKNOWN",
      error: "drain read failed",
    });
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      lastError: {
        message: "drain read failed",
        member: null,
        phase: "planning",
        code: "UNKNOWN",
      },
    });

    // Recovery: the failed send is still queued and the next pump drains it.
    h.orchestrator.submit("再来");
    await tick();
    expect(planner.followups.map(messageText)).toEqual(["首条"]);
    planner.settle();
    await tick();
    expect(planner.followups.map(messageText)).toEqual(["首条", "再来"]);
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      failed: false,
      lastError: null,
    });
  });

  it("retains the planner activation after a failed turn and redrives it on the next send", async () => {
    const logger = { error: vi.fn() };
    const h = createHarness({ logger });
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first send drives the planner; the turn fails at its active
    // boundary with an LlmError-coded agent/error (which precedes idle).
    h.orchestrator.submit("请开始扫雷工作");
    planner.failCurrentTurn("SERVER");
    await tick();

    // specs/063-llm-reliability-opencode-go/spec.md FR-009/FR-012: the
    // activation stays on the planner, the failure is exposed with its
    // stable code, and exactly one structured error line carries the full
    // field set (session/phase/member/code/error).
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "planning",
      active: null,
      activation: "planner",
      paused: true,
      failed: true,
      lastError: {
        message: "injected SERVER failure",
        member: "planner",
        phase: "planning",
        code: "SERVER",
      },
    });
    expect(logger.error).toHaveBeenCalledOnce();
    expect(logger.error.mock.calls[0]?.[0]).toMatch(/orchestration step failed/);
    expect(logger.error.mock.calls[0]?.[1]).toEqual({
      session: SESSION,
      phase: "planning",
      member: "planner",
      code: "SERVER",
      error: "injected SERVER failure",
    });
    expect(player.followups).toHaveLength(0);

    // The next send lifts the pause and re-drives the same member: the
    // planner→player switch did not happen on the failed turn.
    expect(h.orchestrator.submit("再来").queued).toBe(false);
    expect(planner.followups.map(messageText)).toEqual([
      "请开始扫雷工作",
      "再来",
    ]);
    expect(player.followups).toHaveLength(0);

    // The recovered turn succeeds: the existing switch evaluation runs and
    // the player becomes the activation.
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: null,
      activation: "player",
      failed: false,
      lastError: null,
    });
    expectRelayOrUserSources(h);
  });

  it("digests messages queued during a failed turn with the retained member in FIFO order", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first send drives the planner; the second queues behind the turn.
    h.orchestrator.submit("首条");
    expect(h.orchestrator.submit("排队").queued).toBe(true);

    // The turn fails: the pause retains both the activation and the queue.
    planner.failCurrentTurn("TIMEOUT");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      queued: 1,
      activation: "planner",
      lastError: { code: "TIMEOUT", member: "planner" },
    });

    // The next send lifts the pause: the retained member digests the FIFO
    // first ("排队" before "重驱"), so the player is never driven.
    h.orchestrator.submit("重驱");
    expect(planner.followups.map(messageText)).toEqual(["首条", "排队"]);
    planner.settle();
    await tick();
    expect(planner.followups.map(messageText)).toEqual(["首条", "排队", "重驱"]);
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(player.followups).toHaveLength(0);
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: false,
      activation: "player",
    });
    expectRelayOrUserSources(h);
  });

  it("does not retain after a cancel: the aborted turn emits no agent/error", async () => {
    const logger = { error: vi.fn() };
    const h = createHarness({ logger });
    await h.orchestrator.materialize(teamOptions());
    const planner = member(h, PLANNER_ID);

    // Cancel terminates the in-flight planner turn through the abort path,
    // which never emits agent/error
    // (specs/063-llm-reliability-opencode-go/spec.md US2 scenario 6).
    h.orchestrator.submit("首条");
    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });
    await h.orchestrator.whenQuiescent();

    // The existing cancel semantics hold: paused by the cancel, no failure
    // state, no fail-channel log, and the activation is unchanged.
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: false,
      lastError: null,
      active: null,
      activation: "planner",
    });
    expect(planner.followups.map(messageText)).toEqual(["首条"]);
    expect(logger.error).not.toHaveBeenCalled();
  });

  it("reports a failed review drive and retries the terminal record instead of losing it", async () => {
    const logger = { error: vi.fn() };
    const h = createHarness({ logger });
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // Reach the player's terminal turn.
    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "本局对局记录")]);

    // The review drive fails before it starts.
    planner.failNextFollowup();
    player.settle();
    await tick();

    expect(logger.error).toHaveBeenCalledOnce();
    expect(logger.error.mock.calls[0]?.[1]).toEqual({
      session: SESSION,
      phase: "reviewing",
      member: "planner",
      code: "UNKNOWN",
      error: "followup rejected",
    });
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      lastError: {
        message: "followup rejected",
        member: "planner",
        phase: "reviewing",
        code: "UNKNOWN",
      },
    });

    // MINOR1: the terminal record was not consumed. The resumed loop digests
    // the user message, then retries the held review and consumes the record
    // only once the review drive settles.
    h.orchestrator.submit("继续");
    await tick();
    planner.settle();
    await tick();
    expect(planner.followups.map(messageText)).toEqual([
      "请开始",
      "继续",
      "本局对局记录",
    ]);
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      failed: false,
      lastError: null,
      paused: false,
    });

    // The consumed record cannot re-trigger a review on a later player turn.
    h.orchestrator.submit("再来");
    await tick();
    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(planner.followups).toHaveLength(3);
  });
});

describe("TeamOrchestrator materialization rollback", () => {
  it("rolls back the created player when the planner setup fails, then retries cleanly", async () => {
    const members = new Map<string, FakeMember>();
    const team = fakeTeam();
    const loadPlannerMemory = vi.fn<() => Promise<void>>(async () => {
      throw new Error("memory unavailable");
    });
    const compose = vi.fn(async (preset: string): Promise<ComposedPreset> => ({
      agentPreset: `${preset}-resolved`,
      setup: vi.fn(async () => {}),
    }));
    const orchestrator = new TeamOrchestrator(new Context(), {
      compose,
      agents: fakeAgents(members),
      team: team.seam,
      mountPlayerRuntime: (): GameEventSource => ({ peekGameEvent: () => null }),
      loadPlannerMemory,
    });

    await expect(orchestrator.materialize(teamOptions())).rejects.toThrow(
      "memory unavailable",
    );
    const failedPlayer = members.get(PLAYER_ID);
    expect(failedPlayer?.handle.dispose).toHaveBeenCalledOnce();
    expect(team.seam.register).not.toHaveBeenCalled();
    expect(orchestrator.member("player")).toBeUndefined();
    expect(orchestrator.snapshot()).toMatchObject({
      materialized: false,
      phase: null,
      active: null,
    });

    // A retry with the seam repaired materializes from scratch and rests
    // without driving.
    loadPlannerMemory.mockImplementation(async () => {});
    await orchestrator.materialize(teamOptions());
    expect(orchestrator.snapshot()).toMatchObject({
      materialized: true,
      phase: "planning",
      active: null,
      queued: 0,
    });
    expect(orchestrator.member("player")).toBeDefined();
    expect(orchestrator.member("planner")).toBeDefined();
    expect(members.get(PLANNER_ID)?.followups).toHaveLength(0);
  });

  it("rolls back the created player when the planner preset cannot be composed", async () => {
    const members = new Map<string, FakeMember>();
    const team = fakeTeam();
    const compose = vi
      .fn<(preset: string) => Promise<ComposedPreset>>()
      .mockResolvedValueOnce({
        agentPreset: "player-resolved",
        setup: vi.fn(async () => {}),
      })
      .mockRejectedValueOnce(new Error("broken planner preset"));
    const orchestrator = new TeamOrchestrator(new Context(), {
      compose,
      agents: fakeAgents(members),
      team: team.seam,
      mountPlayerRuntime: (): GameEventSource => ({ peekGameEvent: () => null }),
      loadPlannerMemory: async () => {},
    });

    await expect(orchestrator.materialize(teamOptions())).rejects.toThrow(
      "broken planner preset",
    );
    expect(members.get(PLAYER_ID)?.handle.dispose).toHaveBeenCalledOnce();
    expect(team.seam.register).not.toHaveBeenCalled();
    expect(orchestrator.snapshot().materialized).toBe(false);
  });
});
