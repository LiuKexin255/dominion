/**
 * TeamOrchestrator tests: materialization (compose → create → team
 * registration, then resting until the first user send), the
 * alternating-activation state machine (first send → planner; planner
 * quiescence → player; gameEnded → planner review; reviewing → player),
 * queue-first digestion before a switch, cancel / pause / resume,
 * materialization rollback, disposal, failure reporting (logger + snapshot
 * failure state, review retry after a failed drive), the announcer
 * announcement's queue-priority invariance (US2 of
 * specs/065-agent-v2-team-refine/spec.md), the no-synthesized-input rule
 * (every drive carries only team relays and user messages), and the natural
 * round stop (every fifth settled planner review rests the loop instead of
 * auto-driving the player;
 * specs/068-wiki-round-pause/contracts/saolei-loop-round-stop.md §5). The
 * state graph under test is
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
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import type {
  TeamEventStream,
  TeamHandle,
  TeamMemberSource,
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
  MemoryCapabilities,
  MemoryMount,
  PlannerMemoryScope,
  SaoleiToolsUnit,
  TeamMaterializeOptions,
  TeamOrchestratorDeps,
} from "./orchestrator.js";
import { ANNOUNCER_MEMBER_SUMMARY } from "./announcer.js";
import type { GameEventRecord, GameRuntime } from "@dominion/dsh-saolei";
import type { DesktopBridgeService } from "@dominion/dsh-desktop-bridge";

const SESSION = "templates/saolei/sessions/s1";
/** The materialization source preset id half of the memory scope key. */
const PRESET_ID = "planner-p";
const PLAYER_ID = `${SESSION}/player`;
const PLANNER_ID = `${SESSION}/planner`;
const ANNOUNCER_ID = `${SESSION}/announcer`;

/**
 * The exact announcement {@link winner} produces (the gameStatsText template
 * of specs/065-agent-v2-team-refine/data-model.md §3).
 */
const WINNER_STATS_TEXT =
  "本局游戏结束：胜利。\n本局共执行 5 个操作：click 4 次、flag 1 次、chord 0 次。";

/**
 * The exact announcement {@link loser} produces (counts distinct from
 * {@link winner}).
 */
const LOSER_STATS_TEXT =
  "本局游戏结束：失败。\n本局共执行 2 个操作：click 1 次、flag 0 次、chord 1 次。";

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
  senderSessionId: string,
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
  /** Tool definitions registered into this member's scope (by name). */
  readonly tools: Map<string, { name: string }>;
  /** Prompt sections registered into this member's scope. */
  readonly sections: Array<{ name: string; order: number; text: unknown }>;
  /** Agent-scoped faces provided under an isolate label (by service name). */
  readonly faces: Map<string, unknown>;
  /** The member-scope registration spies (unified order assertions). */
  readonly toolsRegister: ReturnType<typeof vi.fn>;
  readonly sectionRegister: ReturnType<typeof vi.fn>;
  readonly provideFace: ReturnType<typeof vi.fn>;
  readonly followups: UserMessage[];
  readonly injections: UserMessage[];
  readonly steers: UserMessage[];
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
 * One agent double: `followup` records + flips running (its message is claimed
 * by the turn, so it never lingers in the inbox), `inject`/`steer` record into
 * the next-step inbox (pending until cancelled — the landed source),
 * `cancel` records + clears the inbox + (like the real loop's converging
 * abort) emits idle, and `settle()` completes a turn. `whenIdle()` resolves on
 * the next idle transition, modelling the official loop's whole-agent
 * quiescence wait.
 */
function createFakeMember(name: string): FakeMember {
  const followups: UserMessage[] = [];
  const injections: UserMessage[] = [];
  const steers: UserMessage[] = [];
  const cancels: Array<{ cause: unknown; options: unknown }> = [];
  const statusListeners: StatusListener[] = [];
  const errorListeners: ErrorListener[] = [];
  const state = { status: "idle" as AgentStatus };
  const events: SessionEvent[] = [];
  const tools = new Map<string, { name: string }>();
  const sections: Array<{ name: string; order: number; text: unknown }> = [];
  const faces = new Map<string, unknown>();
  const nextStep: UserMessage[] = [];
  const nextTurn: UserMessage[] = [];
  const idleWaiters: Array<() => void> = [];
  let failFollowup = false;
  const off = <T>(list: T[], listener: T): (() => void) => () => {
    const index = list.indexOf(listener);
    if (index >= 0) {
      list.splice(index, 1);
    }
  };
  // Member-scope registration seams (specs/074-dsh-grpc-runtime/contracts/
  // team-assembly.md §4): spy identities are kept so the tests can compare
  // `mock.invocationCallOrder` across faces → tools → sections → ready.
  const toolsRegister = vi.fn((definition: { name: string }) => {
    tools.set(definition.name, definition);
    return () => {};
  });
  const sectionRegister = vi.fn(
    (section: { name: string; order: number; text: unknown }) => {
      sections.push(section);
      return () => {};
    },
  );
  const provideFace = vi.fn((service: string, value: unknown) => {
    faces.set(service, value);
    return () => {};
  });
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
    // The member-scope registration surfaces the orchestrator writes to.
    tools: { register: toolsRegister },
    systemPrompt: { section: sectionRegister },
    isolate: vi.fn((_name: string) => ({ provide: provideFace })),
  } as { on: unknown };

  const id = name as Agent["id"];
  const emitIdle = (): void => {
    state.status = "idle";
    for (const listener of [...statusListeners]) {
      listener({ agent, status: "idle" });
    }
    for (const resolve of idleWaiters.splice(0)) {
      resolve();
    }
  };
  const agent = {
    id,
    session: { id, events, snapshotEvents: () => events },
    get status(): AgentStatus {
      return state.status;
    },
    ctx,
    inbox: {
      get nextStep() {
        return nextStep;
      },
      get nextTurn() {
        return nextTurn;
      },
      get hasPending() {
        return nextStep.length > 0 || nextTurn.length > 0;
      },
    },
    inject: vi.fn((message: UserMessage) => {
      injections.push(message);
      nextStep.push(message);
    }),
    steer: vi.fn((message: UserMessage) => {
      steers.push(message);
      nextStep.push(message);
      state.status = "running";
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
      nextStep.length = 0;
      nextTurn.length = 0;
      emitIdle();
    }),
    whenIdle: vi.fn(
      () => new Promise<void>((resolve) => idleWaiters.push(resolve)),
    ),
  } as unknown as Agent;

  return {
    id: name,
    agent,
    ctx: ctx as unknown as Context,
    handle: { agent, dispose: vi.fn(async () => {}) },
    tools,
    sections,
    faces,
    toolsRegister,
    sectionRegister,
    provideFace,
    followups,
    injections,
    steers,
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
      await options.setup?.(member.ctx, member.agent);
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
  /** The stable stream object every registration handle returns (pass-through assertion). */
  readonly stream: TeamEventStream;
}

function fakeTeam(): FakeTeam {
  const queues = new Map<string, UserMessage[]>();
  const registrations: TeamRegistration[] = [];
  const registrationDispose = vi.fn();
  const stream: TeamEventStream = { subscribe: vi.fn(() => () => {}) };
  return {
    queues,
    registrations,
    registrationDispose,
    stream,
    seam: {
      register: vi.fn((registration: TeamRegistration): TeamHandle => {
        registrations.push(registration);
        return { dispose: registrationDispose, events: () => stream };
      }),
      drain: vi.fn((member: TeamMemberSource): UserMessage[] => {
        return queues.get(member.id)?.splice(0) ?? [];
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
  /** The `saolei` foundation face's `createGameRuntime` spy. */
  readonly createGameRuntime: ReturnType<typeof vi.fn>;
  /** The desktop-bridge double the foundation factory receives. */
  readonly desktopBridge: DesktopBridgeService;
  /** The stable runtime instance the fake factory returns. */
  readonly gameRuntime: GameRuntime;
  /** The `saoleiTools` provide face the orchestration reads. */
  readonly saoleiTools: SaoleiToolsUnit;
  /** The `memoryMount` provide face the orchestration reads. */
  readonly memoryMount: MemoryMount;
  /** Every scope `memoryMount.build` was called with. */
  readonly buildScopes: PlannerMemoryScope[];
  /** The unit the memory mount builds (its `ready` is overridable per test). */
  readonly memoryCapabilities: MemoryCapabilities;
  readonly game: { current: GameEventRecord | null; peeks: number };
}

function fakeTool(name: string) {
  return { name } as unknown as SaoleiToolsUnit["tools"][number];
}

function fakeSection(name: string, order: number) {
  return { name, order, text: `section:${name}` };
}

/** A no-op game runtime double (rollback/capability contexts). */
function nullGameRuntime(): GameRuntime {
  return {
    init: async () => ({ isError: false as const, text: "" }),
    operate: async () => ({ isError: false as const, text: "" }),
    remain: () => ({ isError: false as const, text: "" }),
    peekGameEvent: () => null,
  };
}

type Spy = ReturnType<typeof vi.fn>;

/** The first invocation index of a spy (`mock.invocationCallOrder[0]`). */
function firstCall(spy: Spy): number {
  return spy.mock.invocationCallOrder[0] as number;
}

/** The last invocation index of a spy. */
function lastCall(spy: Spy): number {
  return spy.mock.invocationCallOrder[
    spy.mock.calls.length - 1
  ] as number;
}

/**
 * Assert the contract's fixed memory registration order
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §4):
 * faces → tools(×4) → sections(×3) → ready. `sectionsBefore` accounts for the
 * gameplay section the orchestration registers before the memory unit
 * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §3).
 * Cross-seam comparison uses `mock.invocationCallOrder`, so an out-of-order
 * implementation fails here.
 */
function expectMemoryRegistrationOrder(seams: {
  readonly faces: Spy;
  readonly tools: Spy;
  readonly sections: Spy;
  readonly ready: Spy;
  readonly sectionsBefore?: number;
}): void {
  const before = seams.sectionsBefore ?? 0;
  const sectionCallOrder = seams.sections.mock.invocationCallOrder;
  expect(seams.faces).toHaveBeenCalledTimes(1);
  expect(seams.tools).toHaveBeenCalledTimes(4);
  expect(seams.sections).toHaveBeenCalledTimes(before + 3);
  expect(seams.ready).toHaveBeenCalledTimes(1);
  expect(lastCall(seams.faces)).toBeLessThan(firstCall(seams.tools));
  expect(lastCall(seams.tools)).toBeLessThan(sectionCallOrder[before] as number);
  expect(sectionCallOrder[before + 2] as number).toBeLessThan(firstCall(seams.ready));
}

/**
 * Assert the player registration order: tools(×3) → sections(×2:
 * saoleiTools guidance + the foundation gameplay section) → game-handle
 * registration (specs/075-saolei-game-foundation/contracts/
 * team-loop-assembly.md §3).
 */
function expectPlayerRegistrationOrder(seams: {
  readonly tools: Spy;
  readonly sections: Spy;
  readonly handle: Spy;
}): void {
  expect(seams.tools).toHaveBeenCalledTimes(3);
  expect(seams.sections).toHaveBeenCalledTimes(2);
  expect(seams.handle).toHaveBeenCalledTimes(1);
  expect(lastCall(seams.tools)).toBeLessThan(firstCall(seams.sections));
  expect(lastCall(seams.sections)).toBeLessThan(firstCall(seams.handle));
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
  const gameRuntime: GameRuntime = {
    init: vi.fn(async () => ({ isError: false as const, text: "" })),
    operate: vi.fn(async () => ({ isError: false as const, text: "" })),
    remain: vi.fn(() => ({ isError: false as const, text: "" })),
    peekGameEvent: () => {
      game.peeks += 1;
      return game.current;
    },
  };
  // The composition-level game-foundation face (the top-level saolei row
  // provides it; contracts/team-loop-assembly.md §2): the orchestrator builds
  // and owns the runtime through the factory and registers the section.
  const createGameRuntime = vi.fn(
    (_sessionName: string, _desktopBridge: DesktopBridgeService) => gameRuntime,
  );
  const foundation = {
    createGameRuntime,
    gameSection: fakeSection("saolei:game", 50),
  };
  const desktopBridge = { dispatch: vi.fn() } as unknown as DesktopBridgeService;
  // The two capability provide faces the saolei-loop apply would have mounted
  // (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3): the
  // orchestrator reads them off the context when it assembles a member.
  const saoleiTools: SaoleiToolsUnit = {
    tools: [
      fakeTool("saolei_init"),
      fakeTool("saolei_operate"),
      fakeTool("saolei_remain"),
    ],
    sections: [fakeSection("saolei:guidance", 100)],
  };
  const memoryCapabilities: MemoryCapabilities = {
    faces: [{ key: "memory", value: { kind: "memory-face" } }],
    tools: [
      fakeTool("memory"),
      fakeTool("wiki_search"),
      fakeTool("wiki_get"),
      fakeTool("wiki_write"),
    ],
    sections: [
      fakeSection("memory:guidance", 110),
      fakeSection("wiki:guidance", 120),
      fakeSection("memory:snapshot", 200),
    ],
    ready: vi.fn(async () => {}),
  };
  const buildScopes: PlannerMemoryScope[] = [];
  const memoryMount: MemoryMount = {
    build: vi.fn((scope: PlannerMemoryScope): MemoryCapabilities => {
      buildScopes.push(scope);
      return memoryCapabilities;
    }),
  };
  const ctx = new Context();
  ctx.provide("saoleiTools", saoleiTools);
  ctx.provide("memoryMount", memoryMount);
  ctx.provide("saolei", foundation);
  const orchestrator = new TeamOrchestrator(ctx, {
    compose,
    agents,
    team: team.seam,
    desktopBridge,
    ...overrides,
  });
  return {
    orchestrator,
    members,
    agents,
    team,
    compose,
    setups,
    createGameRuntime,
    desktopBridge,
    gameRuntime,
    saoleiTools,
    memoryMount,
    buildScopes,
    memoryCapabilities,
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
    memoryScope: { template: "saolei", preset: PRESET_ID },
    goal: "协作提高扫雷胜率",
    player: { preset: "player-a", model: "model-p" },
    planner: { preset: "planner-a", model: "model-q" },
    ...overrides,
  };
}

function winner(): GameEventRecord {
  return {
    status: "won",
    stats: {
      operationCount: 5,
      operationsByType: { click: 4, flag: 1, chord: 0 },
      correctFlags: 3,
      avgOpsPerMine: 1.67,
    },
    endedAt: 1_000,
  };
}

/**
 * A second terminal record with counts distinct from {@link winner} (the US2
 * swallowed-game scenario).
 */
function loser(): GameEventRecord {
  return {
    status: "lost",
    stats: {
      operationCount: 2,
      operationsByType: { click: 1, flag: 0, chord: 1 },
      correctFlags: 0,
      avgOpsPerMine: "N/A",
    },
    endedAt: 2_000,
  };
}

/**
 * Mirror the team service's live relay for the announce-only announcer member:
 * every announcer production enters the planner's pending queue as the
 * `team-broadcast` user message the real team would render from the member
 * log (the fake team seam drains pre-seeded queues only).
 */
function relayAnnouncementsToPlanner(h: Harness): void {
  const announcer = h.orchestrator.announcer;
  if (announcer === undefined) {
    throw new Error("the announcer member is not materialized");
  }
  announcer.source.subscribe?.((event) => {
    if (event.type !== "assistant/message") {
      return;
    }
    const text = event.data.message.content
      .map((block) => (block.type === "text" ? block.text : ""))
      .join("");
    const queue = h.team.queues.get(PLANNER_ID) ?? [];
    queue.push(relay(ANNOUNCER_ID, "announcer", text));
    h.team.queues.set(PLANNER_ID, queue);
  });
}

/** The texts the announcer member has announced, in order. */
function announcedTexts(h: Harness): string[] {
  return (h.orchestrator.announcer?.source.events ?? []).map((event) => {
    if (event.type !== "assistant/message") {
      return "";
    }
    return event.data.message.content
      .map((block) => (block.type === "text" ? block.text : ""))
      .join("");
  });
}

/**
 * Drive the loop to an in-flight player turn: the first user send starts the
 * planner turn, whose pre-seeded relay starts the player turn. Returns once
 * the player turn's `followup` has landed, so the caller owns the terminal
 * record and the following settle evaluation (the US2 queue scenarios).
 */
async function beginPlayerTurn(
  h: Harness,
): Promise<{ player: FakeMember; planner: FakeMember }> {
  const player = member(h, PLAYER_ID);
  const planner = member(h, PLANNER_ID);
  h.orchestrator.submit("请开始");
  h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
  planner.settle();
  await tick();
  return { player, planner };
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
    // Setup chain: roster mount for both; player registers the saolei static
    // unit (three tools + the guidance section, no memory trace), registers
    // the foundation gameplay section, and wires the orchestrator-owned game
    // runtime as the member's agent-scoped handle (bound to the GAME session
    // resource name — the desktop-bridge key, not the namespaced member
    // session id); planner registers the gameplay section then builds the
    // memory unit with the explicit (template, preset) scope, registering
    // faces → tools → sections and awaiting the fail-loud prefetch.
    expect(h.setups[0]).toHaveBeenCalledWith(player.ctx);
    expect(h.setups[1]).toHaveBeenCalledWith(planner.ctx);
    // The orchestrator built its game runtime through the foundation face
    // exactly once, bound to the GAME session name and the desktop bridge.
    expect(h.createGameRuntime).toHaveBeenCalledTimes(1);
    expect(h.createGameRuntime).toHaveBeenCalledWith(SESSION, h.desktopBridge);
    // Player: three saolei tools and two saolei sections (unit guidance +
    // foundation gameplay), the game handle, no memory face.
    expect([...player.tools.keys()].sort()).toEqual([
      "saolei_init",
      "saolei_operate",
      "saolei_remain",
    ]);
    expect(player.sections.map((section) => section.name)).toEqual([
      "saolei:guidance",
      "saolei:game",
    ]);
    // The handle is the very instance the foundation factory returned.
    expect(player.faces.get("saoleiGame")).toBe(h.gameRuntime);
    expect(player.faces.size).toBe(1);
    // Planner: the explicit scope, the memory face, four tools, the gameplay
    // section plus three memory sections — no saolei tool/guidance/handle.
    expect(h.buildScopes).toEqual([{ template: "saolei", preset: PRESET_ID }]);
    expect(planner.faces.get("memory")).toEqual({ kind: "memory-face" });
    expect(planner.faces.size).toBe(1);
    expect(planner.faces.has("saoleiGame")).toBe(false);
    expect([...planner.tools.keys()].sort()).toEqual([
      "memory",
      "wiki_get",
      "wiki_search",
      "wiki_write",
    ]);
    expect(planner.sections.map((section) => section.name)).toEqual([
      "saolei:game",
      "memory:guidance",
      "wiki:guidance",
      "memory:snapshot",
    ]);
    expect(h.memoryCapabilities.ready).toHaveBeenCalledTimes(1);
    // The fixed cross-face order: player tools → sections (guidance then
    // gameplay) → handle; planner gameplay section → faces → tools → sections
    // → ready (specs/075-saolei-game-foundation/contracts/
    // team-loop-assembly.md §3; specs/074-dsh-grpc-runtime/contracts/
    // team-assembly.md §4).
    expectPlayerRegistrationOrder({
      tools: player.toolsRegister,
      sections: player.sectionRegister,
      handle: player.provideFace,
    });
    expectMemoryRegistrationOrder({
      faces: planner.provideFace,
      tools: planner.toolsRegister,
      sections: planner.sectionRegister,
      ready: h.memoryCapabilities.ready as Spy,
      sectionsBefore: 1,
    });
    // Team registration: goal + the two-member roster with defaults.
    expect(h.team.registrations).toHaveLength(1);
    const registration = h.team.registrations[0] as TeamRegistration;
    expect(registration.goal).toBe("协作提高扫雷胜率");
    expect(
      registration.members.map((registered) => [
        registered.role,
        registered.source.id,
        registered.summary,
      ]),
    ).toEqual([
      ["player", PLAYER_ID, DEFAULT_MEMBER_SUMMARIES.player],
      ["planner", PLANNER_ID, DEFAULT_MEMBER_SUMMARIES.planner],
      ["announcer", ANNOUNCER_ID, ANNOUNCER_MEMBER_SUMMARY],
    ]);
    // The third registration is the announce-only announcer member: no section
    // target, no broadcast consumption, and its log starts empty.
    const announcer = registration.members[2];
    expect(announcer?.source.consumes).toBe(false);
    expect(announcer?.source.sectionTarget).toBeUndefined();
    expect(announcer?.source.events).toEqual([]);
    // The orchestrator's announcer accessor exposes the same member.
    expect(h.orchestrator.announcer?.source.id).toBe(ANNOUNCER_ID);

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

describe("TeamOrchestrator.teamEvents", () => {
  it("passes the team registration's stream through unchanged and keeps the state-error semantics", async () => {
    const h = createHarness();

    const before = (() => {
      try {
        h.orchestrator.teamEvents();
        return undefined;
      } catch (err) {
        return err;
      }
    })();
    expect(before).toBeInstanceOf(OrchestratorStateError);
    expect((before as OrchestratorStateError).code).toBe("UNMATERIALIZED");

    await h.orchestrator.materialize(teamOptions());
    // Zero-shape pass-through: the read face returns the team plugin's own
    // stream object, not a scenario-shaped copy
    // (specs/077-announcer-member-unification/contracts/member-event-stream.md
    // §4).
    expect(h.orchestrator.teamEvents()).toBe(h.team.stream);

    await h.orchestrator.dispose();
    const after = (() => {
      try {
        h.orchestrator.teamEvents();
        return undefined;
      } catch (err) {
        return err;
      }
    })();
    expect(after).toBeInstanceOf(OrchestratorStateError);
    expect((after as OrchestratorStateError).code).toBe("DISPOSED");
  });
});

describe("TeamOrchestrator member delivery face (specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md §2.1)", () => {
  /** The registered member entry of one role. */
  function registeredMember(h: Harness, role: string) {
    const registration = h.team.registrations[0] as TeamRegistration;
    const found = registration.members.find((member) => member.role === role);
    if (found === undefined) {
      throw new Error(`the "${role}" member is not registered`);
    }
    return found;
  }

  it("supplies the face on the agent members' sources and not on the announcer's", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());

    // The agent member kind supplies the face structurally …
    expect(typeof registeredMember(h, "player").source.deliverInFlight).toBe(
      "function",
    );
    expect(typeof registeredMember(h, "planner").source.deliverInFlight).toBe(
      "function",
    );
    // … and the announce-only registration carries none (structural absence,
    // not a disabled registration option).
    expect(
      registeredMember(h, "announcer").source.deliverInFlight,
    ).toBeUndefined();
  });

  it("delegates to the adapter's policy: quiescent no-op, in-flight non-waking inject", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const deliver = registeredMember(h, "planner").source.deliverInFlight;
    if (deliver === undefined) {
      throw new Error("the planner registration must carry the delivery face");
    }
    const planner = member(h, PLANNER_ID);

    // Quiescent: "not a receiver" — no inject, no turn, no bookkeeping.
    deliver(relay(ANNOUNCER_ID, "announcer", "本局游戏结束：胜利。"));
    expect(planner.injections).toEqual([]);
    expect(planner.followups).toEqual([]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "planning",
      active: null,
      queued: 0,
    });

    // In flight: the broadcast joins the next-step inbox without waking the
    // driver — the drive's own turn stays the only turn and the fast-path
    // message never enters the queued/steered accounting.
    h.orchestrator.submit("请开始");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: "planner",
      queued: 0,
    });
    const followupsBefore = planner.followups.length;
    deliver(relay(ANNOUNCER_ID, "announcer", "hook 播报"));
    expect(planner.injections.map(messageText)).toEqual(["hook 播报"]);
    expect(planner.followups).toHaveLength(followupsBefore);
    expect(planner.steers).toEqual([]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: "planner",
      queued: 0,
    });

    planner.settle();
    await h.orchestrator.whenQuiescent();
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
    relayAnnouncementsToPlanner(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // First user send → planner; the planner turn relays the strategy → player.
    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(1);

    // The player's turn ends with a terminal record: review the planner. The
    // announcement is the turn's last input (followup), after the
    // injected player relay.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "本局对局记录")]);
    player.settle();
    await tick();
    expect(planner.injections.map(messageText)).toEqual(["本局对局记录"]);
    expect(planner.followups.map(messageText)).toEqual(["请开始", WINNER_STATS_TEXT]);
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
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

    // A later player turn still observing the same record must not re-review
    // and must not re-announce (record-identity guards).
    h.orchestrator.submit("继续");
    await tick();
    expect(player.followups).toHaveLength(2);
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: "player",
      activation: "player",
    });
    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(planner.injections).toHaveLength(1);
    expect(planner.followups).toHaveLength(2);
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
    expectRelayOrUserSources(h);
  });

  it("digests a queued user message with the current member before switching", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first user send drives the planner; a second one steers into the
    // in-flight turn (061 direction — mid-turn delivery, not an orchestration
    // FIFO).
    h.orchestrator.submit("首条");
    const submitted = h.orchestrator.submit("先消化这条");
    expect(submitted.queued).toBe(true);
    expect(submitted.position).toBe(1);
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "策略")]);

    planner.settle();
    await tick();
    // The steered message was consumed inside the planner turn; the planner
    // turn's settlement advances the flow to the player.
    expect(planner.followups.map(messageText)).toEqual(["首条"]);
    expect(planner.steers.map(messageText)).toEqual(["先消化这条"]);
    expect(planner.steers[0]?.source).toMatchObject({ kind: "user" });
    expect(player.followups).toHaveLength(1);

    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: null,
      activation: "player",
    });
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

describe("TeamOrchestrator game-stats announcement (specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3)", () => {
  it("puts the announcement last in the review input set", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();

    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "本局对局记录")]);
    player.settle();
    await tick();

    // The player's terminal record was drained first (the turn's injected
    // input); the announcement rides at the tail (the followup input — its
    // producing event is the newest).
    expect(planner.injections.map(messageText)).toEqual(["本局对局记录"]);
    const followups = planner.followups.map(messageText);
    expect(followups).toEqual(["请开始", WINNER_STATS_TEXT]);
    expect(followups.at(-1)).toBe(WINNER_STATS_TEXT);
    expect(h.orchestrator.snapshot()).toMatchObject({ phase: "reviewing", active: "planner" });
  });

  it("does not re-announce the same terminal record", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();
    h.game.current = winner();
    player.settle();
    await tick();
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);

    // The review settles, then a later player turn observes the same record:
    // the identity guard skips a second announcement.
    planner.settle();
    await h.orchestrator.whenQuiescent();
    h.orchestrator.submit("继续");
    await tick();
    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
    expect(planner.followups.map(messageText)).toEqual(["请开始", WINNER_STATS_TEXT]);
  });

  it("does not re-announce the same record when the review never formed (statsSentFor guard)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    // No relayAnnouncementsToPlanner: the announcement cannot form the review
    // (the planner drain stays empty), so the record keeps `reviewed` null and
    // only the announce member's record-identity guard protects it.
    const { player, planner } = await beginPlayerTurn(h);

    h.game.current = winner();
    player.settle();
    await tick();

    // The play activity settled: A was announced, then the review activity's
    // planner drive found no input (resting) — the flow rests at review with
    // A still unreviewed.
    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);
    expect(planner.followups.map(messageText)).toEqual(["请开始"]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      active: null,
      activation: "planner",
    });

    // Resume: the planner delivers, then review → play returns the flow to the
    // player; the player's next settlement re-evaluates play → announce with
    // the same record, and the guard skips the second announcement.
    h.orchestrator.submit("继续");
    await tick();
    expect(planner.followups.map(messageText)).toEqual(["请开始", "继续"]);
    planner.settle();
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: null,
      activation: "player",
    });

    h.orchestrator.submit("再来");
    await tick();
    expect(player.followups.map(messageText)).toEqual(["开局策略", "再来"]);
    player.settle();
    await tick();

    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
  });

  it("starts the review with the announcement alone when no relay was queued", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();

    // No planner relay at all: the announcement alone makes the review input
    // non-empty, so the review-start branch is guaranteed to fire.
    h.game.current = winner();
    player.settle();
    await tick();
    expect(planner.followups.map(messageText)).toEqual(["请开始", WINNER_STATS_TEXT]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      active: "planner",
      activation: "planner",
    });
  });

  it("does not announce for an init-only game (no terminal record)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();

    // The player turn quiesces without a terminal record: no announcement,
    // no review.
    expect(h.game.current).toBeNull();
    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.announcer?.source.events).toEqual([]);
    expect(planner.followups.map(messageText)).toEqual(["请开始"]);
  });

  it("does not announce after dispose (the pending record is voided)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const announcer = h.orchestrator.announcer;
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(1);

    // Dispose terminates the in-flight player turn; the terminal record it
    // would have announced with is voided with the materialization.
    h.game.current = winner();
    await h.orchestrator.dispose();
    expect(h.orchestrator.announcer).toBeUndefined();
    expect(announcer?.source.events).toEqual([]);
    expect(planner.followups.map(messageText)).toEqual(["请开始"]);
  });
});

describe("TeamOrchestrator mid-turn delivery and the terminal handoff (specs/061-team-queue-steer/spec.md; specs/076-agent-v2-optimize/spec.md FR-105/FR-113)", () => {
  it("consumes a mid-turn message inside the player turn, then hands off to the announcement", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const { player, planner } = await beginPlayerTurn(h);

    // The game ends while the player turn is in flight; a user message steers
    // into that turn (no orchestration FIFO).
    h.game.current = winner();
    expect(h.orchestrator.submit("在途消息").queued).toBe(true);

    player.settle();
    await tick();

    // The steered message was consumed inside the player turn; the terminal
    // turn's handoff (announce + review) then runs — the mid-turn consumption
    // did not wait for a separate digestion turn.
    expect(player.followups.map(messageText)).toEqual(["开局策略"]);
    expect(player.steers.map(messageText)).toEqual(["在途消息"]);
    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);
    expect(planner.followups.map(messageText)).toEqual(["请开始", WINNER_STATS_TEXT]);
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
  });

  it("announces only the latest terminal record at handoff time (swallowed game not re-announced)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const { player, planner } = await beginPlayerTurn(h);

    // Terminal record A exists when the user message steers in; the in-flight
    // turn opens a new game whose terminal record B overwrites A in the
    // runtime's single record slot.
    h.game.current = winner();
    expect(h.orchestrator.submit("在途消息").queued).toBe(true);
    h.game.current = loser();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "新局终局记录")]);
    expect(announcedTexts(h)).toEqual([]);

    player.settle();
    await tick();

    // Only B is announced — A's guard is never satisfiable again — and B's
    // announcement rides at the tail of the new review input set.
    expect(announcedTexts(h)).toEqual([LOSER_STATS_TEXT]);
    expect(planner.injections.map(messageText)).toEqual(["新局终局记录"]);
    expect(planner.followups.map(messageText)).toEqual(["请开始", LOSER_STATS_TEXT]);
    expect(planner.followups.map(messageText)).not.toContain(WINNER_STATS_TEXT);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      active: "planner",
      activation: "planner",
    });
  });

  it("does not re-announce when a failed review retries its held input set", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const { player, planner } = await beginPlayerTurn(h);

    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "本局对局记录")]);
    player.settle();
    await tick();

    // The review drive started with A's announcement; the planner turn fails
    // at its active boundary, so the review transition stays held.
    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);
    planner.failCurrentTurn("SERVER");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      lastError: { code: "TRANSIENT", member: "planner", origin: "member-turn" },
    });

    // Resume: the delivered message is handled first, then the flow restarts
    // the review activity, whose member adapter replays the held input set —
    // the announcement is not sent again; the retry re-delivers the held
    // messages (the same objects).
    h.orchestrator.submit("继续");
    await tick();
    planner.settle();
    await tick();
    expect(planner.injections.map(messageText)).toEqual([
      "本局对局记录",
      "本局对局记录",
    ]);
    expect(planner.followups.map(messageText)).toEqual([
      "请开始",
      WINNER_STATS_TEXT,
      "继续",
      WINNER_STATS_TEXT,
    ]);
    const statsReplays = planner.followups.filter(
      (message) => messageText(message) === WINNER_STATS_TEXT,
    );
    expect(statsReplays).toHaveLength(2);
    // The two deliveries are the same held message object (pendingReview),
    // not two announcements.
    expect(statsReplays[0]).toBe(statsReplays[1]);
    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);

    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);
  });

  it("announces after cancel/resume when the terminal record survived the race", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    relayAnnouncementsToPlanner(h);
    const { player, planner } = await beginPlayerTurn(h);

    // The terminal record lands while the player turn is in flight; cancel
    // pauses the loop before the handoff evaluation reaches case 4.
    h.game.current = winner();
    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      active: null,
      activation: "player",
    });
    expect(announcedTexts(h)).toEqual([]);

    // Send resumes: the player digests the new message first; the untouched
    // record is announced with the handoff that follows.
    h.orchestrator.submit("继续");
    await tick();
    expect(player.followups.map(messageText)).toEqual(["开局策略", "继续"]);
    expect(announcedTexts(h)).toEqual([]);

    player.settle();
    await tick();
    // The delivery-started turn settled, then the recovery chain restarted
    // the play activity and the member adapter replayed its held input set.
    expect(announcedTexts(h)).toEqual([]);
    expect(player.followups.map(messageText)).toEqual([
      "开局策略",
      "继续",
      "开局策略",
    ]);

    player.settle();
    await tick();
    expect(announcedTexts(h)).toEqual([WINNER_STATS_TEXT]);
    expect(planner.followups.map(messageText)).toEqual(["请开始", WINNER_STATS_TEXT]);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      active: "planner",
      activation: "planner",
    });
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

    // The delivery turn settled, then the recovery chain restarted the play
    // activity with the held input set (the aborted drive's relays).
    player.settle();
    await tick();
    expect(player.followups.map(messageText)).toEqual(["策略", "新消息", "策略"]);

    player.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      active: null,
      activation: "player",
    });
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

    // A fail-loud relay read fails the delivery assembly before any drive.
    h.team.seam.drain.mockImplementationOnce(() => {
      throw new Error("drain read failed");
    });
    h.orchestrator.submit("首条");
    await tick();

    // The delivery assembly failure joins the freeze channel: it is logged
    // with its session/phase context and exposed with the INTERNAL code and
    // the orchestration origin (specs/076-agent-v2-optimize/contracts/
    // flow-orchestration.md §5).
    expect(logger.error).toHaveBeenCalledOnce();
    expect(logger.error.mock.calls[0]?.[0]).toMatch(/orchestration step failed/);
    expect(logger.error.mock.calls[0]?.[1]).toEqual({
      session: SESSION,
      phase: "planning",
      member: null,
      code: "INTERNAL",
      error: "drain read failed",
    });
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      lastError: {
        message: "drain read failed",
        member: null,
        phase: "planning",
        code: "INTERNAL",
        origin: "orchestration",
      },
    });

    // Recovery: the failed assembly consumed nothing, so the next send opens
    // the planner turn; its settlement re-judges the edge (plan → play).
    h.orchestrator.submit("再来");
    expect(planner.followups.map(messageText)).toEqual(["再来"]);
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      failed: false,
      lastError: null,
      activation: "player",
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
        code: "TRANSIENT",
        origin: "member-turn",
      },
    });
    expect(logger.error).toHaveBeenCalledOnce();
    expect(logger.error.mock.calls[0]?.[0]).toMatch(/orchestration step failed/);
    expect(logger.error.mock.calls[0]?.[1]).toEqual({
      session: SESSION,
      phase: "planning",
      member: "planner",
      code: "TRANSIENT",
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

  it("keeps a mid-turn message in the inbox across a failed turn and claims it in the next turn", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // The first send drives the planner; the second steers into the turn.
    h.orchestrator.submit("首条");
    expect(h.orchestrator.submit("排队").queued).toBe(true);

    // The turn fails: the freeze retains the activation; the steered message
    // was not lost (it stays pending in the member inbox).
    planner.failCurrentTurn("TIMEOUT");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      activation: "planner",
      lastError: { code: "TRANSIENT", member: "planner", origin: "member-turn" },
    });

    // The next send opens the retained member's turn; the settlement re-judges
    // the edge and the player becomes the activation.
    h.orchestrator.submit("重驱");
    expect(planner.followups.map(messageText)).toEqual(["首条", "重驱"]);
    expect(planner.steers.map(messageText)).toEqual(["排队"]);
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
    relayAnnouncementsToPlanner(h);
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    // Reach the player's terminal turn.
    h.orchestrator.submit("请开始");
    h.team.queues.set(PLAYER_ID, [relay(planner.agent.id, "planner", "开局策略")]);
    planner.settle();
    await tick();
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [relay(player.agent.id, "player", "本局对局记录")]);

    // The review drive fails before it starts; the announcement was already
    // sent and its unit is held with the rest of the review input set.
    planner.failNextFollowup();
    player.settle();
    await tick();
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);

    expect(logger.error).toHaveBeenCalledOnce();
    expect(logger.error.mock.calls[0]?.[1]).toEqual({
      session: SESSION,
      phase: "reviewing",
      member: "planner",
      code: "INTERNAL",
      error: "followup rejected",
    });
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      lastError: {
        message: "followup rejected",
        member: "planner",
        phase: "reviewing",
        code: "INTERNAL",
        origin: "orchestration",
      },
    });

    // The terminal record was not consumed. The resumed loop handles the
    // delivered message, then the flow restarts the review activity, whose
    // adapter replays the held input set and consumes the record only once
    // the review drive settles.
    h.orchestrator.submit("继续");
    await tick();
    planner.settle();
    await tick();
    // The failed attempt injected the player relay once; the retry re-injects
    // the held input set and the announcement is the followup again.
    expect(planner.injections.map(messageText)).toEqual([
      "本局对局记录",
      "本局对局记录",
    ]);
    expect(planner.followups.map(messageText)).toEqual([
      "请开始",
      "继续",
      WINNER_STATS_TEXT,
    ]);
    // The retried review input carries the one announcement — no second send.
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
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
    expect(planner.injections).toHaveLength(2);
    expect(h.orchestrator.announcer?.source.events).toHaveLength(1);
  });
});

/** A capability context: the game-foundation face plus the two provide faces
 * with an overridable memory ready. */
function capabilityContext(options: {
  readonly ready?: () => Promise<void>;
  readonly omitSaoleiTools?: boolean;
  readonly omitMemoryMount?: boolean;
  readonly createGameRuntime?: () => GameRuntime;
} = {}): Context {
  const ctx = new Context();
  ctx.provide("desktopBridge", { dispatch: () => {} });
  ctx.provide("saolei", {
    createGameRuntime: options.createGameRuntime ?? (() => nullGameRuntime()),
    gameSection: { name: "saolei:game", order: 50, text: "" },
  });
  if (options.omitSaoleiTools !== true) {
    ctx.provide("saoleiTools", { tools: [], sections: [] });
  }
  if (options.omitMemoryMount !== true) {
    ctx.provide("memoryMount", {
      build: () => ({
        faces: [],
        tools: [],
        sections: [],
        ready: options.ready ?? (async () => {}),
      }),
    });
  }
  return ctx;
}

describe("TeamOrchestrator materialization rollback", () => {
  it(
    "rolls back the created player when the planner prefetch rejects, " +
      "then retries cleanly",
    async () => {
    const members = new Map<string, FakeMember>();
    const team = fakeTeam();
    const ready = vi.fn(async () => {
      throw new Error("memory unavailable");
    });
    const compose = vi.fn(async (preset: string): Promise<ComposedPreset> => ({
      agentPreset: `${preset}-resolved`,
      setup: vi.fn(async () => {}),
    }));
    const createGameRuntime = vi.fn(() => nullGameRuntime());
    const orchestrator = new TeamOrchestrator(
      capabilityContext({ ready, createGameRuntime }),
      {
        compose,
        agents: fakeAgents(members),
        team: team.seam,
      },
    );

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
    // The failed attempt had already built a game runtime; the rollback drops
    // it (contracts/team-loop-assembly.md §2/§6).
    expect(createGameRuntime).toHaveBeenCalledTimes(1);

    // A retry with the prefetch repaired materializes from scratch — a fresh
    // runtime is built through the foundation factory — and rests without
    // driving.
    ready.mockImplementation(async () => {});
    await orchestrator.materialize(teamOptions());
    expect(createGameRuntime).toHaveBeenCalledTimes(2);
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
    const orchestrator = new TeamOrchestrator(capabilityContext(), {
      compose,
      agents: fakeAgents(members),
      team: team.seam,
    });

    await expect(orchestrator.materialize(teamOptions())).rejects.toThrow(
      "broken planner preset",
    );
    expect(members.get(PLAYER_ID)?.handle.dispose).toHaveBeenCalledOnce();
    expect(team.seam.register).not.toHaveBeenCalled();
    expect(orchestrator.snapshot().materialized).toBe(false);
  });

  it(
    "fails loud and rolls back when the memoryMount capability is not " +
      "mounted",
    async () => {
    const members = new Map<string, FakeMember>();
    const team = fakeTeam();
    const orchestrator = new TeamOrchestrator(
      capabilityContext({ omitMemoryMount: true }),
      {
        compose: vi.fn(async (preset: string): Promise<ComposedPreset> => ({
          agentPreset: `${preset}-resolved`,
          setup: vi.fn(async () => {}),
        })),
        agents: fakeAgents(members),
        team: team.seam,
      },
    );

    await expect(orchestrator.materialize(teamOptions())).rejects.toThrow(
      /"memoryMount" capability is not mounted/,
    );
    expect(members.get(PLAYER_ID)?.handle.dispose).toHaveBeenCalledOnce();
    expect(team.seam.register).not.toHaveBeenCalled();
    expect(orchestrator.snapshot().materialized).toBe(false);
  });

  it("fails loud when the saoleiTools capability is not mounted", async () => {
    const members = new Map<string, FakeMember>();
    const orchestrator = new TeamOrchestrator(
      capabilityContext({ omitSaoleiTools: true }),
      {
        compose: vi.fn(async (preset: string): Promise<ComposedPreset> => ({
          agentPreset: `${preset}-resolved`,
          setup: vi.fn(async () => {}),
        })),
        agents: fakeAgents(members),
        team: fakeTeam().seam,
      },
    );

    await expect(orchestrator.materialize(teamOptions())).rejects.toThrow(
      /"saoleiTools" capability is not mounted/,
    );
    expect(orchestrator.snapshot().materialized).toBe(false);
  });

  it("fails loud when the game foundation face is not composed", async () => {
    const ctx = new Context();
    ctx.provide("saoleiTools", { tools: [], sections: [] });
    ctx.provide("memoryMount", {
      build: () => ({
        faces: [],
        tools: [],
        sections: [],
        ready: async () => {},
      }),
    });
    const orchestrator = new TeamOrchestrator(ctx, {
      compose: vi.fn(async (preset: string): Promise<ComposedPreset> => ({
        agentPreset: `${preset}-resolved`,
        setup: vi.fn(async () => {}),
      })),
      agents: fakeAgents(new Map()),
      team: fakeTeam().seam,
    });

    await expect(orchestrator.materialize(teamOptions())).rejects.toThrow(
      /"saolei" game foundation is not composed/,
    );
    expect(orchestrator.snapshot().materialized).toBe(false);
  });
});

describe("TeamOrchestrator registration-order assertion", () => {
  it("detects a scrambled order (sensitivity guard)", () => {
    // The guard: the assertion helper must reject a timeline that violates
    // faces → tools → sections → ready, so the materialize assertions cannot
    // pass vacuously (specs/074-dsh-grpc-runtime/contracts/team-assembly.md
    // §4).
    const faces = vi.fn();
    const tools = vi.fn();
    const sections = vi.fn();
    const ready = vi.fn();
    tools();
    tools();
    tools();
    tools();
    faces(); // faces AFTER tools → out of order
    sections();
    sections();
    sections();
    ready();

    expect(() =>
      expectMemoryRegistrationOrder({ faces, tools, sections, ready }),
    ).toThrow();
  });

  it("accepts the contract order", () => {
    const faces = vi.fn();
    const tools = vi.fn();
    const sections = vi.fn();
    const ready = vi.fn();
    faces();
    for (let i = 0; i < 4; i += 1) tools();
    for (let i = 0; i < 3; i += 1) sections();
    ready();

    expect(() =>
      expectMemoryRegistrationOrder({ faces, tools, sections, ready }),
    ).not.toThrow();
  });
});

/** The player's terminal-report relay text (the review turn's input). */
const PLAYER_RECORD_RELAY = "本局对局记录";

/** The planner's review-broadcast relay text (the next player turn's input). */
const ROUND_REVIEW_RELAY = "复盘结论";

/**
 * Play one completed round from an in-flight player turn: the player's game
 * reaches a fresh terminal record (the review input relay is seeded) and the
 * settled planner review completes the round. The review broadcast relay is
 * seeded before the review settles, so the post-review evaluation starts the
 * next player turn — the loop's structural continuation.
 */
async function completeRound(
  h: Harness,
  player: FakeMember,
  planner: FakeMember,
): Promise<void> {
  h.game.current = winner();
  h.team.queues.set(PLANNER_ID, [
    relay(player.agent.id, "player", PLAYER_RECORD_RELAY),
  ]);
  player.settle();
  await tick();
  h.team.queues.set(PLAYER_ID, [
    relay(planner.agent.id, "planner", ROUND_REVIEW_RELAY),
  ]);
  planner.settle();
  await tick();
}

/**
 * Complete four consecutive rounds after the first player turn: each settled
 * review auto-drives the next player turn, so the fifth round's player turn
 * is left in flight (below the {@link ROUND_STOP_INTERVAL} boundary).
 */
async function playFourRounds(h: Harness): Promise<{
  player: FakeMember;
  planner: FakeMember;
}> {
  const { player, planner } = await beginPlayerTurn(h);
  for (let round = 0; round < 4; round += 1) {
    await completeRound(h, player, planner);
  }
  return { player, planner };
}

/** The natural stop's full snapshot (no failure state, activation on player). */
const STOPPED_SNAPSHOT = {
  materialized: true,
  phase: "playing",
  active: null,
  activation: "player",
  paused: false,
  queued: 0,
  failed: false,
  lastError: null,
};

describe("TeamOrchestrator natural round stop (specs/068-wiki-round-pause/contracts/saolei-loop-round-stop.md §5)", () => {
  it("auto-drives the player after rounds 1-4 with the review relays (case 1)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);

    await beginPlayerTurn(h);

    for (let round = 1; round <= 4; round += 1) {
      const drives = player.followups.length;
      await completeRound(h, player, planner);
      // Below the interval the victory switch structurally continues: the
      // player is driven with the review broadcast relay.
      expect(h.orchestrator.snapshot()).toMatchObject({
        phase: "playing",
        active: "player",
        activation: "player",
        paused: false,
        failed: false,
        lastError: null,
      });
      expect(player.followups.length).toBeGreaterThan(drives);
      expect(player.followups.map(messageText).at(-1)).toBe(ROUND_REVIEW_RELAY);
    }
    expectRelayOrUserSources(h);
  });

  it("rests at the fifth settled review: no player drive, activation player, no failure (case 2)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);

    const drives = player.followups.length;
    await completeRound(h, player, planner);

    // The spent budget switched the activation but produced no drive; the
    // stop is neither a failure nor a cancel.
    expect(player.followups).toHaveLength(drives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });

  it("digests a user message queued during the fifth review before stopping (case 3)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);

    // The fifth game ends; its review turn starts.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [
      relay(player.agent.id, "player", PLAYER_RECORD_RELAY),
    ]);
    player.settle();
    await tick();
    expect(planner.followups.map(messageText).at(-1)).toBe(PLAYER_RECORD_RELAY);

    // A user message steers into the in-flight review turn (mid-turn).
    expect(h.orchestrator.submit("排队消息").queued).toBe(true);
    const playerDrives = player.followups.length;
    planner.settle();
    await tick();

    // The review settled: its onSettle counts the fifth round and the edge
    // sends the flow to rest (no player drive). The steered message was
    // consumed inside the review turn.
    expect(planner.steers.map(messageText)).toEqual(["排队消息"]);
    expect(player.followups).toHaveLength(playerDrives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });

  it("drives the player with the unconsumed review broadcast on the next submit (case 4)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);
    await completeRound(h, player, planner);

    // The stop left the review broadcast queued; the next user message
    // delivers it together with the user message (relay injected, user
    // message last) and drives the player.
    expect(h.orchestrator.submit("继续").queued).toBe(false);
    await tick();
    expect(player.injections.map(messageText).at(-1)).toBe(ROUND_REVIEW_RELAY);
    expect(player.followups.map(messageText).at(-1)).toBe("继续");
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: "player",
      activation: "player",
      paused: false,
      failed: false,
      lastError: null,
    });
  });

  it("does not count a restarted game without a terminal record (case 5)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);

    // The fifth game is restarted mid-way: no terminal record, so no review
    // (and neither an increment nor a reset).
    const plannerDrives = planner.followups.length;
    h.game.current = null;
    player.settle();
    await tick();
    expect(planner.followups).toHaveLength(plannerDrives);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: null,
      activation: "player",
    });

    // The restart's real terminal record completes the fifth round and stops:
    // the retained four counts prove the restart did not reset the counter.
    h.orchestrator.submit("继续");
    await tick();
    const drives = player.followups.length;
    await completeRound(h, player, planner);
    expect(player.followups).toHaveLength(drives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });

  it("does not count a failed review; the retried review counts at the interval boundary (case 6)", async () => {
    const h = createHarness({ logger: { error: vi.fn() } });
    await h.orchestrator.materialize(teamOptions());
    const player = member(h, PLAYER_ID);
    const planner = member(h, PLANNER_ID);
    await beginPlayerTurn(h);
    for (let round = 0; round < 3; round += 1) {
      await completeRound(h, player, planner);
    }

    // Round 4's review drive fails at the active boundary: the round is not
    // counted and the pump pauses.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [
      relay(player.agent.id, "player", PLAYER_RECORD_RELAY),
    ]);
    player.settle();
    await tick();
    const drives = player.followups.length;
    planner.failCurrentTurn("SERVER");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      activation: "planner",
      lastError: { code: "TRANSIENT", member: "planner", origin: "member-turn" },
    });
    expect(player.followups).toHaveLength(drives);

    // Resume: the queued message is digested, then the held review retries and
    // settles. The retry counts round 4 — below the interval — so the player
    // is auto-driven into round 5 (a counted failure would already have
    // stopped here).
    h.orchestrator.submit("继续");
    await tick();
    expect(planner.followups.map(messageText).at(-1)).toBe("继续");
    planner.settle();
    await tick();
    h.team.queues.set(PLAYER_ID, [
      relay(planner.agent.id, "planner", ROUND_REVIEW_RELAY),
    ]);
    planner.settle();
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: "player",
      activation: "player",
      paused: false,
      failed: false,
      lastError: null,
    });

    // Round 5 completes and stops: only the retried count (4) plus this round
    // reaches the interval.
    const round5Drives = player.followups.length;
    await completeRound(h, player, planner);
    expect(player.followups).toHaveLength(round5Drives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });

  it("retries the fifth round's failed review and stops on success (case 6, spec US4 scenario 7)", async () => {
    const h = createHarness({ logger: { error: vi.fn() } });
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);

    // The fifth game's review drive fails before it settles.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [
      relay(player.agent.id, "player", PLAYER_RECORD_RELAY),
    ]);
    player.settle();
    await tick();
    const drives = player.followups.length;
    planner.failCurrentTurn("SERVER");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: true,
      lastError: { code: "TRANSIENT", member: "planner", origin: "member-turn" },
    });
    expect(player.followups).toHaveLength(drives);

    // Resume: the queue is digested, the held review retries and settles, and
    // the spent budget reaches the stop.
    h.orchestrator.submit("继续");
    await tick();
    planner.settle();
    await tick();
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(drives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });

  it("recounts from 1 after the stop: no stop until the tenth round (case 7)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);
    await completeRound(h, player, planner);

    // Resume: the user message drives the player into the first round after
    // the stop (round 6).
    h.orchestrator.submit("继续");
    await tick();

    for (let round = 6; round <= 9; round += 1) {
      await completeRound(h, player, planner);
      expect(h.orchestrator.snapshot()).toMatchObject({
        phase: "playing",
        active: "player",
        activation: "player",
      });
    }

    const drives = player.followups.length;
    await completeRound(h, player, planner);
    expect(player.followups).toHaveLength(drives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });

  it("keeps the player relays unconsumed at the stop (the next drive retrieves them) (case 8)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);

    // The fifth game ends and its review starts; the review broadcast is
    // seeded for the player.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [
      relay(player.agent.id, "player", PLAYER_RECORD_RELAY),
    ]);
    player.settle();
    await tick();
    const reviewBroadcast = relay(planner.agent.id, "planner", ROUND_REVIEW_RELAY);
    h.team.queues.set(PLAYER_ID, [reviewBroadcast]);
    const drives = player.followups.length;
    planner.settle();
    await tick();

    // The stop did not drain the player relays.
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
    expect(player.followups).toHaveLength(drives);
    expect(h.team.queues.get(PLAYER_ID)).toEqual([reviewBroadcast]);

    // The next drive retrieves the preserved relay.
    h.orchestrator.submit("继续");
    await tick();
    expect(h.team.queues.get(PLAYER_ID)).toEqual([]);
    expect(player.injections).toContain(reviewBroadcast);
  });

  it("still stops after a cancel during the fifth review once the queue is digested (contract §4)", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const { player, planner } = await playFourRounds(h);

    // The fifth game ends; the review turn is in flight when the cancel lands.
    h.game.current = winner();
    h.team.queues.set(PLANNER_ID, [
      relay(player.agent.id, "player", PLAYER_RECORD_RELAY),
    ]);
    player.settle();
    await tick();
    const drives = player.followups.length;
    h.orchestrator.cancel();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      paused: true,
      active: null,
      activation: "planner",
      lastError: null,
    });

    // Resume: the delivered message is handled first, then the flow restarts
    // the review activity (replaying the held record input set); the settled
    // review counts the fifth round and the spent budget reaches the stop
    // without driving the player.
    h.orchestrator.submit("继续");
    await tick();
    expect(planner.followups.map(messageText).at(-1)).toBe("继续");
    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(drives);
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "reviewing",
      active: "planner",
      activation: "planner",
    });

    planner.settle();
    await tick();
    expect(player.followups).toHaveLength(drives);
    expect(h.orchestrator.snapshot()).toEqual(STOPPED_SNAPSHOT);
  });
});

describe("TeamOrchestrator delivery split and error contract (specs/076-agent-v2-optimize/contracts/flow-orchestration.md §3/§5)", () => {
  it("delivers in-flight (steer) and quiescent (assembly) to the active member", async () => {
    const h = createHarness();
    await h.orchestrator.materialize(teamOptions());
    const planner = member(h, PLANNER_ID);
    const player = member(h, PLAYER_ID);

    // Quiescent: the message assembles a turn (followup).
    expect(h.orchestrator.submit("首条").queued).toBe(false);
    expect(planner.followups.map(messageText)).toEqual(["首条"]);
    expect(planner.injections).toEqual([]);

    // In flight: the message steers into the turn.
    expect(h.orchestrator.submit("在途").queued).toBe(true);
    expect(planner.steers.map(messageText)).toEqual(["在途"]);
    expect(planner.followups.map(messageText)).toEqual(["首条"]);

    planner.settle();
    await tick();
    // plan → play, whose empty drain rests the loop at the player.
    expect(h.orchestrator.snapshot()).toMatchObject({
      phase: "playing",
      active: null,
      activation: "player",
    });

    // The next quiescent delivery belongs to the new active member.
    h.orchestrator.submit("给 player");
    expect(player.followups.map(messageText)).toEqual(["给 player"]);
    player.settle();
    await h.orchestrator.whenQuiescent();
  });

  const codes: ReadonlyArray<[string, string]> = [
    ["RATE_LIMIT", "TRANSIENT"],
    ["QUOTA", "QUOTA"],
    ["INVALID_CREDENTIAL", "CREDENTIAL"],
    ["NO_ADAPTER", "CONFIG"],
    ["CONTEXT_WINDOW_EXCEEDED", "INPUT_TOO_LARGE"],
    ["INVARIANT", "INTERNAL"],
  ];

  for (const [engineCode, loopCode] of codes) {
    it(`reports the loop code ${loopCode} for the engine failure ${engineCode}`, async () => {
      const logger = { error: vi.fn() };
      const h = createHarness({ logger });
      await h.orchestrator.materialize(teamOptions());
      const planner = member(h, PLANNER_ID);

      h.orchestrator.submit("首条");
      planner.failCurrentTurn(engineCode);
      await tick();

      expect(h.orchestrator.snapshot()).toMatchObject({
        paused: true,
        failed: true,
        lastError: {
          code: loopCode,
          origin: "member-turn",
          member: "planner",
          detail: { engineCode, message: `injected ${engineCode} failure` },
        },
      });
      expect(logger.error.mock.calls[0]?.[1]).toMatchObject({ code: loopCode });
    });
  }

  it("freezes on a member failure and restarts the current activity on recovery", async () => {
    const logger = { error: vi.fn() };
    const h = createHarness({ logger });
    await h.orchestrator.materialize(teamOptions());
    const planner = member(h, PLANNER_ID);

    // plan freezes on failure; nothing is driven while frozen.
    h.orchestrator.submit("首条");
    planner.failCurrentTurn("SERVER");
    await tick();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      active: null,
      activation: "planner",
    });

    // Recovery: the delivery drive, then the flow restarts plan (whose replay
    // is empty here) and judges the edge.
    h.orchestrator.submit("恢复");
    expect(planner.followups.map(messageText)).toEqual(["首条", "恢复"]);
    planner.settle();
    await h.orchestrator.whenQuiescent();
    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: false,
      failed: false,
      lastError: null,
      activation: "player",
    });
  });

  it("freezes on abort without writing a failure fact (the aborted channel)", async () => {
    const logger = { error: vi.fn() };
    const h = createHarness({ logger });
    await h.orchestrator.materialize(teamOptions());
    const planner = member(h, PLANNER_ID);

    h.orchestrator.submit("首条");
    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });
    await h.orchestrator.whenQuiescent();

    expect(h.orchestrator.snapshot()).toMatchObject({
      paused: true,
      failed: false,
      lastError: null,
      active: null,
      activation: "planner",
    });
    expect(logger.error).not.toHaveBeenCalled();
    expect(h.orchestrator.cancel()).toEqual({ dropped: [] });
  });
});
