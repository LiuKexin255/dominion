import { describe, expect, it, vi, beforeEach } from "vitest";
import { AgentSessions, finalResponse } from "./session.js";
import type { DshContext } from "./dsh.js";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";

/**
 * Unit tests for the conversation registry and round driver. The cordis
 * Context is a hand-rolled fake whose `on` captures listeners; tests drive
 * the dsh event sequence (running → turn/start → assistant/message →
 * turn/end → idle, https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/agent-lifecycle.md) by emitting into the captured
 * listeners — no module interception (style/javascript.md Mock convention).
 */

type Listener = (...args: never[]) => void;

interface Harness {
  ctx: DshContext;
  agentsGet: ReturnType<typeof vi.fn>;
  agentsCreate: ReturnType<typeof vi.fn>;
  fiberDispose: ReturnType<typeof vi.fn>;
  listeners: Map<string, Listener[]>;
}

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
  const fiberDispose = vi.fn(async () => {});
  const ctx = {
    on,
    agents: { get: agentsGet, create: agentsCreate },
    fiber: { dispose: fiberDispose },
  } as unknown as DshContext;
  return { ctx, agentsGet, agentsCreate, fiberDispose, listeners };
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
});

describe("AgentSessions.send", () => {
  it("creates the agent once, follows up with a user message, and returns the reply", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    const handle = fakeHandle(agent);
    harness.agentsGet.mockReturnValue(undefined);
    harness.agentsCreate.mockResolvedValue(handle);

    const sessions = new AgentSessions(harness.ctx);
    const promise = sessions.send("conv-1", "hello there");
    await driveRound(harness, agent, ["Hello! How can I help you today?"]);

    await expect(promise).resolves.toBe("Hello! How can I help you today?");
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(harness.agentsCreate).toHaveBeenCalledWith({
      sessionId: "conv-1",
      meta: { cwd: process.cwd() },
      agentOptions: { provider: "deepseek-official", model: "fake-chat-v1" },
    });
    expect(agent.followup).toHaveBeenCalledTimes(1);
    const message = (agent.followup as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(message.content).toEqual([{ type: "text", text: "hello there" }]);
    expect(message.source).toEqual({ kind: "user" });
    expect(message.role).toBe("user");
  });

  it("reuses the live agent for the same conversation without re-creating", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    const handle = fakeHandle(agent);
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(handle);

    const sessions = new AgentSessions(harness.ctx);
    const first = sessions.send("conv-1", "one");
    await driveRound(harness, agent, ["first reply"]);
    await expect(first).resolves.toBe("first reply");

    const second = sessions.send("conv-1", "two");
    await driveRound(harness, agent, ["second reply"]);
    await expect(second).resolves.toBe("second reply");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(agent.followup).toHaveBeenCalledTimes(2);
  });

  it("deduplicates concurrent first creations of the same conversation", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    const handle = fakeHandle(agent);
    harness.agentsGet.mockReturnValue(undefined);
    let resolveCreate: (value: AgentHandle) => void = () => {};
    harness.agentsCreate.mockImplementation(
      () =>
        new Promise<AgentHandle>((resolve) => {
          resolveCreate = resolve;
        }),
    );

    const sessions = new AgentSessions(harness.ctx);
    const first = sessions.send("conv-1", "a");
    const second = sessions.send("conv-1", "b");
    await flush();
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    resolveCreate(handle);
    await flush();

    // Rounds serialize per conversation: exactly one followup is in flight.
    expect(agent.followup).toHaveBeenCalledTimes(1);
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, assistantEvent("reply a"));
    emit(harness, "agent/status", { agent, status: "idle" });
    await expect(first).resolves.toBe("reply a");

    await driveRound(harness, agent, ["reply b"]);
    await expect(second).resolves.toBe("reply b");
    expect(agent.followup).toHaveBeenCalledTimes(2);
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });

  it("re-creates a session whose registry entry went stale", async () => {
    const harness = createHarness();
    const stale = fakeAgent("conv-1");
    const fresh = fakeAgent("conv-1");
    harness.agentsCreate.mockResolvedValueOnce(fakeHandle(stale)).mockResolvedValueOnce(fakeHandle(fresh));
    // Miss on the first send; a different live agent on the second (loop reload).
    harness.agentsGet.mockReturnValueOnce(undefined).mockReturnValueOnce(fresh);

    const sessions = new AgentSessions(harness.ctx);
    const first = sessions.send("conv-1", "one");
    await driveRound(harness, stale, ["stale reply"]);
    await expect(first).resolves.toBe("stale reply");

    const second = sessions.send("conv-1", "two");
    await driveRound(harness, fresh, ["fresh reply"]);
    await expect(second).resolves.toBe("fresh reply");
    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
  });

  it("returns the LAST assistant message when a round produced several", async () => {
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
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
    const promise = sessions.send("conv-1", "hello");
    await driveRound(harness, agent, []);

    await expect(promise).resolves.toBe("");
  });

  it("rejects a failed round but keeps the session reusable and the process alive", async () => {
    // Edge case: fake-llm unreachable — the round fails (mapped to INTERNAL /
    // HTTP 500 upstream), the conversation stays registered, and the next
    // round succeeds on the same agent. Registry lookups confirm the live
    // agent so the entry is reused instead of re-created (the first send
    // misses on the sessions map, not on agents.get).
    const harness = createHarness();
    const agent = fakeAgent("conv-1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
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
    const roundA = sessions.send("conv-a", "a");
    await driveRound(harness, agentA, ["ra"]);
    await roundA;
    const roundB = sessions.send("conv-b", "b");
    await driveRound(harness, agentB, ["rb"]);
    await roundB;

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
