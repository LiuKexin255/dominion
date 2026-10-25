/**
 * Sts2Orchestrator tests (specs/080-host-main-sub-model/contracts/
 * sts2-loop-minimal.md §3/§6): materialization parameters, whole-attempt
 * rollback, the member projection, snapshot projection, the single-agent
 * drive lifecycle as seen through the seam, the state-error codes and the
 * release ordering.
 *
 * The collaborators are constructor-injected DI doubles (`style/javascript.md`
 * Mock convention: `vi.fn()` recording, no module interception). The agent
 * double is hand-driven the same way as the driver tests', plus the session
 * event face the envelope subscribes.
 */

import type {
  Agent,
  AgentCancelCause,
  AgentHandle,
  CreateAgentOptions,
} from "@deepseek-ai/dsh-agent";
import type { Context } from "@deepseek-ai/cordis";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import type { TeamMemberEvent } from "@dominion/dsh-team";
import { describe, expect, it, vi } from "vitest";

import { Sts2Orchestrator, Sts2StateError } from "./orchestrator.js";
import type {
  AgentCreationSeam,
  ComposePreset,
  ComposedPreset,
  OrchestratorLogger,
  Sts2MaterializeInput,
} from "./orchestrator.js";

const SESSION = "templates/sts2/sessions/s1";
const SESSION_ID = `${SESSION}/main`;
const AGENT_CTX = { name: "agent-ctx" } as unknown as Context;

/** One materialization input with the given main-member configuration. */
function input(
  member: Omit<Sts2MaterializeInput["members"][number], "role"> = {
    preset: "main-preset",
  },
): Sts2MaterializeInput {
  return {
    session: SESSION,
    template: "sts2",
    members: [{ role: "main", ...member }],
  };
}

function textOf(message: UserMessage): string {
  const block = message.content[0];
  return block !== undefined && block.type === "text" ? block.text : "";
}

function textsOf(messages: readonly UserMessage[]): string[] {
  return messages.map(textOf);
}

/** An engine failure shaped like a harness error (stable `code` + message). */
function engineError(code: string, text = "engine failed"): Error {
  return Object.assign(new Error(text), { code });
}

type Listener = (...args: unknown[]) => void;

/**
 * The hand-driven agent/handle double: `whenIdle` resolves on {@link settle},
 * `followup`/`inject` start the "running" status the official loop reports,
 * and `ctx.on` records every subscription the driver and the envelope make.
 */
function createFakeAgent(sessionId: string) {
  let status: "idle" | "running" = "idle";
  const idleWaiters: Array<() => void> = [];
  const listeners = new Map<string, Set<Listener>>();
  const injections: UserMessage[] = [];
  const followups: UserMessage[] = [];
  const steers: UserMessage[] = [];
  const cancelCauses: AgentCancelCause[] = [];
  const nextStep: UserMessage[] = [];
  const nextTurn: UserMessage[] = [];

  const inject = vi.fn((message: UserMessage) => {
    injections.push(message);
    nextStep.push(message);
    status = "running";
  });
  const followup = vi.fn((message: UserMessage) => {
    followups.push(message);
    nextTurn.push(message);
    status = "running";
  });
  const steer = vi.fn((message: UserMessage) => {
    steers.push(message);
    nextStep.push(message);
  });
  const cancel = vi.fn((cause: AgentCancelCause) => {
    cancelCauses.push(cause);
    // The real cancel clears the inbox unless keepInbox is set.
    nextStep.length = 0;
    nextTurn.length = 0;
  });

  const ctx = {
    on(name: string, listener: Listener) {
      let set = listeners.get(name);
      if (set === undefined) {
        set = new Set();
        listeners.set(name, set);
      }
      set.add(listener);
      return () => {
        set.delete(listener);
      };
    },
    logger: { warn: vi.fn() },
  };

  const agent = {
    id: sessionId,
    get status() {
      return status;
    },
    ctx,
    inbox: {
      get nextStep() {
        return nextStep;
      },
      get nextTurn() {
        return nextTurn;
      },
      remove: (messageId: UserMessage["id"]) => {
        for (const list of [nextStep, nextTurn]) {
          const index = list.findIndex(
            (message) => String(message.id) === String(messageId),
          );
          if (index >= 0) {
            list.splice(index, 1);
            return true;
          }
        }
        return false;
      },
    },
    whenIdle: () => new Promise<void>((resolve) => idleWaiters.push(resolve)),
    inject,
    followup,
    steer,
    cancel,
  } as unknown as Agent;

  const dispose = vi.fn(async () => {});
  const handle = { agent, dispose } as unknown as AgentHandle;

  return {
    agent,
    handle,
    dispose,
    injections,
    followups,
    steers,
    cancelCauses,
    nextStep,
    nextTurn,
    inject,
    followup,
    steer,
    cancel,
    /** Publish one turn failure (the real boundary always precedes idle). */
    fail(error: unknown): void {
      for (const listener of [...(listeners.get("agent/error") ?? [])]) {
        listener({ agent, error });
      }
    },
    /** Publish one session log production of this agent. */
    emitSessionEvent(event: unknown): void {
      for (const listener of [...(listeners.get("session/event") ?? [])]) {
        listener({ id: sessionId }, event);
      }
    },
    listenerCount(name: string): number {
      return listeners.get(name)?.size ?? 0;
    },
    resetCalls(): void {
      injections.length = 0;
      followups.length = 0;
      steers.length = 0;
      cancelCauses.length = 0;
      inject.mockClear();
      followup.mockClear();
      steer.mockClear();
      cancel.mockClear();
    },
    /** Complete the in-flight turn. */
    settle(): void {
      status = "idle";
      for (const resolve of idleWaiters.splice(0)) {
        resolve();
      }
    },
  };
}

type FakeAgent = ReturnType<typeof createFakeAgent>;

interface HarnessOptions {
  readonly compose?: ComposePreset;
  /** Return `"throw"` to fail the corresponding create attempt. */
  readonly create?: (attempt: number) => "ok" | "throw";
  readonly logger?: OrchestratorLogger;
  readonly provider?: string;
}

/** Build the orchestrator over DI doubles, recording every creation attempt. */
function createHarness(options: HarnessOptions = {}) {
  const created: CreateAgentOptions[] = [];
  const handles: FakeAgent[] = [];
  const presetSetup = vi.fn(async (_agentCtx: Context): Promise<void> => {});
  const compose = vi.fn(
    options.compose ??
      (async (preset: string): Promise<ComposedPreset> => ({
        agentPreset: `templates/sts2/presets/${preset}`,
        setup: presetSetup,
      })),
  );
  let createAttempt = 0;
  const create = vi.fn(
    async (creation: CreateAgentOptions): Promise<AgentHandle> => {
      created.push(creation);
      createAttempt += 1;
      if (options.create?.(createAttempt) === "throw") {
        throw new Error("agent creation failed");
      }
      const fake = createFakeAgent(String(creation.sessionId));
      handles.push(fake);
      await creation.setup?.(AGENT_CTX);
      return fake.handle;
    },
  );
  const orchestrator = new Sts2Orchestrator(
    {} as Context,
    {
      compose,
      agents: { create } as AgentCreationSeam,
      ...(options.logger === undefined ? {} : { logger: options.logger }),
      ...(options.provider === undefined ? {} : { provider: options.provider }),
    },
  );
  return {
    orchestrator,
    compose,
    create,
    created,
    handles,
    presetSetup,
    fake: (): FakeAgent => {
      const fake = handles[0];
      if (fake === undefined) {
        throw new Error("no agent was created");
      }
      return fake;
    },
  };
}

/** The unmaterialized snapshot every fresh orchestrator projects. */
const UNMATERIALIZED_SNAPSHOT = {
  materialized: false,
  active: null,
  activation: "main",
  paused: false,
  queued: 0,
  failed: false,
  lastError: null,
};

/** Discards the failure reports of the rollback tests. */
const SILENT_LOGGER: OrchestratorLogger = { error: vi.fn() };

describe("Sts2Orchestrator.materialize", () => {
  it("composes the preset, creates the main agent and subscribes the envelope", async () => {
    // Given a harness with the deployment provider.
    const harness = createHarness({ provider: "deployment-route" });

    // When the single main member is materialized.
    await harness.orchestrator.materialize(input({ preset: "main-preset" }));

    // Then the creation binds the session id, the composed declaration and the
    // deployment route.
    expect(harness.compose).toHaveBeenCalledWith("main-preset");
    expect(harness.create).toHaveBeenCalledTimes(1);
    const creation = harness.created[0];
    expect(creation?.sessionId).toBe(SESSION_ID);
    expect(creation?.meta).toEqual({
      cwd: process.cwd(),
      agentPreset: "templates/sts2/presets/main-preset",
    });
    expect(creation?.agentOptions).toEqual({ provider: "deployment-route" });
    expect(harness.presetSetup).toHaveBeenCalledWith(AGENT_CTX);

    // And the member projection reports the single agent member.
    const fake = harness.fake();
    expect(harness.orchestrator.members()).toEqual([
      { role: "main", kind: "agent" },
    ]);
    expect(harness.orchestrator.member("main")).toBe(fake.handle);
    expect(harness.orchestrator.member("other")).toBeUndefined();
    expect(harness.orchestrator.snapshot()).toEqual({
      materialized: true,
      active: null,
      activation: "main",
      paused: false,
      queued: 0,
      failed: false,
      lastError: null,
    });
  });

  it("prefers the member's own provider/model route over the deployment route", async () => {
    const harness = createHarness({ provider: "deployment-route" });

    await harness.orchestrator.materialize(
      input({ preset: "p", provider: "member-route", model: "member-model" }),
    );

    expect(harness.created[0]?.agentOptions).toEqual({
      provider: "member-route",
      model: "member-model",
    });
  });

  it("subscribes the main envelope on the created agent", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();
    const received: TeamMemberEvent[] = [];
    harness.orchestrator.teamEvents().subscribe((event) => received.push(event));
    const event = { type: "assistant/message", seq: 1, time: 1, data: {} };

    fake.emitSessionEvent(event);

    expect(received).toEqual([
      { role: "main", memberId: SESSION_ID, channel: "log", event },
    ]);
  });

  it.each([
    ["two members", [input().members[0], input().members[0]]],
    ["a non-main role", [{ role: "other", preset: "p" }]],
    ["an empty preset", [{ role: "main", preset: "" }]],
  ] as const)("rejects %s before any creation", async (_label, members) => {
    const harness = createHarness();

    await expect(
      harness.orchestrator.materialize({ ...input(), members }),
    ).rejects.toThrow(/sts2-loop materialization requires/);

    expect(harness.compose).not.toHaveBeenCalled();
    expect(harness.create).not.toHaveBeenCalled();
    expect(harness.orchestrator.members()).toEqual([]);
    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);
  });

  it("rolls a compose failure back to unmaterialized and retries", async () => {
    // Given a compose seam that fails its first call only.
    let attempts = 0;
    const compose: ComposePreset = async (preset) => {
      attempts += 1;
      if (attempts === 1) {
        throw new Error("compose failed");
      }
      return {
        agentPreset: `templates/sts2/presets/${preset}`,
        setup: vi.fn(),
      };
    };
    const harness = createHarness({ compose, logger: SILENT_LOGGER });

    // When the first materialization fails.
    await expect(harness.orchestrator.materialize(input())).rejects.toThrow(
      "compose failed",
    );

    // Then nothing was created and the orchestrator is unmaterialized.
    expect(harness.create).not.toHaveBeenCalled();
    expect(harness.orchestrator.members()).toEqual([]);
    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);

    // And a retry succeeds.
    await harness.orchestrator.materialize(input());
    expect(harness.orchestrator.members()).toEqual([
      { role: "main", kind: "agent" },
    ]);
  });

  it("rolls an agent-creation failure back to unmaterialized and retries", async () => {
    const harness = createHarness({
      create: (attempt) => (attempt === 1 ? "throw" : "ok"),
      logger: SILENT_LOGGER,
    });

    await expect(harness.orchestrator.materialize(input())).rejects.toThrow(
      "agent creation failed",
    );

    expect(harness.create).toHaveBeenCalledTimes(1);
    expect(harness.orchestrator.members()).toEqual([]);
    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);

    await harness.orchestrator.materialize(input());
    expect(harness.orchestrator.members()).toEqual([
      { role: "main", kind: "agent" },
    ]);
  });

  it("rolls a preset setup failure back to unmaterialized and retries", async () => {
    // Given a composed preset whose scope mount fails its first attempt only.
    let attempts = 0;
    const compose: ComposePreset = async (preset) => ({
      agentPreset: `templates/sts2/presets/${preset}`,
      async setup() {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("setup failed");
        }
      },
    });
    const harness = createHarness({ compose, logger: SILENT_LOGGER });

    await expect(harness.orchestrator.materialize(input())).rejects.toThrow(
      "setup failed",
    );

    // The factory-owned rollback leaves nothing reachable through the seam.
    expect(harness.orchestrator.members()).toEqual([]);
    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);

    await harness.orchestrator.materialize(input());
    expect(harness.orchestrator.members()).toEqual([
      { role: "main", kind: "agent" },
    ]);
  });

  it("rejects a second materialization", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());

    await expect(harness.orchestrator.materialize(input())).rejects.toMatchObject(
      { name: "Sts2StateError", code: "ALREADY_MATERIALIZED" },
    );
  });
});

describe("Sts2Orchestrator state guards", () => {
  it("projects the unmaterialized snapshot and rejects the stateful faces", async () => {
    const harness = createHarness();

    // Then the snapshot reports the resting main activation and no member.
    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);
    expect(harness.orchestrator.members()).toEqual([]);
    expect(harness.orchestrator.member("main")).toBeUndefined();

    // And every stateful face throws a same-coded state error.
    expect(() => harness.orchestrator.submit("hello")).toThrow(Sts2StateError);
    expect(() => harness.orchestrator.submit("hello")).toThrow(/not materialized/);
    expect(() => harness.orchestrator.teamEvents()).toThrow(Sts2StateError);
    expect(() => harness.orchestrator.cancel()).toThrow(Sts2StateError);
    expect(harness.compose).not.toHaveBeenCalled();
    await expect(harness.orchestrator.whenQuiescent()).resolves.toBeUndefined();
  });

  it("rejects every face with DISPOSED after dispose", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    await harness.orchestrator.dispose();

    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);
    expect(harness.orchestrator.members()).toEqual([]);
    for (const call of [
      () => harness.orchestrator.submit("hello"),
      () => harness.orchestrator.teamEvents(),
      () => harness.orchestrator.materialize(input()),
    ]) {
      let caught: unknown;
      try {
        await call();
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeInstanceOf(Sts2StateError);
      expect((caught as Sts2StateError).code).toBe("DISPOSED");
    }
    // Cancel is the idempotent exception: a disposed session has nothing left
    // to freeze, so it stays a no-op.
    expect(() => harness.orchestrator.cancel()).not.toThrow();
  });
});

describe("Sts2Orchestrator drive lifecycle", () => {
  it("opens a turn at rest, steers in flight and settles back to quiescence", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();

    // When the user submits at rest.
    harness.orchestrator.submit("hello");

    // Then the single message opens the turn and the snapshot reports the
    // in-flight main member.
    expect(textsOf(fake.followups)).toEqual(["hello"]);
    expect(textsOf(fake.injections)).toEqual([]);
    expect(harness.orchestrator.snapshot()).toMatchObject({
      active: "main",
      queued: 0,
    });

    // When a second message arrives in flight, it steers and counts as queued.
    harness.orchestrator.submit("steered");
    expect(textsOf(fake.steers)).toEqual(["steered"]);
    expect(harness.orchestrator.snapshot()).toMatchObject({
      active: "main",
      queued: 1,
    });

    // Then the turn's settlement returns the orchestration to its resting
    // state after whenQuiescent resolves.
    const quiescent = harness.orchestrator.whenQuiescent();
    fake.settle();
    await quiescent;
    expect(harness.orchestrator.snapshot()).toMatchObject({
      active: null,
      queued: 0,
      paused: false,
      failed: false,
    });
  });

  it("cancels the in-flight turn, freezes, and replays the held input", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();
    harness.orchestrator.submit("first");
    harness.orchestrator.submit("steered");

    // When the caller cancels mid-turn.
    harness.orchestrator.cancel();

    // Then the agent receives the user cause and the snapshot freezes.
    expect(fake.cancelCauses).toEqual([{ kind: "user" }]);
    expect(harness.orchestrator.snapshot().paused).toBe(true);
    const quiescent = harness.orchestrator.whenQuiescent();
    fake.settle();
    await quiescent;
    expect(harness.orchestrator.snapshot()).toMatchObject({
      active: null,
      paused: true,
      failed: false,
    });

    // When the user delivers again, the held steering is replayed first.
    fake.resetCalls();
    harness.orchestrator.submit("resume");
    expect(textsOf(fake.injections)).toEqual(["steered"]);
    expect(textsOf(fake.followups)).toEqual(["resume"]);
    expect(harness.orchestrator.snapshot().paused).toBe(false);
  });

  it("freezes a quiescent driver without touching the agent", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();

    harness.orchestrator.cancel();

    expect(fake.cancel).not.toHaveBeenCalled();
    expect(harness.orchestrator.snapshot()).toMatchObject({
      active: null,
      paused: true,
    });
  });

  it("projects a failed turn into the snapshot and reports it once", async () => {
    const logger: OrchestratorLogger = { error: vi.fn() };
    const harness = createHarness({ logger });
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();
    harness.orchestrator.submit("first");

    // When the turn fails with a transient engine error.
    fake.fail(engineError("RATE_LIMIT", "rate limited"));
    const quiescent = harness.orchestrator.whenQuiescent();
    fake.settle();
    await quiescent;

    // Then the snapshot carries the classified failure under member-turn and
    // the injected logger reports it exactly once.
    expect(harness.orchestrator.snapshot()).toEqual({
      materialized: true,
      active: null,
      activation: "main",
      paused: true,
      queued: 0,
      failed: true,
      lastError: {
        message: "rate limited",
        member: "main",
        code: "TRANSIENT",
        origin: "member-turn",
        detail: { engineCode: "RATE_LIMIT", message: "rate limited" },
      },
    });
    expect(logger.error).toHaveBeenCalledTimes(1);
    expect(logger.error).toHaveBeenCalledWith(
      expect.stringContaining("turn failed"),
      expect.objectContaining({
        session: SESSION,
        member: "main",
        code: "TRANSIENT",
        error: "rate limited",
      }),
    );

    // And the next successful delivery clears the failure fact.
    harness.orchestrator.submit("retry");
    const recovered = harness.orchestrator.whenQuiescent();
    fake.settle();
    await recovered;
    expect(harness.orchestrator.snapshot()).toMatchObject({
      failed: false,
      lastError: null,
    });
  });
});

describe("Sts2Orchestrator.dispose", () => {
  it("converges the in-flight turn before releasing the agent and subscriptions", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();
    harness.orchestrator.submit("first");
    expect(fake.listenerCount("session/event")).toBe(1);

    // When dispose starts while the turn is in flight.
    const disposing = harness.orchestrator.dispose();

    // Then the turn is cancelled with the teardown cause but the agent is not
    // released until it converges.
    expect(fake.cancelCauses).toEqual([{ kind: "disposed" }]);
    expect(fake.dispose).not.toHaveBeenCalled();
    fake.settle();
    await disposing;

    // Then the agent and every subscription are released, and the projection
    // is unmaterialized.
    expect(fake.dispose).toHaveBeenCalledTimes(1);
    expect(fake.listenerCount("session/event")).toBe(0);
    expect(fake.listenerCount("agent/error")).toBe(0);
    expect(harness.orchestrator.members()).toEqual([]);
    expect(harness.orchestrator.snapshot()).toEqual(UNMATERIALIZED_SNAPSHOT);

    // And dispose is idempotent.
    await harness.orchestrator.dispose();
    expect(fake.dispose).toHaveBeenCalledTimes(1);
  });

  it("releases a quiescent agent without cancelling it", async () => {
    const harness = createHarness();
    await harness.orchestrator.materialize(input());
    const fake = harness.fake();

    await harness.orchestrator.dispose();

    expect(fake.cancel).not.toHaveBeenCalled();
    expect(fake.dispose).toHaveBeenCalledTimes(1);
  });
});

describe("Sts2Orchestrator seam compatibility", () => {
  it("accepts the host's scene-neutral logger and projects a host-shaped snapshot", async () => {
    // The host passes its `SceneLogger`: a method taking an opaque record. The
    // package logger must accept it structurally (the package never imports
    // the host), so this assignment is the compile-time check.
    const hostLogger = {
      error(_message: string, _context: Readonly<Record<string, unknown>>): void {},
    };
    const compose: ComposePreset = async () => ({
      agentPreset: "templates/sts2/presets/main-preset",
      setup: vi.fn(),
    });
    const orchestrator = new Sts2Orchestrator({} as Context, {
      compose,
      logger: hostLogger,
    });

    // The host's `SceneSnapshot` shape, structurally restated here.
    const snapshot: {
      materialized: boolean;
      active: string | null;
      activation: string;
      paused: boolean;
      queued: number;
      failed: boolean;
      lastError: {
        message: string;
        member: string | null;
        code: string;
        origin: "member-turn" | "orchestration";
        detail: { engineCode?: string; message: string };
      } | null;
    } = orchestrator.snapshot();

    expect(snapshot).toEqual(UNMATERIALIZED_SNAPSHOT);
  });
});
