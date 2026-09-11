import { describe, expect, it, vi, beforeEach } from "vitest";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import {
  AgentSessions,
  ConversationNotCreatedError,
  finalResponse,
} from "./session.js";
import type { DshContext } from "./dsh.js";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";

/**
 * Unit tests for the explicit conversation registry and round driver. The
 * cordis Context is a hand-rolled fake whose `on` captures listeners; tests
 * drive the dsh event sequence (running → turn/start → assistant/message →
 * turn/end → idle, https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/agent-lifecycle.md) by emitting into the captured
 * listeners — no module interception (style/javascript.md Mock convention).
 *
 * The preset composition resolves through the `presetAuthoring` seam
 * (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2): the
 * harness injects a `vi.fn()` compose double, so the create/rebuild
 * semantics (R4) — and the mandatory-preset rejection for an absent id — are
 * asserted against the calls the service makes.
 */

type Listener = (...args: never[]) => void;

interface Harness {
  ctx: DshContext;
  agentsGet: ReturnType<typeof vi.fn>;
  agentsCreate: ReturnType<typeof vi.fn>;
  compose: ReturnType<typeof vi.fn>;
  fiberDispose: ReturnType<typeof vi.fn>;
  listeners: Map<string, Listener[]>;
}

/** The setup hook compose() returns; asserted by identity on agentsCreate. */
const standardSetup = vi.fn(async () => {});
const toolsSetup = vi.fn(async () => {});

function fakeAgent(id: string) {
  return {
    id,
    session: { id },
    followup: vi.fn(),
    whenIdle: vi.fn(async () => {}),
  } as unknown as Agent;
}

function fakeHandle(agent: Agent): AgentHandle {
  return { agent, dispose: vi.fn(async () => {}) };
}

function createHarness(): Harness {
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
  const agentsGet = vi.fn();
  const agentsCreate = vi.fn();
  const compose = vi.fn(async (presetId?: string) => {
    if (presetId === undefined) {
      // Preset selection is mandatory (060 preset derivation): the real
      // plugin rejects an id-less compose INVALID_ARGUMENT before anything
      // resolves, so the double mirrors that boundary.
      throw new PresetAuthoringError(
        "INVALID_ARGUMENT",
        "preset id is required: this deployment configures no default preset",
      );
    }
    return presetId === "demo-tools"
      ? { agentPreset: presetId, setup: toolsSetup }
      : { agentPreset: presetId, setup: standardSetup };
  });
  const fiberDispose = vi.fn(async () => {});
  const ctx = {
    on,
    get: vi.fn(() => ({ compose })),
    agents: { get: agentsGet, create: agentsCreate },
    fiber: { dispose: fiberDispose },
  } as unknown as DshContext;
  return { ctx, agentsGet, agentsCreate, compose, fiberDispose, listeners };
}

function emit(harness: Harness, name: string, ...args: unknown[]): void {
  for (const listener of [...(harness.listeners.get(name) ?? [])]) {
    (listener as (...emitArgs: unknown[]) => void)(...args);
  }
}

function assistantEvent(text: string, turn = 1) {
  return {
    type: "assistant/message",
    data: { message: { content: [{ type: "text", text }] } },
    seq: turn,
    time: 0,
  };
}

/** Drain the microtask queue so pending creations and round chains settle. */
async function flush(times = 20): Promise<void> {
  for (let i = 0; i < times; i++) {
    await Promise.resolve();
  }
}

/** Drive one full successful round on the agent. */
async function driveRound(harness: Harness, agent: Agent, replies: string[]): Promise<void> {
  await flush();
  emit(harness, "agent/status", { agent, status: "running" });
  emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });
  for (const reply of replies) {
    emit(harness, "session/event", agent.session, assistantEvent(reply));
  }
  emit(harness, "session/event", agent.session, { type: "turn/end", data: { turn: 1, reason: { kind: "completed" } } });
  emit(harness, "agent/status", { agent, status: "idle" });
}

beforeEach(() => {
  vi.restoreAllMocks();
  standardSetup.mockClear();
  toolsSetup.mockClear();
});

describe("AgentSessions.create", () => {
  it("composes the preset and records the resolved id in the creation meta (V1-3)", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(undefined);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const view = await sessions.create("conv-1", "demo-tools");

    expect(harness.compose).toHaveBeenCalledWith("demo-tools");
    // The meta is the session header source: the resolved preset id rides
    // into it alongside cwd, and the compose setup hook is the factory's
    // mount hook (specs/058-dsh-preset-roster-demo/research.md R10).
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    const options = harness.agentsCreate.mock.calls[0][0] as Record<string, unknown>;
    expect(options.meta).toEqual({ cwd: process.cwd(), agentPreset: "demo-tools" });
    expect(options.setup).toBe(toolsSetup);
    expect(view).toEqual({
      name: "conversations/conv-1",
      preset: "demo-tools",
      createTime: expect.any(Date),
    });
  });

  it("rejects a create without a preset (preset selection is mandatory)", async () => {
    const harness = createHarness();
    harness.agentsGet.mockReturnValue(undefined);

    const sessions = new AgentSessions(harness.ctx);

    await expect(sessions.create("conv-1")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });
    expect(harness.compose).toHaveBeenCalledWith(undefined);
    // The rejection precedes any agent creation.
    expect(harness.agentsCreate).not.toHaveBeenCalled();
  });

  it("is idempotent for the same conversation id and same preset", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    const handle = fakeHandle(agent);
    harness.agentsCreate.mockResolvedValue(handle);

    const sessions = new AgentSessions(harness.ctx);
    const first = await sessions.create("conv-1", "demo-tools");
    const second = await sessions.create("conv-1", "demo-tools");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(handle.dispose).not.toHaveBeenCalled();
    expect(second).toEqual(first);
  });

  it("rebuilds on a different preset after disposing the old agent (R4)", async () => {
    const harness = createHarness();
    const standard = fakeAgent("conv-1");
    const tools = fakeAgent("conv-1");
    const standardHandle = fakeHandle(standard);
    harness.agentsGet.mockReturnValue(standard);
    harness.agentsCreate.mockResolvedValueOnce(standardHandle).mockResolvedValueOnce(fakeHandle(tools));

    const sessions = new AgentSessions(harness.ctx);
    const first = await sessions.create("conv-1", "demo-standard");
    const second = await sessions.create("conv-1", "demo-tools");

    expect(standardHandle.dispose).toHaveBeenCalledTimes(1);
    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
    const secondOptions = harness.agentsCreate.mock.calls[1][0] as Record<string, unknown>;
    expect(secondOptions.meta).toEqual({ cwd: process.cwd(), agentPreset: "demo-tools" });
    expect(second.preset).toBe("demo-tools");
    expect(second.preset).not.toBe(first.preset);
  });

  it("serializes concurrent creates into one composition (dedup)", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    // The live registry holds the created agent, so the second create's
    // idempotency check sees a live entry instead of a stale one.
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const [first, second] = await Promise.all([
      sessions.create("conv-1", "demo-tools"),
      sessions.create("conv-1", "demo-tools"),
    ]);

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(second).toEqual(first);
  });

  it("surfaces a compose rejection without creating any agent", async () => {
    const harness = createHarness();
    harness.agentsGet.mockReturnValue(undefined);
    harness.compose.mockRejectedValue(
      Object.assign(new Error('unknown preset "nope"; available: demo-standard, demo-tools'), {
        name: "PresetAuthoringError",
      }),
    );

    const sessions = new AgentSessions(harness.ctx);
    await expect(sessions.create("conv-1", "nope")).rejects.toThrow("unknown preset");
    expect(harness.agentsCreate).not.toHaveBeenCalled();
  });
});

describe("AgentSessions.send", () => {
  it("follows up on the created conversation's agent and returns the reply", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-tools");
    const promise = sessions.send("conv-1", "hello there");
    await driveRound(harness, agent, ["Hello! How can I help you today?"]);

    await expect(promise).resolves.toBe("Hello! How can I help you today?");
    expect(agent.followup).toHaveBeenCalledTimes(1);
    const message = (agent.followup as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(message.content).toEqual([{ type: "text", text: "hello there" }]);
    expect(message.source).toEqual({ kind: "user" });
    expect(message.role).toBe("user");
  });

  it("reuses the created agent for every round without re-creating", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-standard");
    const first = sessions.send("conv-1", "one");
    await driveRound(harness, agent, ["first reply"]);
    await expect(first).resolves.toBe("first reply");

    const second = sessions.send("conv-1", "two");
    await driveRound(harness, agent, ["second reply"]);
    await expect(second).resolves.toBe("second reply");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(agent.followup).toHaveBeenCalledTimes(2);
  });

  it("rejects a send on a conversation that was never created (FR-002)", async () => {
    const harness = createHarness();

    const sessions = new AgentSessions(harness.ctx);
    await expect(sessions.send("never-created", "hello")).rejects.toBeInstanceOf(
      ConversationNotCreatedError,
    );
    await expect(sessions.send("never-created", "hello")).rejects.toThrow(
      "conversation never-created not created; call CreateConversation first",
    );
  });

  it("counts a session whose registry entry went stale as not created", async () => {
    const harness = createHarness();
    const stale = fakeAgent("conv-1");
    harness.agentsCreate.mockResolvedValue(fakeHandle(stale));
    // The live registry never holds the agent (loop-level reload disposed
    // it behind our record) — create() registers without a registry check,
    // so the miss only surfaces at send time.
    harness.agentsGet.mockReturnValue(undefined);

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-standard");

    await expect(sessions.send("conv-1", "hello")).rejects.toBeInstanceOf(
      ConversationNotCreatedError,
    );
    // The stale record was evicted; no agent is re-created lazily.
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });

  it("fails an in-flight round when the conversation is rebuilt (R4)", async () => {
    const harness = createHarness();
    const standard = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(standard);
    const standardHandle = fakeHandle(standard);
    harness.agentsCreate
      .mockResolvedValueOnce(standardHandle)
      .mockResolvedValueOnce(fakeHandle(fakeAgent("conv-1")));

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-standard");
    const round = sessions.send("conv-1", "hello");
    await flush();
    emit(harness, "agent/status", { agent: standard, status: "running" });

    // Rebuild while the round is in flight: dispose emits agent/disposed,
    // which must settle the round as a failure instead of hanging on idle.
    const rebuild = sessions.create("conv-1", "demo-tools");
    await flush();
    emit(harness, "agent/disposed", { agent: standard });
    await expect(round).rejects.toThrow("disposed mid-round");
    await rebuild;
    expect(standardHandle.dispose).toHaveBeenCalledTimes(1);
  });

  it("returns the LAST assistant message when a round produced several", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-standard");
    const promise = sessions.send("conv-1", "hello");
    await driveRound(harness, agent, ["draft one", "draft two", "final answer"]);

    await expect(promise).resolves.toBe("final answer");
  });

  it("resolves with the empty string when the round has no assistant message", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-standard");
    const promise = sessions.send("conv-1", "hello");
    await driveRound(harness, agent, []);

    await expect(promise).resolves.toBe("");
  });

  it("rejects a failed round but keeps the conversation reusable and the process alive", async () => {
    // Edge case: fake-llm unreachable — the round fails (mapped to INTERNAL /
    // HTTP 500 upstream), the conversation stays registered, and the next
    // round succeeds on the same agent.
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-1", "demo-standard");
    const failed = sessions.send("conv-1", "hello");
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    const boom = new Error("fake-llm unreachable (TRANSPORT)");
    emit(harness, "agent/error", { agent, turn: 1, step: 1, error: boom });
    emit(
      harness,
      "session/event",
      agent.session,
      { type: "turn/end", data: { turn: 1, reason: { kind: "error", error: { code: "TRANSPORT" } } } },
    );
    emit(harness, "agent/status", { agent, status: "idle" });
    await expect(failed).rejects.toThrow("fake-llm unreachable");

    const recovered = sessions.send("conv-1", "hello again");
    await driveRound(harness, agent, ["back online"]);
    await expect(recovered).resolves.toBe("back online");
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });
});

describe("AgentSessions.shutdown", () => {
  it("disposes every agent handle and then the root fiber", async () => {
    const harness = createHarness();
    const agentA = fakeAgent("conv-a");
    const agentB = fakeAgent("conv-b");
    const handleA = fakeHandle(agentA);
    const handleB = fakeHandle(agentB);
    harness.agentsGet.mockReturnValue(undefined);
    harness.agentsCreate.mockResolvedValueOnce(handleA).mockResolvedValueOnce(handleB);

    const sessions = new AgentSessions(harness.ctx);
    await sessions.create("conv-a", "demo-standard");
    await sessions.create("conv-b", "demo-tools");

    await sessions.shutdown();

    expect(handleA.dispose).toHaveBeenCalledTimes(1);
    expect(handleB.dispose).toHaveBeenCalledTimes(1);
    expect(harness.fiberDispose).toHaveBeenCalledTimes(1);
  });
});

describe("finalResponse", () => {
  it("concatenates the text blocks of the last assistant message", () => {
    const events = [
      { type: "turn/start", data: { turn: 1 } },
      assistantEvent("first"),
      assistantEvent("part one, "),
      {
        type: "assistant/message",
        data: { message: { content: [{ type: "text", text: "part one, " }, { type: "reasoning", text: "hidden" }, { type: "text", text: "part two" }] } },
      },
    ];
    expect(finalResponse(events)).toBe("part one, part two");
  });

  it("returns the empty string when no assistant message exists", () => {
    expect(
      finalResponse([{ type: "turn/start" }, { type: "turn/end", data: { turn: 1, reason: { kind: "completed" } } }]),
    ).toBe("");
  });
});
