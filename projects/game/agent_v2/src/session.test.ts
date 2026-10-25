import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentHandle, AgentStatus, CreateAgentOptions } from "@deepseek-ai/dsh-agent";
import type { PromptAssembly } from "@deepseek-ai/dsh-system-prompt";
import { createUserMessage, LlmError } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { TeamEventStream, TeamHandle, TeamLiveEvent, TeamMemberEvent, TeamMemberSource, TeamRegistration } from "@dominion/dsh-team";
import { DEFAULT_MODEL, LOOP_ERROR_TEXT, TeamSessionError, TeamSessions, loopErrorText } from "./session.js";
import type { TeamView } from "./session.js";
import type { MemberCollector, TeamHistory, TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type {
  SceneMember,
  SceneOrchestration,
  SceneRegistry,
  SceneSnapshot,
} from "./orchestration.js";
import type { SceneSpec } from "./scenes.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/ChatEvent.js";
import type { AnnouncerMember } from "@dominion/dsh-saolei-loop";
import type { GameEventRecord, GameRuntime } from "@dominion/dsh-saolei";

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
 * The team service is a hand-rolled double (the harness's established seam —
 * the real plugin's fan-out semantics are covered by its own unit tests):
 * `register` asserts the scene roster and returns a handle whose stream fans
 * every member source's productions out under the member envelope, and the
 * member agent scopes are fake contexts the tests emit into — no module
 * interception (style/javascript.md Mock convention;
 * specs/077-announcer-member-unification/contracts/member-event-stream.md §5).
 */

type Listener = (...args: never[]) => void;

const S1 = "templates/saolei/sessions/s1";
const P_PLAYER = "templates/saolei/presets/p-player";
const P_PLANNER = "templates/saolei/presets/p-planner";
/** A second planner preset for refresh-swap cases; its id half ends in "planner". */
const P_PLANNER_ALT = "templates/saolei/presets/p-alt-planner";
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
    systemPrompt: {
      assemble: ReturnType<typeof vi.fn>;
      section: ReturnType<typeof vi.fn>;
    };
    /** The member-scope tool registration surface the orchestrator writes. */
    tools: { register: ReturnType<typeof vi.fn> };
    /** The agent-scoped face registration surface (isolate → provide). */
    isolate: ReturnType<typeof vi.fn>;
  };
  readonly handle: AgentHandle;
  readonly followups: UserMessage[];
  readonly injections: UserMessage[];
  readonly steers: UserMessage[];
  readonly cancels: unknown[];
  /** Emit one event into this member's agent scope (the member source faces' input). */
  emitEvent(name: string, ...args: unknown[]): void;
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
  readonly authoring: { compose: ReturnType<typeof vi.fn>; get: ReturnType<typeof vi.fn> };
  readonly listModels: ReturnType<typeof vi.fn>;
  /** The `memoryMount.build` spy (the planner capability factory). */
  readonly memoryMount: ReturnType<typeof vi.fn>;
  /** The built unit's fail-loud prefetch spy. */
  readonly memoryReady: ReturnType<typeof vi.fn>;
  readonly createGameRuntime: ReturnType<typeof vi.fn>;
  readonly teamQueues: Map<string, UserMessage[]>;
  readonly teamRegister: ReturnType<typeof vi.fn>;
  /**
   * The unified-stream subscribe/detach accounting of the team double: one
   * subscribe per materialized entry, one detach at its teardown
   * (specs/077-announcer-member-unification/contracts/host-member-parity.md §1).
   */
  readonly teamStreamSubscriptions: { subscribes: number; detaches: number };
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
  const listenersByEvent = new Map<string, Listener[]>();
  const idleWaiters: Array<() => void> = [];
  const steers: UserMessage[] = [];
  const nextStep: UserMessage[] = [];
  const nextTurn: UserMessage[] = [];
  const resolveIdle = (): void => {
    for (const resolve of idleWaiters.splice(0)) {
      resolve();
    }
  };
  const memberCtx: {
    on: ReturnType<typeof vi.fn>;
    systemPrompt: {
      assemble: ReturnType<typeof vi.fn>;
      section: ReturnType<typeof vi.fn>;
    };
    tools: { register: ReturnType<typeof vi.fn> };
    isolate: ReturnType<typeof vi.fn>;
    agent?: Agent;
  } = {
    on: vi.fn((name: string, listener: Listener) => {
      const list = listenersByEvent.get(name) ?? [];
      list.push(listener);
      listenersByEvent.set(name, list);
      return () => {
        const current = listenersByEvent.get(name) ?? [];
        const index = current.indexOf(listener);
        if (index >= 0) {
          current.splice(index, 1);
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
      section: vi.fn(() => () => {}),
    },
    // The member-scope registration surfaces the orchestrator writes
    // (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §4).
    tools: { register: vi.fn(() => () => {}) },
    isolate: vi.fn(() => ({ provide: vi.fn(() => () => {}) })),
  };
  const agent = {
    id,
    session: { id, events: [] },
    get status(): AgentStatus {
      return state.status;
    },
    ctx: memberCtx,
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
      member.injections.push(message);
      nextStep.push(message);
    }),
    steer: vi.fn((message: UserMessage) => {
      steers.push(message);
      nextStep.push(message);
      state.status = "running";
      emitStatus(member, "running");
    }),
    followup: vi.fn((message: UserMessage) => {
      member.followups.push(message);
      state.status = "running";
      emitStatus(member, "running");
    }),
    cancel: vi.fn((cause: unknown) => {
      member.cancels.push(cause);
      nextStep.length = 0;
      nextTurn.length = 0;
      state.status = "idle";
      emitStatus(member, "idle");
      resolveIdle();
    }),
    whenIdle: vi.fn(
      () => new Promise<void>((resolve) => idleWaiters.push(resolve)),
    ),
  } as unknown as Agent;
  memberCtx.agent = agent;
  const member: MemberFake = {
    id,
    agent,
    ctx: memberCtx as MemberFake["ctx"],
    handle: { agent, dispose: vi.fn(async () => {}) },
    followups: [],
    injections: [],
    steers,
    cancels: [],
    emitEvent: (name: string, ...args: unknown[]) => {
      for (const listener of [...(listenersByEvent.get(name) ?? [])]) {
        (listener as (...emitArgs: unknown[]) => void)(...args);
      }
    },
    settle: () => {
      state.status = "idle";
      emitStatus(member, "idle");
      resolveIdle();
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
  /** Omit the `memoryMount` provide face (composition-error test). */
  readonly omitMemoryMount?: boolean;
  /** Injected scene registry (defaults to the module scene table). */
  readonly scenes?: SceneRegistry;
  /** Role the authoring double reports for a preset id (synthetic scenes). */
  readonly presetRole?: (presetId: string) => string;
}

function createHarness(options: HarnessOptions = {}): Harness {
  // The host context is a plain fake: nothing subscribes at the root — the
  // entry consumes the team's unified stream — so `on` only satisfies the
  // DshContext shape.
  const on = vi.fn(() => () => {});
  const members = new Map<string, MemberFake>();
  // The members' own scopes carry every registration (the member sources'
  // log/live faces), so one emit dispatches to the member's listeners exactly
  // like the dsh root event would.
  const emitStatus = (member: MemberFake, status: AgentStatus): void => {
    member.emitEvent("agent/status", { agent: member.agent, status });
  };
  const emitError = (member: MemberFake, error: unknown): void => {
    member.emitEvent("agent/error", { agent: member.agent, error });
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
      role: options.presetRole?.(id) ?? (id.endsWith("planner") ? "planner" : "player"),
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
  // The two capability provide faces the saolei-loop apply would have mounted
  // (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3): the
  // orchestrator reads them off the host context when it assembles a member.
  const saoleiTools = {
    tools: [
      { name: "saolei_init" },
      { name: "saolei_operate" },
      { name: "saolei_remain" },
    ],
    sections: [{ name: "saolei:guidance", order: 100, text: "" }],
  };
  const memoryReady = vi.fn(async () => {});
  const memoryCapabilities = {
    faces: [{ key: "memory", value: { kind: "memory-face" } }],
    tools: [
      { name: "memory" },
      { name: "wiki_search" },
      { name: "wiki_get" },
      { name: "wiki_write" },
    ],
    sections: [
      { name: "memory:guidance", order: 110, text: "" },
      { name: "wiki:guidance", order: 120, text: "" },
      { name: "memory:snapshot", order: 200, text: "" },
    ],
    ready: memoryReady,
  };
  const memoryBuild = vi.fn(() => memoryCapabilities);
  const memoryMount = { build: memoryBuild };
  const game = { current: null as GameEventRecord | null };
  const gameRuntime = {
    init: vi.fn(async () => ({ isError: false as const, text: "" })),
    operate: vi.fn(async () => ({ isError: false as const, text: "" })),
    remain: vi.fn(() => ({ isError: false as const, text: "" })),
    peekGameEvent: () => game.current,
  } as unknown as GameRuntime;
  const createGameRuntime = vi.fn(() => gameRuntime);
  const teamQueues = new Map<string, UserMessage[]>();
  // The host takes exactly one unified-stream subscription per entry, and
  // releases it exactly once at teardown — the accounting the single-path
  // assertion reads (the real team plugin's stream is its own unit-tested
  // surface).
  const teamStreamSubscriptions = { subscribes: 0, detaches: 0 };
  const teamRegister = vi.fn((registration: TeamRegistration): TeamHandle => {
    // The saolei team's three-member roster: player + planner + the
    // announce-only announcer (specs/076-agent-v2-optimize/contracts/
    // announcer-member.md §1/§3).
    expect(registration.members).toHaveLength(3);
    const announcer = registration.members[2];
    expect(announcer).toMatchObject({
      role: "announcer",
      summary: "终局播报对局结果与操作统计",
    });
    expect(announcer?.source.id).toBe(`${S1}/announcer`);
    expect(announcer?.source.consumes).toBe(false);
    expect(announcer?.source.sectionTarget).toBeUndefined();

    // The registration's unified member event stream, fanning each member
    // source's productions out under the member envelope — the shape the real
    // team plugin produces
    // (specs/077-announcer-member-unification/contracts/member-event-stream.md
    // §2.2/§3).
    const consumers = new Set<(event: TeamMemberEvent) => void>();
    const sourceOffs: Array<(() => void) | undefined> = [];
    const fanOut = (event: TeamMemberEvent): void => {
      for (const consumer of [...consumers]) {
        consumer(event);
      }
    };
    for (const member of registration.members) {
      sourceOffs.push(
        member.source.subscribe?.((event) => {
          fanOut({ role: member.role, memberId: member.source.id, channel: "log", event });
        }),
        member.source.live?.subscribe((event) => {
          fanOut({ role: member.role, memberId: member.source.id, channel: "live", event });
        }),
      );
    }
    const stream: TeamEventStream = {
      subscribe: (consumer) => {
        consumers.add(consumer);
        teamStreamSubscriptions.subscribes += 1;
        let attached = true;
        return () => {
          if (!attached) {
            return;
          }
          attached = false;
          consumers.delete(consumer);
          teamStreamSubscriptions.detaches += 1;
        };
      },
    };
    return {
      // Disposal drops the member subscriptions with the registration (the
      // real team service's member teardown), so an old lifecycle's late
      // production reaches no consumer.
      dispose: () => {
        for (const off of sourceOffs.splice(0)) {
          off?.();
        }
        consumers.clear();
      },
      events: () => stream,
    };
  });
  const fiberDispose = vi.fn(async () => {});
  const loggerError = vi.fn();
  const desktopBridge = {};
  const ctx = {
    on,
    agents: { create: agentsCreate, get: vi.fn() },
    get: vi.fn((name: string) => {
      if (name === "presetAuthoring") {
        return authoring;
      }
      if (name === "saoleiTools") {
        return saoleiTools;
      }
      if (name === "memoryMount") {
        return options.omitMemoryMount === true ? undefined : memoryMount;
      }
      if (name === "desktopBridge") {
        return desktopBridge;
      }
      return undefined;
    }),
    // The composition-level game foundation face (the top-level saolei row
    // provides it; contracts/team-loop-assembly.md §2).
    saolei: {
      createGameRuntime,
      gameSection: { name: "saolei:game", order: 50, text: "" },
    },
    desktopBridge,
    fiber: { dispose: fiberDispose },
    llm: { listModels: vi.fn(async () => [{ id: "glm-5.3" }]) },
  } as unknown as DshContext;
  const sessions = new TeamSessions(ctx, {
    authoring,
    listModels,
    agents: { create: agentsCreate },
    team: {
      register: teamRegister,
      drain: (member: TeamMemberSource) => teamQueues.get(member.id)?.splice(0) ?? [],
    },
    logger: { error: loggerError },
    provider: "glm-responses",
    scenes: options.scenes,
  });
  return {
    ctx,
    sessions,
    agentsCreate,
    members,
    authoring,
    listModels,
    memoryMount: memoryBuild,
    memoryReady,
    createGameRuntime,
    teamQueues,
    teamRegister,
    teamStreamSubscriptions,
    fiberDispose,
    loggerError,
    game,
  };
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

/**
 * A scene orchestration double for the synthetic-scene tests: the seam
 * surface plus the test controls (emit one envelope, settle the quiescent
 * point). It enters through the injected scene registry's factory, never
 * through module interception (style/javascript.md Mock convention;
 * specs/080-host-main-sub-model/contracts/scene-registry.md §1).
 */
interface SceneStub {
  readonly orchestration: SceneOrchestration;
  readonly materialize: ReturnType<typeof vi.fn>;
  readonly submit: ReturnType<typeof vi.fn>;
  readonly dispose: ReturnType<typeof vi.fn>;
  emit(envelope: TeamMemberEvent): void;
  settle(): void;
}

function sceneStub(
  handle: AgentHandle,
  projected: readonly SceneMember[],
): SceneStub {
  const consumers = new Set<(event: TeamMemberEvent) => void>();
  const state = {
    materialized: false,
    active: null as string | null,
    paused: false,
    queued: 0,
    failed: false,
  };
  let resolveQuiescent: (() => void) | undefined;
  const materialize = vi.fn(async () => {
    state.materialized = true;
  });
  const submit = vi.fn((_text: string) => {
    state.active = "single";
  });
  const dispose = vi.fn(async () => {
    state.materialized = false;
  });
  const orchestration: SceneOrchestration = {
    materialize,
    members: () => (state.materialized ? projected : []),
    member: (role) =>
      projected.some((member) => member.role === role && member.kind === "agent")
        ? handle
        : undefined,
    teamEvents: () => ({
      subscribe: (consumer) => {
        consumers.add(consumer);
        return () => {
          consumers.delete(consumer);
        };
      },
    }),
    snapshot: (): SceneSnapshot => ({
      materialized: state.materialized,
      active: state.active,
      activation: "single",
      paused: state.paused,
      queued: state.queued,
      failed: state.failed,
      lastError: null,
    }),
    submit,
    cancel: vi.fn(),
    whenQuiescent: () =>
      new Promise<void>((resolve) => {
        resolveQuiescent = resolve;
      }),
    dispose,
  };
  return {
    orchestration,
    materialize,
    submit,
    dispose,
    emit: (envelope) => {
      for (const consumer of [...consumers]) {
        consumer(envelope);
      }
    },
    settle: () => {
      state.active = null;
      resolveQuiescent?.();
      resolveQuiescent = undefined;
    },
  };
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

/** A terminal game record for the mounted player runtime double. */
function winnerRecord(): GameEventRecord {
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
 * One entry member as the private registry holds it: the discriminated union
 * narrowed to the fields the disappearance assertions read
 * (specs/077-announcer-member-unification/contracts/host-member-parity.md §1).
 */
interface EntryMemberSeam {
  readonly kind: "agent" | "announce-only";
  readonly role: string;
  readonly preset?: string;
}

/**
 * The private live-entry registry seam: the entry's unified member collection,
 * the dynamic (role, memberId) collectors, the announcer member and the
 * history of one materialization generation (refresh/unsubscribe and
 * roster-derivation assertions).
 */
function entrySeam(
  h: Harness,
  session: string,
): {
  announcer: AnnouncerMember | undefined;
  dynamicCollectors: Map<string, MemberCollector>;
  history: TeamHistory;
  members: ReadonlyArray<EntryMemberSeam>;
} {
  const teams = (
    h.sessions as unknown as {
      teams: Map<
        string,
        {
          orchestrator: { announcer?: AnnouncerMember };
          dynamicCollectors: Map<string, MemberCollector>;
          history: TeamHistory;
          members: ReadonlyArray<EntryMemberSeam>;
        }
      >;
    }
  ).teams;
  const entry = teams.get(session);
  if (entry === undefined) {
    throw new Error(`session "${session}" has no live entry`);
  }
  return {
    announcer: entry.orchestrator.announcer,
    dynamicCollectors: entry.dynamicCollectors,
    history: entry.history,
    members: entry.members,
  };
}

async function materializeDefault(h: Harness): Promise<TeamView> {
  return h.sessions.materialize(S1, { members: defaultMembers() });
}

/** Emit one member text turn's dsh event sequence and settle it via idle. */
function driveTextTurn(h: Harness, target: MemberFake, text: string): void {
  const attemptId = `${target.id}/attempt-1`;
  target.emitEvent("session/event", target.agent.session, { type: "turn/start", data: { turn: 1 } });
  // Live model chunks ride `agent/assistant-stream`; there is no
  // `assistant/chunk` session event (specs/070-dsh-upgrade/research.md D7 终态结论).
  target.emitEvent("agent/assistant-stream", {
    agent: target.agent,
    frame: { type: "start", attemptId, revision: 1, turn: 1, step: 1 },
  });
  target.emitEvent("agent/assistant-stream", {
    agent: target.agent,
    frame: {
      type: "chunk",
      attemptId,
      revision: 2,
      index: 0,
      time: 0,
      chunk: { type: "block-start", index: 0, blockType: "text" },
    },
  });
  target.emitEvent("agent/assistant-stream", {
    agent: target.agent,
    frame: { type: "chunk", attemptId, revision: 3, index: 1, time: 0, chunk: { type: "text-delta", index: 0, text } },
  });
  target.emitEvent("agent/assistant-stream", {
    agent: target.agent,
    frame: {
      type: "chunk",
      attemptId,
      revision: 4,
      index: 2,
      time: 0,
      chunk: { type: "block-end", index: 0, block: { type: "text", text } },
    },
  });
  target.emitEvent("session/event", target.agent.session, {
    type: "assistant/message",
    data: { turn: 1, step: 1, message: { content: [{ type: "text", text }] } },
  });
  target.emitEvent("agent/assistant-stream", {
    agent: target.agent,
    frame: { type: "end", attemptId, index: 3, outcome: { kind: "committed", eventType: "assistant/message", seq: 1 } },
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
    expect(h.createGameRuntime).toHaveBeenCalledTimes(1);
    expect(h.memoryMount).toHaveBeenCalledTimes(1);
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
      // The announce-only announcer closes the roster: no preset/model
      // (specs/076-agent-v2-optimize/data-model.md §4).
      [`${S1}/team/members/announcer`, "announcer", "", ""],
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
    // contract); the provider catalogs queried are the two split routes. The
    // announcer closes the roster with no model.
    expect(view.members.map((entry) => entry.model)).toEqual([
      "opencode-go/kimi-k3",
      DEFAULT_MODEL,
      "",
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

  it("accepts the sts2 scene and validates roster shapes against its single main member", async () => {
    const sts2Session = "templates/sts2/sessions/s1";
    const h = createHarness({ presetRole: () => "main" });

    // Empty members: the scene-agnostic structure layer rejects first.
    await expect(h.sessions.materialize(sts2Session, { members: [] })).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: "team.members must not be empty",
    });
    // The two-member saolei-shaped roster is a scene roster violation: the
    // sts2 scene materializes exactly one member
    // (specs/080-host-main-sub-model/contracts/face-main-sub.md §1).
    await expect(
      h.sessions.materialize(sts2Session, {
        members: [
          { role: "player", preset: "templates/sts2/presets/p-player" },
          { role: "planner", preset: "templates/sts2/presets/p-planner" },
        ],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: "sts2 team materialization requires exactly 1 members (main), got 2",
    });
    // A structurally invalid roster is rejected by the structure layer before
    // the scene roster, matching the two-layer fail-fast order.
    await expect(
      h.sessions.materialize(sts2Session, { members: [{ role: "", preset: "nope" }] }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: "every team member must carry a non-empty role",
    });

    // No preset/model validation or orchestration path was entered, and the
    // session stays unmaterialized.
    expect(h.authoring.get).not.toHaveBeenCalled();
    expect(h.listModels).not.toHaveBeenCalled();
    expect(h.agentsCreate).not.toHaveBeenCalled();
    expect(h.teamRegister).not.toHaveBeenCalled();
    expect(() => h.sessions.getTeam(sts2Session)).toThrow("team not materialized");
  });

  it("keeps the saolei roster error text byte-for-byte", async () => {
    const h = createHarness();

    await expect(
      h.sessions.materialize(S1, { members: [{ role: "player", preset: P_PLAYER }] }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message:
        "saolei team materialization requires exactly 2 members (player and planner), got 1",
    });
    await expect(
      h.sessions.materialize(S1, {
        members: [
          { role: "player", preset: P_PLAYER },
          { role: "player", preset: P_PLAYER },
        ],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message:
        'saolei team member roles must be exactly {"player", "planner"}, got [player, player]',
    });
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
    // The announcer enters the roster server-side and is never
    // caller-configurable: a request carrying it stays INVALID_ARGUMENT
    // (specs/076-agent-v2-optimize/contracts/announcer-member.md §3 item 4).
    await expect(
      h.sessions.materialize(S1, {
        members: [
          { role: "player", preset: P_PLAYER },
          { role: "planner", preset: P_PLANNER },
          { role: "announcer", preset: P_PLANNER },
        ],
      }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    await expect(
      h.sessions.materialize(S1, {
        members: [
          { role: "player", preset: P_PLAYER },
          { role: "announcer", preset: P_PLANNER },
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
    h.memoryReady.mockRejectedValueOnce(new Error("memory unavailable"));

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

/** The provide spy of a member's first agent-scoped face registration. */
function faceProvide(member: MemberFake): Spy {
  const first = member.ctx.isolate.mock.results[0] as
    | { value: { provide: Spy } }
    | undefined;
  if (first === undefined) {
    throw new Error("the member registered no agent-scoped face");
  }
  return first.value.provide;
}

/**
 * Assert the contract's fixed memory registration order
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §4):
 * faces → tools(×4) → sections(×3) → ready. The planner's first section is the
 * gameplay section registered before the memory unit
 * (specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §3), so the
 * memory sections are the section calls after the first. The seams of one
 * member setup are compared through `mock.invocationCallOrder`, so an
 * out-of-order implementation fails this assertion.
 */
function expectMemoryRegistrationOrder(h: Harness, planner: MemberFake): void {
  const provide = faceProvide(planner);
  const sectionOrders =
    planner.ctx.systemPrompt.section.mock.invocationCallOrder;
  expect(provide).toHaveBeenCalledTimes(1);
  expect(planner.ctx.tools.register).toHaveBeenCalledTimes(4);
  expect(planner.ctx.systemPrompt.section).toHaveBeenCalledTimes(4);
  expect(h.memoryReady).toHaveBeenCalledTimes(1);
  // The gameplay section precedes the memory faces; the three memory sections
  // follow the memory tools.
  expect(sectionOrders[0] as number).toBeLessThan(firstCall(provide));
  expect(lastCall(provide)).toBeLessThan(
    firstCall(planner.ctx.tools.register),
  );
  expect(lastCall(planner.ctx.tools.register)).toBeLessThan(
    sectionOrders[1] as number,
  );
  expect(sectionOrders[3] as number).toBeLessThan(firstCall(h.memoryReady));
}

describe("TeamSessions planner memory wiring (074 team-assembly.md §4)", () => {
  it(
    "builds the planner memory unit with the materialization-source scope " +
      "and prefetches once",
    async () => {
      const h = createHarness();

      await materializeDefault(h);

      // Only the planner member builds + prefetches; the factory receives the
      // (template, materialization source preset) scope halves — the id
      // halves, NOT full resource names (the memory client builds
      // `templates/{template}/presets/{preset}` from these halves;
      // specs/071-memory-preset-scope/contracts/dsh-plugins.md §4).
      expect(h.memoryMount).toHaveBeenCalledTimes(1);
      expect(h.memoryMount.mock.calls[0]?.[0]).toEqual({
        template: "saolei",
        preset: "p-planner",
      });
      expect(h.memoryReady).toHaveBeenCalledTimes(1);
      // The fixed cross-face order on the planner member:
      // faces → tools ×4 → sections ×3 → ready
      // (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §4).
      const planner = member(h, PLANNER_ID);
      expectMemoryRegistrationOrder(h, planner);
      // The player branch consumed no memory capability; it registered the
      // game handle instead (the orchestrator-owned instance).
      const player = member(h, PLAYER_ID);
      expect(h.memoryMount).toHaveBeenCalledTimes(1);
      expect(player.ctx.isolate).toHaveBeenCalledTimes(1);
      expect(player.ctx.isolate).toHaveBeenCalledWith("saoleiGame");
    },
  );

  it("re-derives the scope halves on refresh, following a swapped planner preset", async () => {
    const h = createHarness();

    await materializeDefault(h);
    expect(h.memoryMount.mock.calls[0]?.[0]).toEqual({
      template: "saolei",
      preset: "p-planner",
    });

    // Refresh with the same configuration: the scope stays the two id halves
    // built from the unchanged materialization source preset
    // (specs/071-memory-preset-scope/contracts/dsh-plugins.md §4).
    await h.sessions.materialize(S1, { members: defaultMembers() });
    expect(h.memoryMount).toHaveBeenCalledTimes(2);
    expect(h.memoryMount.mock.calls[1]?.[0]).toEqual({
      template: "saolei",
      preset: "p-planner",
    });

    // Refresh swapping the planner preset: the fresh planner reads the NEW
    // preset's collection, so the scope follows the swapped member
    // (specs/071-memory-preset-scope/spec.md FR-009).
    await h.sessions.materialize(S1, {
      members: [
        { role: "player", preset: P_PLAYER },
        { role: "planner", preset: P_PLANNER_ALT },
      ],
    });
    expect(h.memoryMount).toHaveBeenCalledTimes(3);
    expect(h.memoryMount.mock.calls[2]?.[0]).toEqual({
      template: "saolei",
      preset: "p-alt-planner",
    });
  });

  it(
    "rolls the whole materialization back when the prefetch rejects " +
      "(no half-materialized team)",
    async () => {
      const h = createHarness();
      h.memoryReady.mockRejectedValueOnce(
        new Error("memory service unreachable"),
      );

      await expect(materializeDefault(h)).rejects.toThrow(
        "memory service unreachable",
      );
      const player = member(h, PLAYER_ID);
      expect(player.handle.dispose).toHaveBeenCalledTimes(1);
      expect(() => h.sessions.getTeam(S1)).toThrow(TeamSessionError);
      // The team never registered: the rollback is complete.
      expect(h.teamRegister).not.toHaveBeenCalled();

      // Retry with the storage reachable materializes from scratch.
      h.memoryReady.mockImplementation(async () => {});
      await materializeDefault(h);
      expect(h.sessions.getTeam(S1).name).toBe(`${S1}/team`);
      expect(h.memoryReady).toHaveBeenCalledTimes(2);
    },
  );

  it("fails loud when the memoryMount capability is not composed", async () => {
    const h = createHarness({ omitMemoryMount: true });

    await expect(materializeDefault(h)).rejects.toThrow(
      /"memoryMount" capability is not mounted/,
    );
    // The failure lands in the planner setup, after both member creations
    // were invoked and the player was rolled back (no half-materialized team).
    expect(h.agentsCreate).toHaveBeenCalledTimes(2);
    expect(h.teamRegister).not.toHaveBeenCalled();
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

    // The second send steered into the in-flight planner turn (mid-turn
    // delivery, specs/061-team-queue-steer/spec.md).
    expect(planner.steers.map(messageText)).toEqual(["second"]);

    // Settling the planner turn advances plan → play; the empty player drain
    // rests the loop, so both streams end at the team's static point.
    driveTextTurn(h, planner, "reply one");
    await flush();

    expect(planner.followups).toHaveLength(1);
    expect(first.ended).toBe(true);
    expect(second.ended).toBe(true);
    expect(h.sessions.listTeamMessages(S1).map((entry) => entry.member)).toEqual([
      "user",
      "user",
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

describe("TeamSessions announcer announcement projection (specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §4)", () => {
  /** Drive the team to the player's terminal turn (planner strategy → player). */
  async function driveToTerminalTurn(h: Harness, stream: StreamRecorder): Promise<void> {
    h.sessions.send(S1, "请开始", stream);
    await flush();
    const planner = member(h, PLANNER_ID);
    h.teamQueues.set(PLAYER_ID, [relay(planner.agent, "planner", "开局策略")]);
    driveTextTurn(h, planner, "开局策略");
    await flush();
    h.game.current = winnerRecord();
    driveTextTurn(h, member(h, PLAYER_ID), "已点击 (0,0)");
    await flush();
  }

  it("projects the announcement into the merge history and fans out its team_message frame", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const stream = fakeStream();

    await driveToTerminalTurn(h, stream);

    const statsText =
      "本局游戏结束：胜利。\n本局共执行 5 个操作：click 4 次、flag 1 次、chord 0 次。";
    const announcement = h.sessions
      .listTeamMessages(S1)
      .find((entry) => entry.member === "announcer");
    expect(announcement?.message.role).toBe("ROLE_AGENT");
    expect(announcement?.message.blocks).toEqual([{ text: { content: statsText } }]);

    // The live team_message frame carries the same entry (seq + message
    // object), so ListTeamMessages and the stream cannot diverge.
    const frame = stream.events.find((event) => event.teamMessage?.member === "announcer");
    expect(frame?.teamMessage?.seq).toBe(String(announcement?.seq));
    expect(frame?.teamMessage?.message).toBe(announcement?.message);
  });

  it("detaches the announcer subscription with the entry teardown (refresh)", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const old = entrySeam(h, S1);
    expect(old.announcer).toBeDefined();

    // Refresh tears the old generation down; its subscription detaches with
    // the entry, so a late announcement cannot enter that history.
    await h.sessions.materialize(S1, { members: defaultMembers() });

    old.announcer?.announce("旧生命周期播报");
    expect(old.history.listTeamMessages()).toEqual([]);
    // The new generation's projections start empty.
    expect(h.sessions.listTeamMessages(S1)).toEqual([]);
  });
});

describe("TeamSessions unified member holding (specs/077-announcer-member-unification/contracts/host-member-parity.md §1/§2)", () => {
  it("holds the three members uniformly, derives the roster from them, and takes one unified-stream subscription per entry", async () => {
    const h = createHarness();
    await materializeDefault(h);

    // The entry's single member-event consumption path: one unified-stream
    // subscription, with no parallel announce-only subscription.
    expect(h.teamStreamSubscriptions).toEqual({ subscribes: 1, detaches: 0 });

    const entry = entrySeam(h, S1);
    expect(entry.members.map((member) => [member.role, member.kind])).toEqual([
      ["player", "agent"],
      ["planner", "agent"],
      ["announcer", "announce-only"],
    ]);
    // The agent members own a handle + collector; the announce-only member
    // holds no runtime at all — the empty object pins that absence.
    expect(entry.members[0]).toMatchObject({ kind: "agent", role: "player", preset: P_PLAYER });
    expect(entry.members[1]).toMatchObject({ kind: "agent", role: "planner", preset: P_PLANNER });
    expect(entry.members[2]).toEqual({ kind: "announce-only", role: "announcer" });

    // The roster is the projection of that unified collection: same members,
    // the announce-only one in its empty preset/model form.
    const view = h.sessions.getTeam(S1);
    expect(view.members.map((member) => member.role)).toEqual(
      entry.members.map((member) => member.role),
    );
    expect(view.members.map((member) => [member.preset, member.model])).toEqual([
      [P_PLAYER, DEFAULT_MODEL],
      [P_PLANNER, DEFAULT_MODEL],
      ["", ""],
    ]);

    // Refresh releases the old entry through its single detach and takes
    // exactly one new subscription.
    await h.sessions.materialize(S1, { members: defaultMembers() });
    expect(h.teamStreamSubscriptions).toEqual({ subscribes: 2, detaches: 1 });
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
  it("breaks out of a frozen, failed orchestration with a stream error instead of spinning or ending cleanly", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);
    const player = member(h, PLAYER_ID);

    // The first send starts a planner turn; two more sends steer into it.
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

    // The planner's output relays a strategy; the flow's next drive (the
    // player) throws at the start site, surfacing the orchestration-layer
    // failure.
    h.teamQueues.set(PLAYER_ID, [relay(planner.agent, "planner", "策略")]);
    (player.agent.followup as ReturnType<typeof vi.fn>).mockImplementationOnce(() => {
      throw new Error("drive exploded");
    });
    planner.settle();
    await flush();

    // The failure is surfaced, not disguised as a natural stop: every active
    // stream receives the INTERNAL failure (with the host's user-action text)
    // and the injected logger records it.
    expect(stream.failures).toEqual([
      { code: "INTERNAL", message: "drive exploded — 内部错误" },
    ]);
    expect(second.failures).toEqual([
      { code: "INTERNAL", message: "drive exploded — 内部错误" },
    ]);
    expect(third.failures).toEqual([
      { code: "INTERNAL", message: "drive exploded — 内部错误" },
    ]);
    expect(stream.ended).toBe(false);
    expect(h.loggerError).toHaveBeenCalledTimes(1);
    expect(h.loggerError.mock.calls[0]?.[1]).toMatchObject({
      session: S1,
      member: "player",
      code: "INTERNAL",
      error: "drive exploded",
    });

    // A later send lifts the freeze: the delivery turn, then the flow's
    // restart of the current activity (the held replay); the team then
    // quiesces and the new stream ends.
    const resumed = fakeStream();
    h.sessions.send(S1, "resume", resumed);
    await flush();
    player.settle();
    await flush();
    player.settle();
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
      code: "TRANSIENT",
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
    planner.emitEvent("session/event", planner.agent.session, {
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
    // The announcer is served by the same member read: the announce-only
    // member has no preset/model and its output-only system_prompt is empty
    // (specs/076-agent-v2-optimize/contracts/announcer-member.md §3 item 3).
    const announcer = await h.sessions.getTeamMember(S1, "announcer");
    expect(announcer).toEqual({
      name: `${S1}/team/members/announcer`,
      role: "announcer",
      preset: "",
      model: "",
      systemPrompt: "",
    });
    // The team snapshot member projection keeps system_prompt empty (the
    // field is served only by GetTeamMember).
    expect(h.sessions.getTeam(S1).members.every((entry) => entry.systemPrompt === "")).toBe(true);
    await expect(h.sessions.getTeamMember(S1, "robot")).rejects.toMatchObject({
      code: "NOT_FOUND",
      message: 'team member "robot" does not exist; the roster is player/planner/announcer',
    });
    // A malformed session resource name fails loud at the read entry instead
    // of degrading the roster description.
    await expect(h.sessions.getTeamMember("sessions/s1", "player")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message:
        'session must be a game session resource name ("templates/{template}/sessions/{session}"), got "sessions/s1"',
    });
    let viewError: unknown;
    try {
      h.sessions.listMemberMessages(S1, "announcer");
    } catch (err) {
      viewError = err;
    }
    expect(viewError).toMatchObject({
      code: "NOT_FOUND",
      message:
        'team member "announcer" has no message view; the member-view roster is player/planner',
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

describe("TeamSessions loop error presentation (specs/076-agent-v2-optimize/spec.md FR-107)", () => {
  it("maps every loop failure code onto a non-empty user-action text", () => {
    const codes = [
      "TRANSIENT",
      "QUOTA",
      "CREDENTIAL",
      "CONFIG",
      "INPUT_TOO_LARGE",
      "INTERNAL",
    ] as const;

    // The host owns the six-code table; the loop layer only classifies.
    expect(Object.keys(LOOP_ERROR_TEXT).sort()).toEqual([...codes].sort());
    for (const code of codes) {
      expect(loopErrorText(code)).not.toBe("");
      expect(Object.values(LOOP_ERROR_TEXT)).toContain(loopErrorText(code));
    }
  });

  it("carries the loop code's text on the orchestration failure frame", async () => {
    const h = createHarness();
    await materializeDefault(h);
    const planner = member(h, PLANNER_ID);

    const stream = fakeStream();
    // A delivery assembly failure is an orchestration-interior failure
    // (INTERNAL): the frame carries the host's user-action text.
    (planner.agent.followup as ReturnType<typeof vi.fn>).mockImplementationOnce(() => {
      throw new Error("boom");
    });
    h.sessions.send(S1, "first", stream);
    await flush();

    expect(stream.failures).toEqual([
      { code: "INTERNAL", message: `boom — ${LOOP_ERROR_TEXT.INTERNAL}` },
    ]);
  });
});

describe("TeamSessions scene-neutral registry path (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§3)", () => {
  const SOLO = "solo";
  const SOLO_SESSION = `templates/${SOLO}/sessions/s1`;
  const SOLO_PRESET = `templates/${SOLO}/presets/p-solo`;

  it("materializes, drives and refreshes a synthetic single-member scene with no announcer", async () => {
    const solo = fakeMember("solo-agent", () => {}, () => {});
    const stubs: SceneStub[] = [];
    const scene: SceneSpec = {
      roster: ["single"],
      presetRoles: ["single"],
      rolePoolTemplate: { single: "single" },
      orchestratable: true,
      teamGoal: "",
      faceMembers: ["single"],
      createOrchestration: () => {
        const stub = sceneStub(solo.handle, [
          { role: "single", kind: "agent" },
        ]);
        stubs.push(stub);
        return stub.orchestration;
      },
    };
    const h = createHarness({
      scenes: { [SOLO]: scene },
      presetRole: () => "single",
    });

    const view = await h.sessions.materialize(SOLO_SESSION, {
      members: [{ role: "single", preset: SOLO_PRESET }],
    });

    // The host hands the scene the validated neutral input: preset id and the
    // split provider/model route (specs/063-llm-reliability-opencode-go/
    // contracts/model-selection.md §3).
    expect(stubs).toHaveLength(1);
    expect(stubs[0]?.materialize).toHaveBeenCalledWith({
      session: SOLO_SESSION,
      template: SOLO,
      members: [
        {
          role: "single",
          preset: "p-solo",
          provider: "glm-responses",
          model: DEFAULT_BARE_MODEL,
        },
      ],
    });
    // The roster is the orchestration's member projection; no server-added
    // member exists (specs/080-host-main-sub-model/spec.md US1 AC3).
    expect(
      view.members.map((entry) => [entry.role, entry.preset, entry.model]),
    ).toEqual([["single", SOLO_PRESET, DEFAULT_MODEL]]);
    expect(view.activeMember).toBe("single");
    // Member-name pre-check / roster text derive from the injected scene's
    // faceMembers, not from a module-level role literal
    // (specs/080-host-main-sub-model/contracts/scene-registry.md §4 item 5).
    await expect(
      h.sessions.getTeamMember(SOLO_SESSION, "announcer"),
    ).rejects.toMatchObject({
      code: "NOT_FOUND",
      message: 'team member "announcer" does not exist; the roster is single',
    });

    // Drive: the user input is fixed into the merged sequence and handed to
    // the scene orchestration through the seam.
    const stream = fakeStream();
    h.sessions.send(SOLO_SESSION, "hello", stream);
    await flush();
    expect(stubs[0]?.submit).toHaveBeenCalledWith("hello");
    expect(stream.events.some((event) => event.teamMessage?.member === "user")).toBe(true);

    // The scene's unified stream feeds the runtime derived from members():
    // one agent collector for the single role.
    stubs[0]?.emit({
      role: "single",
      memberId: "solo-agent",
      channel: "log",
      event: {
        type: "assistant/message",
        data: { message: { content: [{ type: "text", text: "hi from main" }] } },
      },
    } as unknown as TeamMemberEvent);
    await flush();
    expect(
      h.sessions
        .listTeamMessages(SOLO_SESSION)
        .map((entry) => [entry.member, entry.message.blocks[0]?.text?.content]),
    ).toEqual([
      ["user", "hello"],
      ["single", "hi from main"],
    ]);
    expect(
      stream.events.some((event) => event.teamMessage?.member === "single"),
    ).toBe(true);

    // Settling the orchestration ends the stream at its quiescent point.
    stubs[0]?.settle();
    await flush();
    expect(stream.ended).toBe(true);

    // Refresh rebuilds through the scene factory: the old generation is
    // disposed, create_time survives and the history restarts empty.
    const first = h.sessions.getTeam(SOLO_SESSION);
    const refreshed = await h.sessions.materialize(SOLO_SESSION, {
      members: [{ role: "single", preset: SOLO_PRESET }],
    });
    expect(stubs).toHaveLength(2);
    expect(stubs[0]?.dispose).toHaveBeenCalledOnce();
    expect(refreshed.createTime).toBe(first.createTime);
    expect(h.sessions.listTeamMessages(SOLO_SESSION)).toEqual([]);
    expect(refreshed.members.map((entry) => entry.role)).toEqual(["single"]);

    // A seam state error is mapped onto FAILED_PRECONDITION by its stable
    // code, never by class identity
    // (specs/080-host-main-sub-model/contracts/scene-registry.md §2).
    stubs[1]?.submit.mockImplementationOnce(() => {
      throw Object.assign(new Error("scene orchestration is disposed"), {
        code: "DISPOSED",
      });
    });
    let stateError: unknown;
    try {
      h.sessions.send(SOLO_SESSION, "again", fakeStream());
    } catch (err) {
      stateError = err;
    }
    expect(stateError).toBeInstanceOf(TeamSessionError);
    expect(stateError).toMatchObject({
      code: "FAILED_PRECONDITION",
      message: "scene orchestration is disposed",
    });
  });

  it("derives an announce-only projected member without a caller configuration", async () => {
    const solo = fakeMember("solo-agent", () => {}, () => {});
    const stubs: SceneStub[] = [];
    const scene: SceneSpec = {
      roster: ["single"],
      presetRoles: ["single"],
      rolePoolTemplate: { single: "single" },
      orchestratable: true,
      teamGoal: "",
      faceMembers: ["single", "voice"],
      createOrchestration: () => {
        const stub = sceneStub(solo.handle, [
          { role: "single", kind: "agent" },
          { role: "voice", kind: "announce-only" },
        ]);
        stubs.push(stub);
        return stub.orchestration;
      },
    };
    const h = createHarness({
      scenes: { [SOLO]: scene },
      presetRole: () => "single",
    });

    const view = await h.sessions.materialize(SOLO_SESSION, {
      members: [{ role: "single", preset: SOLO_PRESET }],
    });

    // The projection's kinds drive the runtime shapes: the announce-only
    // member carries no preset/model and needs no caller configuration
    // (specs/080-host-main-sub-model/contracts/scene-registry.md §2/§3).
    expect(view.members.map((entry) => [entry.role, entry.preset, entry.model])).toEqual([
      ["single", SOLO_PRESET, DEFAULT_MODEL],
      ["voice", "", ""],
    ]);
    expect(await h.sessions.getTeamMember(SOLO_SESSION, "voice")).toMatchObject({
      role: "voice",
      preset: "",
      model: "",
      systemPrompt: "",
    });
    // Announce-only members have no member view (kind-derived, not role-cased).
    expect(() => h.sessions.listMemberMessages(SOLO_SESSION, "voice")).toThrow(
      'team member "voice" has no message view; the member-view roster is single',
    );

    // The announce-only log production projects into the merged sequence.
    stubs[0]?.emit({
      role: "voice",
      memberId: "voice-source",
      channel: "log",
      event: {
        type: "assistant/message",
        data: { message: { content: [{ type: "text", text: "announcement" }] } },
      },
    } as unknown as TeamMemberEvent);
    expect(
      h.sessions
        .listTeamMessages(SOLO_SESSION)
        .map((entry) => [entry.member, entry.message.blocks[0]?.text?.content]),
    ).toEqual([["voice", "announcement"]]);
  });
});

describe("TeamSessions dynamic role projection (specs/080-host-main-sub-model/contracts/scene-registry.md §3)", () => {
  const SOLO = "solo";
  const SOLO_SESSION = `templates/${SOLO}/sessions/s1`;
  const SOLO_PRESET = `templates/${SOLO}/presets/p-solo`;
  const ROOM = "room-a";
  const ROOM_ONE = `${SOLO_SESSION}/room-a/1`;
  const ROOM_TWO = `${SOLO_SESSION}/room-a/2`;

  /** A synthetic single-member scene whose stub the test drives directly. */
  function createSoloHarness(): { h: Harness; lastStub: () => SceneStub } {
    const solo = fakeMember("solo-agent", () => {}, () => {});
    const stubs: SceneStub[] = [];
    const scene: SceneSpec = {
      roster: ["single"],
      presetRoles: ["single"],
      rolePoolTemplate: { single: "single" },
      orchestratable: true,
      teamGoal: "",
      faceMembers: ["single"],
      createOrchestration: () => {
        const stub = sceneStub(solo.handle, [{ role: "single", kind: "agent" }]);
        stubs.push(stub);
        return stub.orchestration;
      },
    };
    const h = createHarness({ scenes: { [SOLO]: scene }, presetRole: () => "single" });
    return {
      h,
      lastStub: () => {
        const stub = stubs[stubs.length - 1];
        if (stub === undefined) {
          throw new Error("the scene factory has not run");
        }
        return stub;
      },
    };
  }

  /** One log-channel production of a role outside the projection. */
  function roomOutput(memberId: string, text: string): TeamMemberEvent {
    return {
      role: ROOM,
      memberId,
      channel: "log",
      event: {
        type: "assistant/message",
        data: { message: { content: [{ type: "text", text }] } },
      },
    } as unknown as TeamMemberEvent;
  }

  async function materializeSolo(h: Harness): Promise<void> {
    await h.sessions.materialize(SOLO_SESSION, {
      members: [{ role: "single", preset: SOLO_PRESET }],
    });
  }

  it("projects a non-roster role per (role, memberId) with member-labelled frames and merged entries only", async () => {
    const { h, lastStub } = createSoloHarness();
    await materializeSolo(h);
    const stream = fakeStream();
    h.sessions.send(SOLO_SESSION, "hello", stream);
    await flush();

    // Two distinct producers of the SAME role: the key is (role, memberId), so
    // each gets its own collector and neither swallows the other's event.
    lastStub().emit(roomOutput(ROOM_ONE, "from room one"));
    lastStub().emit(roomOutput(ROOM_TWO, "from room two"));
    await flush();

    const entries = h.sessions.listTeamMessages(SOLO_SESSION);
    expect(
      entries.map((entry) => [entry.member, entry.message.blocks[0]?.text?.content]),
    ).toEqual([
      ["user", "hello"],
      [ROOM, "from room one"],
      [ROOM, "from room two"],
    ]);
    // A repeat envelope for an existing (role, memberId) reuses its collector:
    // no new collector is created and its production keeps appending at the
    // tail of the merge sequence.
    const roomOneKey = `${ROOM}\u0000${ROOM_ONE}`;
    const entry = entrySeam(h, SOLO_SESSION);
    const roomOneCollector = entry.dynamicCollectors.get(roomOneKey);
    expect(roomOneCollector).toBeDefined();
    lastStub().emit(roomOutput(ROOM_ONE, "from room one again"));
    await flush();

    expect(entry.dynamicCollectors.size).toBe(2);
    expect(entry.dynamicCollectors.get(roomOneKey)).toBe(roomOneCollector);
    const merged = h.sessions.listTeamMessages(SOLO_SESSION);
    expect(merged).toHaveLength(4);
    expect(merged[3]?.member).toBe(ROOM);
    expect(merged[3]?.message.blocks[0]?.text?.content).toBe("from room one again");

    // The merge entries reach the live stream as member-labelled team_message
    // frames, and the member event frames carry the envelope role.
    expect(
      stream.events
        .filter((event) => event.teamMessage?.member === ROOM)
        .map((event) => event.teamMessage?.seq),
    ).toEqual(["2", "3", "4"]);
    const roomFrames = stream.events.filter((event) => event.member === ROOM);
    expect(roomFrames.length).toBeGreaterThan(0);
    expect(roomFrames.every((event) => event.teamMessage === undefined)).toBe(true);

    // Entry-scoped per-member collectors; the role never enters the roster.
    expect(entry.members.map((member) => member.role)).toEqual(["single"]);
    expect(h.sessions.getTeam(SOLO_SESSION).members.map((member) => member.role)).toEqual([
      "single",
    ]);
    // Viewless: merged entries exist, but no member view and no member_view
    // frame was written for the dynamic role.
    expect(entry.history.listMemberMessages(ROOM)).toEqual([]);
    expect(stream.events.some((event) => event.memberView !== undefined)).toBe(false);

    // No member resource or member view is served for the dynamic role.
    await expect(
      h.sessions.getTeamMember(SOLO_SESSION, ROOM),
    ).rejects.toMatchObject({ code: "NOT_FOUND" });
    let viewError: unknown;
    try {
      h.sessions.listMemberMessages(SOLO_SESSION, ROOM);
    } catch (err) {
      viewError = err;
    }
    expect(viewError).toMatchObject({ code: "NOT_FOUND" });
  });

  it("destroys the dynamic projection with its entry generation (refresh carries nothing over)", async () => {
    const { h, lastStub } = createSoloHarness();
    await materializeSolo(h);
    const stream = fakeStream();
    h.sessions.send(SOLO_SESSION, "hello", stream);
    await flush();

    const oldStub = lastStub();
    oldStub.emit(roomOutput(ROOM_ONE, "before refresh"));
    await flush();

    const old = entrySeam(h, SOLO_SESSION);
    expect(old.dynamicCollectors.size).toBe(1);
    expect(
      old.history.listTeamMessages().map((entry) => entry.message.blocks[0]?.text?.content),
    ).toEqual(["hello", "before refresh"]);

    await materializeSolo(h);

    // The fresh generation starts with an empty dynamic map and empty
    // projections; the old generation's collectors were released with it.
    const fresh = entrySeam(h, SOLO_SESSION);
    expect(fresh.dynamicCollectors.size).toBe(0);
    expect(old.dynamicCollectors.size).toBe(0);
    expect(h.sessions.listTeamMessages(SOLO_SESSION)).toEqual([]);

    // The old generation's subscription detached with its entry: a late
    // production reaches neither history.
    oldStub.emit(roomOutput(ROOM_TWO, "after refresh"));
    await flush();
    expect(
      old.history.listTeamMessages().map((entry) => entry.message.blocks[0]?.text?.content),
    ).toEqual(["hello", "before refresh"]);
    expect(h.sessions.listTeamMessages(SOLO_SESSION)).toEqual([]);
  });
});

describe("TeamSessions face semantics for a single-member main scene (specs/080-host-main-sub-model/contracts/face-main-sub.md §1-§4)", () => {
  const SOLO = "solo";
  const SOLO_SESSION = `templates/${SOLO}/sessions/s1`;
  const SOLO_PRESET = `templates/${SOLO}/presets/p-single`;
  const SOLO_ROLE = "single";

  /**
   * A controllable single-member orchestration double. Unlike the shared
   * {@link sceneStub}, its snapshot is driven explicitly (active / activation /
   * paused / queued), and cancel/dispose converge an in-flight turn to idle —
   * the orchestration-side facts the face contract maps
   * (specs/080-host-main-sub-model/contracts/face-main-sub.md §3).
   */
  interface FaceStub {
    readonly orchestration: SceneOrchestration;
    readonly materialize: ReturnType<typeof vi.fn>;
    readonly submit: ReturnType<typeof vi.fn>;
    readonly cancel: ReturnType<typeof vi.fn>;
    readonly dispose: ReturnType<typeof vi.fn>;
    readonly state: {
      active: string | null;
      activation: string;
      paused: boolean;
      queued: number;
    };
    /** Open the single member's turn (the live running transition). */
    startTurn(): void;
  }

  function faceStub(handle: AgentHandle): FaceStub {
    const state = {
      materialized: false,
      active: null as string | null,
      activation: SOLO_ROLE,
      paused: false,
      queued: 0,
      failed: false,
    };
    const consumers = new Set<(event: TeamMemberEvent) => void>();
    const waiters: Array<() => void> = [];
    const emit = (envelope: TeamMemberEvent): void => {
      for (const consumer of [...consumers]) {
        consumer(envelope);
      }
    };
    const live = (event: TeamLiveEvent): void => {
      emit({
        role: SOLO_ROLE,
        memberId: String(handle.agent.id),
        channel: "live",
        event,
      });
    };
    const resolveQuiescent = (): void => {
      for (const resolve of waiters.splice(0)) {
        resolve();
      }
    };
    const idle = (): void => {
      // The turn converges to idle: an in-flight turn settles with the outcome
      // the host recorded (ABORTED on refresh, CANCELED on cancel)
      // (specs/080-host-main-sub-model/contracts/face-main-sub.md §1/§3).
      state.active = null;
      live({ kind: "status", status: "idle" });
    };
    const materialize = vi.fn(async () => {
      state.materialized = true;
    });
    const submit = vi.fn(() => {
      if (state.active === null) {
        state.active = SOLO_ROLE;
        return;
      }
      state.queued += 1;
    });
    const cancel = vi.fn(() => {
      if (state.active !== null) {
        idle();
      }
      state.paused = true;
      state.queued = 0;
      resolveQuiescent();
    });
    const dispose = vi.fn(async () => {
      if (state.active !== null) {
        idle();
      }
      state.materialized = false;
      resolveQuiescent();
    });
    const orchestration: SceneOrchestration = {
      materialize,
      members: (): readonly SceneMember[] =>
        state.materialized ? [{ role: SOLO_ROLE, kind: "agent" }] : [],
      member: (role) => (role === SOLO_ROLE ? handle : undefined),
      teamEvents: () => ({
        subscribe: (consumer) => {
          consumers.add(consumer);
          return () => {
            consumers.delete(consumer);
          };
        },
      }),
      snapshot: (): SceneSnapshot => ({
        materialized: state.materialized,
        active: state.active,
        activation: state.activation,
        paused: state.paused,
        queued: state.queued,
        failed: state.failed,
        lastError: null,
      }),
      submit,
      cancel,
      whenQuiescent: () =>
        new Promise<void>((resolve) => {
          waiters.push(resolve);
        }),
      dispose,
    };
    return {
      orchestration,
      materialize,
      submit,
      cancel,
      dispose,
      state,
      startTurn: () => {
        state.active = SOLO_ROLE;
        live({ kind: "status", status: "running" });
      },
    };
  }

  function createFaceHarness(): {
    h: Harness;
    stubs: FaceStub[];
    lastStub: () => FaceStub;
  } {
    const solo = fakeMember("single-agent", () => {}, () => {});
    const stubs: FaceStub[] = [];
    const scene: SceneSpec = {
      roster: [SOLO_ROLE],
      presetRoles: [SOLO_ROLE],
      rolePoolTemplate: { [SOLO_ROLE]: SOLO_ROLE },
      orchestratable: true,
      teamGoal: "",
      faceMembers: [SOLO_ROLE],
      createOrchestration: () => {
        const stub = faceStub(solo.handle);
        stubs.push(stub);
        return stub.orchestration;
      },
    };
    const h = createHarness({
      scenes: { [SOLO]: scene },
      presetRole: () => SOLO_ROLE,
    });
    return {
      h,
      stubs,
      lastStub: () => {
        const stub = stubs[stubs.length - 1];
        if (stub === undefined) {
          throw new Error("the scene factory has not run");
        }
        return stub;
      },
    };
  }

  async function materializeSolo(h: Harness): Promise<TeamView> {
    return h.sessions.materialize(SOLO_SESSION, {
      members: [{ role: SOLO_ROLE, preset: SOLO_PRESET }],
    });
  }

  it("materializes exactly the scene roster as one agent member, with no server-added member (§1 物化/§2 名册固定)", async () => {
    const { h, stubs } = createFaceHarness();

    const view = await materializeSolo(h);

    // §1 物化输入: the host hands the scene the validated neutral input — the
    // single caller member, its preset id and the split model route
    // (specs/063-llm-reliability-opencode-go/contracts/model-selection.md §3).
    expect(stubs).toHaveLength(1);
    expect(stubs[0]?.materialize).toHaveBeenCalledOnce();
    expect(stubs[0]?.materialize).toHaveBeenCalledWith({
      session: SOLO_SESSION,
      template: SOLO,
      members: [
        {
          role: SOLO_ROLE,
          preset: "p-single",
          provider: "glm-responses",
          model: DEFAULT_BARE_MODEL,
        },
      ],
    });

    // §1 物化产物 / §2 名册固定: the roster is the orchestration's member
    // projection — exactly one agent member, with no announcer or other
    // server-added member.
    expect(view.name).toBe(`${SOLO_SESSION}/team`);
    expect(
      view.members.map((entry) => [entry.name, entry.role, entry.preset, entry.model]),
    ).toEqual([
      [
        `${SOLO_SESSION}/team/members/${SOLO_ROLE}`,
        SOLO_ROLE,
        SOLO_PRESET,
        DEFAULT_MODEL,
      ],
    ]);
    expect(h.sessions.getTeam(SOLO_SESSION).members).toHaveLength(1);
    // §3 静止 = activation: the single member owns the next input.
    expect(view.activeMember).toBe(SOLO_ROLE);
    expect(h.sessions.listTeamMessages(SOLO_SESSION)).toEqual([]);

    // §2/§4: non-roster reads are NOT_FOUND, and the roster text derives from
    // the injected scene's faceMembers (no announcer literal in the host).
    await expect(
      h.sessions.getTeamMember(SOLO_SESSION, "announcer"),
    ).rejects.toMatchObject({
      code: "NOT_FOUND",
      message: 'team member "announcer" does not exist; the roster is single',
    });
    let viewError: unknown;
    try {
      h.sessions.listMemberMessages(SOLO_SESSION, "announcer");
    } catch (err) {
      viewError = err;
    }
    expect(viewError).toMatchObject({
      code: "NOT_FOUND",
      message:
        'team member "announcer" has no message view; the member-view roster is single',
    });

    // §2 名册内成功面: the roster member read serves the configured state and
    // the instance's live assembly surface, and its member view exists (empty
    // here) — distinct from the non-roster NOT_FOUND path above.
    const memberRead = await h.sessions.getTeamMember(SOLO_SESSION, SOLO_ROLE);
    expect(memberRead).toMatchObject({ role: SOLO_ROLE, preset: SOLO_PRESET });
    expect(memberRead.systemPrompt).toContain("system prompt of single-agent");
    expect(h.sessions.listMemberMessages(SOLO_SESSION, SOLO_ROLE)).toEqual([]);
  });

  it("rejects a roster shape or role outside the scene table before constructing the orchestration (§1 校验/§4 错误码与文案)", async () => {
    const { h, stubs } = createFaceHarness();

    // §1 校验: the scene roster is exactly one member — a double member is a
    // shape violation with the scene table's roster text.
    await expect(
      h.sessions.materialize(SOLO_SESSION, {
        members: [
          { role: SOLO_ROLE, preset: SOLO_PRESET },
          { role: SOLO_ROLE, preset: SOLO_PRESET },
        ],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message:
        "solo team materialization requires exactly 1 members (single), got 2",
    });

    // §1 校验: the member role must equal the scene roster role.
    await expect(
      h.sessions.materialize(SOLO_SESSION, {
        members: [{ role: "main", preset: SOLO_PRESET }],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: 'solo team member roles must be exactly {"single"}, got [main]',
    });

    // §1 校验: the preset exists and its role equals the member role.
    h.authoring.get.mockResolvedValueOnce({
      id: "p-single",
      template: "solo",
      role: "main",
      persona: "",
      createTime: new Date(0),
      updateTime: new Date(0),
    });
    await expect(
      h.sessions.materialize(SOLO_SESSION, {
        members: [{ role: SOLO_ROLE, preset: SOLO_PRESET }],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message:
        'scene check failed: preset "p-single" carries role "main" but member "single" requires the matching role',
    });

    // Fail-fast: no orchestration was constructed.
    expect(stubs).toHaveLength(0);
    expect(() => h.sessions.getTeam(SOLO_SESSION)).toThrow("team not materialized");

    // §1 失败: a validation failure leaves no half-materialized entry behind;
    // a retry with valid input materializes cleanly.
    const retried = await materializeSolo(h);
    expect(retried.members.map((entry) => entry.role)).toEqual([SOLO_ROLE]);
    expect(stubs).toHaveLength(1);
    expect(h.sessions.getTeam(SOLO_SESSION).members).toHaveLength(1);
  });

  it("serves the driving member in flight, the activation at rest, and keeps the owner across cancel (§3 active_member)", async () => {
    const { h, lastStub } = createFaceHarness();
    await materializeSolo(h);
    const stub = lastStub();
    const stream = fakeStream();

    // §3 静止: no turn is in flight; the merged value is the activation.
    expect(stub.state.active).toBeNull();
    expect(h.sessions.getTeam(SOLO_SESSION).activeMember).toBe(SOLO_ROLE);

    // §3 在途: submit opens the single member's turn; the merged value follows
    // the in-flight driving member.
    h.sessions.send(SOLO_SESSION, "hello", stream);
    await flush();
    expect(stub.submit).toHaveBeenCalledWith("hello");
    stub.startTurn();
    expect(stub.state.active).toBe(SOLO_ROLE);
    expect(h.sessions.getTeam(SOLO_SESSION).activeMember).toBe(SOLO_ROLE);

    // §3 排队: an in-flight message steers into the turn, and the accepting
    // stream's first frame carries the 1-based queue position.
    const queued = fakeStream();
    h.sessions.send(SOLO_SESSION, "second", queued);
    expect(queued.events[0]?.queued?.position).toBe(1);

    // §3 Cancel: terminate the in-flight turn (turn_end{CANCELED}), freeze, and
    // keep the owner — cancel does not change the activation.
    h.sessions.cancel(SOLO_SESSION);
    await flush();
    expect(stub.cancel).toHaveBeenCalledOnce();
    expect(stub.state.active).toBeNull();
    expect(stub.state.paused).toBe(true);
    expect(stub.state.queued).toBe(0);
    expect(h.sessions.getTeam(SOLO_SESSION).activeMember).toBe(SOLO_ROLE);
    const canceled = stream.events.filter((event) => event.turnEnd !== undefined);
    expect(canceled).toHaveLength(1);
    expect(canceled[0]?.member).toBe(SOLO_ROLE);
    expect(canceled[0]?.turnEnd?.status).toBe("TURN_STATUS_CANCELED");
    expect(stream.ended).toBe(true);
    expect(queued.ended).toBe(true);
    // Both messages stay fixed in the merged sequence (enqueue-time fixation).
    expect(
      h.sessions.listTeamMessages(SOLO_SESSION).map((entry) => entry.member),
    ).toEqual(["user", "user"]);

    // §3 Cancel is idempotent and leaves the owner unchanged.
    expect(() => h.sessions.cancel(SOLO_SESSION)).not.toThrow();
    expect(stub.cancel).toHaveBeenCalledTimes(2);
    expect(h.sessions.getTeam(SOLO_SESSION).activeMember).toBe(SOLO_ROLE);

    // §3 恢复: a Send after the frozen static point resumes the orchestration —
    // the cancel freeze is not terminal.
    const resumed = fakeStream();
    h.sessions.send(SOLO_SESSION, "resume", resumed);
    await flush();
    expect(stub.submit).toHaveBeenCalledTimes(3);
    expect(stub.state.active).toBe(SOLO_ROLE);
  });

  it("refreshes isomorphically: ABORTED in-flight turn, voided input, preserved create_time, idempotent rebuild (§1 刷新)", async () => {
    const { h, stubs, lastStub } = createFaceHarness();
    const first = await materializeSolo(h);
    const oldStub = lastStub();
    const stream = fakeStream();

    // In flight with the single member: the user input is fixed at acceptance
    // and handed to the orchestration, never consumed before the refresh.
    h.sessions.send(SOLO_SESSION, "hello", stream);
    await flush();
    oldStub.startTurn();
    expect(
      h.sessions.listTeamMessages(SOLO_SESSION).map((entry) => entry.member),
    ).toEqual(["user"]);

    const refreshed = await materializeSolo(h);

    // §1 刷新: the in-flight turn terminates as turn_end{ABORTED}; the old
    // generation is released and its stream ends at the static point.
    expect(oldStub.dispose).toHaveBeenCalledOnce();
    expect(stream.ended).toBe(true);
    const aborted = stream.events.filter((event) => event.turnEnd !== undefined);
    expect(aborted).toHaveLength(1);
    expect(aborted[0]?.member).toBe(SOLO_ROLE);
    expect(aborted[0]?.turnEnd?.status).toBe("TURN_STATUS_ABORTED");

    // §1 刷新: the unconsumed input is voided together with the old history.
    expect(h.sessions.listTeamMessages(SOLO_SESSION)).toEqual([]);
    // §1 刷新: create_time survives and the rebuilt roster is the same single
    // member, resting on the scene activation.
    expect(refreshed.createTime).toBe(first.createTime);
    expect(refreshed.members.map((entry) => entry.role)).toEqual([SOLO_ROLE]);
    expect(h.sessions.getTeam(SOLO_SESSION).activeMember).toBe(SOLO_ROLE);

    // §1 刷新: a repeated UpdateTeam with the same configuration is an
    // idempotent rebuild — same projection, create_time still preserved.
    const repeated = await materializeSolo(h);
    expect(stubs).toHaveLength(3);
    expect(stubs[1]?.dispose).toHaveBeenCalledOnce();
    expect(repeated.createTime).toBe(first.createTime);
    expect(repeated.members).toEqual(refreshed.members);
    expect(h.sessions.listTeamMessages(SOLO_SESSION)).toEqual([]);
  });
});

describe("TeamSessions sts2 scene over the real Sts2Orchestrator (specs/080-host-main-sub-model/contracts/face-main-sub.md §1-§4; sts2-loop-minimal.md §3/§6)", () => {
  const STS2_SESSION = "templates/sts2/sessions/s1";
  const STS2_PRESET = "templates/sts2/presets/p-main";
  const MAIN_ID = `${STS2_SESSION}/main`;

  /**
   * The production sts2 scene (registered in the module scene table) over the
   * harness's agent-creation and preset-compose doubles. The preset double
   * reports the `main` role the sts2 scene vocabulary requires.
   */
  function createSts2Harness(): Harness {
    return createHarness({ presetRole: () => "main" });
  }

  it("materializes the single main member through the real orchestration and drives its unified events", async () => {
    const h = createSts2Harness();
    const view = await h.sessions.materialize(STS2_SESSION, {
      members: [{ role: "main", preset: STS2_PRESET }],
    });

    // §1 物化: the host hands the validated neutral input to the sts2 factory,
    // whose orchestrator creates the agent under `${session}/main` with the
    // split model route (specs/063-llm-reliability-opencode-go/contracts/
    // model-selection.md §3).
    expect(h.agentsCreate).toHaveBeenCalledOnce();
    expect(h.agentsCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        sessionId: MAIN_ID,
        agentOptions: { provider: "glm-responses", model: DEFAULT_BARE_MODEL },
      }),
    );

    // §1 物化产物 / §2 名册固定: exactly the scene roster, no announcer or
    // other server-added member, and no team relay for the non-team scene.
    expect(
      view.members.map((entry) => [entry.name, entry.role, entry.preset, entry.model]),
    ).toEqual([
      [`${STS2_SESSION}/team/members/main`, "main", STS2_PRESET, DEFAULT_MODEL],
    ]);
    expect(view.activeMember).toBe("main");
    expect(h.teamRegister).not.toHaveBeenCalled();
    // §4: a non-roster role is not a member resource of the sts2 scene. The
    // member-name pre-check is scene-agnostic across the injected registry
    // (specs/080-host-main-sub-model/contracts/scene-registry.md §4 item 5),
    // so a role outside every scene vocabulary renders the sts2 roster text.
    await expect(
      h.sessions.getTeamMember(STS2_SESSION, "referee"),
    ).rejects.toMatchObject({
      code: "NOT_FOUND",
      message: 'team member "referee" does not exist; the roster is main',
    });

    const main = h.members.get(MAIN_ID);
    if (main === undefined) {
      throw new Error("the main agent was not created");
    }

    // §3 静止: the user message opens one turn through the real driver's
    // single-message assembly (followup) and is fixed into the merged sequence
    // at acceptance.
    const stream = fakeStream();
    h.sessions.send(STS2_SESSION, "hello", stream);
    await flush();
    expect(main.followups).toHaveLength(1);
    expect(messageText(main.followups[0]!)).toBe("hello");

    // §2/§5: the real event stream labels the main agent's log production
    // with the `(role: "main", memberId)` envelope; the host projects it
    // through the member collector and the send stream.
    main.emitEvent("session/event", main.agent.session, {
      type: "assistant/message",
      data: { message: { content: [{ type: "text", text: "hi from main" }] } },
    });
    await flush();
    expect(
      h.sessions
        .listTeamMessages(STS2_SESSION)
        .map((entry) => [entry.member, entry.message.blocks[0]?.text?.content]),
    ).toEqual([
      ["user", "hello"],
      ["main", "hi from main"],
    ]);
    expect(stream.events.some((event) => event.teamMessage?.member === "main")).toBe(
      true,
    );

    // §3 静止收束: the turn settles, the quiescence watcher ends the stream.
    main.settle();
    await flush();
    expect(stream.ended).toBe(true);
    expect(h.sessions.getTeam(STS2_SESSION).activeMember).toBe("main");
  });

  it("rejects roster and preset violations with INVALID_ARGUMENT before constructing the orchestration", async () => {
    const h = createSts2Harness();

    // §1 校验: the scene roster is exactly one member.
    await expect(
      h.sessions.materialize(STS2_SESSION, {
        members: [
          { role: "main", preset: STS2_PRESET },
          { role: "main", preset: STS2_PRESET },
        ],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: "sts2 team materialization requires exactly 1 members (main), got 2",
    });
    // §1 校验: the member role must equal the scene roster role.
    await expect(
      h.sessions.materialize(STS2_SESSION, {
        members: [{ role: "player", preset: STS2_PRESET }],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: 'sts2 team member roles must be exactly {"main"}, got [player]',
    });
    // §1 校验: the preset must exist and its role must equal the member role.
    h.authoring.get.mockResolvedValueOnce({
      id: "p-main",
      template: "player",
      role: "player",
      persona: "",
      createTime: new Date(0),
      updateTime: new Date(0),
    });
    await expect(
      h.sessions.materialize(STS2_SESSION, {
        members: [{ role: "main", preset: STS2_PRESET }],
      }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message:
        'scene check failed: preset "p-main" carries role "player" but member "main" requires the matching role',
    });

    // Fail-fast: no orchestration or agent was constructed.
    expect(h.agentsCreate).not.toHaveBeenCalled();
    expect(() => h.sessions.getTeam(STS2_SESSION)).toThrow("team not materialized");

    // §1 未物化面 / §4 错误码: Send is FAILED_PRECONDITION while unmaterialized.
    let sendError: unknown;
    try {
      h.sessions.send(STS2_SESSION, "hello", fakeStream());
    } catch (err) {
      sendError = err;
    }
    expect(sendError).toMatchObject({ code: "FAILED_PRECONDITION" });

    // A retry with valid input materializes cleanly (no half-materialized
    // state survived the failures).
    const retried = await h.sessions.materialize(STS2_SESSION, {
      members: [{ role: "main", preset: STS2_PRESET }],
    });
    expect(retried.members.map((entry) => entry.role)).toEqual(["main"]);
    expect(h.agentsCreate).toHaveBeenCalledOnce();
  });

  it("refreshes the single main member isomorphically through the real orchestration", async () => {
    const h = createSts2Harness();
    const first = await h.sessions.materialize(STS2_SESSION, {
      members: [{ role: "main", preset: STS2_PRESET }],
    });
    const oldMain = h.members.get(MAIN_ID);
    if (oldMain === undefined) {
      throw new Error("the main agent was not created");
    }

    // In flight: the input is fixed at acceptance and drives the main agent.
    const stream = fakeStream();
    h.sessions.send(STS2_SESSION, "hello", stream);
    await flush();
    expect(oldMain.followups).toHaveLength(1);
    expect(
      h.sessions.listTeamMessages(STS2_SESSION).map((entry) => entry.member),
    ).toEqual(["user"]);

    const refreshed = await h.sessions.materialize(STS2_SESSION, {
      members: [{ role: "main", preset: STS2_PRESET }],
    });

    // §1 刷新: the in-flight turn terminates as turn_end{ABORTED}, the old
    // generation is released, and its stream ends at the static point.
    expect(h.agentsCreate).toHaveBeenCalledTimes(2);
    expect(oldMain.handle.dispose).toHaveBeenCalledOnce();
    expect(stream.ended).toBe(true);
    const aborted = stream.events.filter((event) => event.turnEnd !== undefined);
    expect(aborted).toHaveLength(1);
    expect(aborted[0]?.member).toBe("main");
    expect(aborted[0]?.turnEnd?.status).toBe("TURN_STATUS_ABORTED");

    // §1 刷新: the unconsumed input is voided with the old history, create_time
    // survives, and the rebuilt roster rests on the scene activation.
    expect(h.sessions.listTeamMessages(STS2_SESSION)).toEqual([]);
    expect(refreshed.createTime).toBe(first.createTime);
    expect(refreshed.members.map((entry) => entry.role)).toEqual(["main"]);
    expect(h.sessions.getTeam(STS2_SESSION).activeMember).toBe("main");
  });
});
