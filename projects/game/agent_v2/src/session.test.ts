import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentSessions } from "./session.js";
import type { TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";

/**
 * Unit tests for the session registry, per-session FIFO queue, and dispose
 * semantics (specs/049-agent-v2-dsh-init/data-model.md §2.2/§2.3;
 * contracts/conversation-api.md §3 event-order invariants). The cordis
 * Context is a hand-rolled fake whose `on` captures listeners; tests drive
 * the dsh event sequence (running → turn/start → assistant/chunk →
 * assistant/message → turn/end → idle,
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/agent-lifecycle.md)
 * by emitting into the captured listeners — no module interception
 * (style/javascript.md Mock convention).
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

interface StreamRecorder extends TurnStream {
  events: ChatEvent[];
  ended: boolean;
}

function fakeStream(): StreamRecorder {
  const recorder = {
    events: [] as ChatEvent[],
    ended: false,
    write: (event: ChatEvent) => {
      recorder.events.push(event);
    },
    end: () => {
      recorder.ended = true;
    },
  };
  return recorder;
}

function chunk(chunkData: Record<string, unknown>) {
  return { type: "assistant/chunk", data: { turn: 1, step: 1, chunk: chunkData } };
}

function assistantMessage(blocks: Array<Record<string, unknown>>, usage?: Record<string, number>) {
  return {
    type: "assistant/message",
    data: { turn: 1, step: 1, message: { content: blocks }, ...(usage ? { usage } : {}) },
  };
}

/**
 * Payload discriminator (the proto-loader virtual oneof field only exists on
 * runtime-decoded messages; handlers construct plain objects, so detect the
 * populated oneof arm by field presence).
 */
function payloadOf(event: ChatEvent): string {
  if (event.queued !== undefined) return "queued";
  if (event.turnStart !== undefined) return "turnStart";
  if (event.blockStart !== undefined) return "blockStart";
  if (event.delta !== undefined) return "delta";
  if (event.blockEnd !== undefined) return "blockEnd";
  if (event.turnEnd !== undefined) return "turnEnd";
  return "";
}

/** Drain the microtask queue so pending creations and turn chains settle. */
async function flush(times = 20): Promise<void> {
  for (let i = 0; i < times; i++) {
    await Promise.resolve();
  }
}

/**
 * Drive one full successful turn (text block with two deltas) for `agent`
 * and settle it via the idle transition.
 */
async function driveTurn(harness: Harness, agent: Agent, text: string): Promise<void> {
  await flush();
  emit(harness, "agent/status", { agent, status: "running" });
  emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });
  emit(harness, "session/event", agent.session, chunk({ type: "block-start", index: 0, blockType: "text" }));
  emit(harness, "session/event", agent.session, chunk({ type: "text-delta", index: 0, text }));
  emit(harness, "session/event", agent.session, chunk({ type: "block-end", index: 0, block: { type: "text", text } }));
  emit(harness, "session/event", agent.session, chunk({ type: "usage", usage: { inputTokens: 3, outputTokens: 5 } }));
  emit(harness, "session/event", agent.session, assistantMessage([{ type: "text", text }]));
  emit(harness, "session/event", agent.session, { type: "turn/end", data: { turn: 1, reason: { kind: "completed" } } });
  emit(harness, "agent/status", { agent, status: "idle" });
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("AgentSessions.send", () => {
  it("streams mapped block events and closes with turn_end{COMPLETED} carrying usage", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(undefined);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const stream = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "hello", stream);
    await driveTurn(harness, agent, "Hi there!");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(harness.agentsCreate).toHaveBeenCalledWith({
      sessionId: "templates/saolei/sessions/s1",
      agentOptions: { provider: "glm-responses", model: "glm-5.2" },
    });

    const payloads = stream.events.map(payloadOf);
    // The turn's first frame is turn_start (contract §3-2), then the mapped
    // block frames, then the single terminal turn_end.
    expect(payloads).toEqual(["turnStart", "blockStart", "delta", "blockEnd", "turnEnd"]);
    expect(payloads[0]).toBe("turnStart");
    expect(stream.events[0]?.turnId).toBe(stream.events[4]?.turnId);
    expect(stream.events[2]?.delta?.text).toBe("Hi there!");
    expect(stream.events[4]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
    expect(stream.events[4]?.turnEnd?.usage?.inputTokens).toBe("3");
    expect(stream.ended).toBe(true);

    const followup = (agent.followup as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(followup.content).toEqual([{ type: "text", text: "hello" }]);
    expect(followup.source).toEqual({ kind: "user" });
  });

  it("reuses the live agent for the same session without re-creating", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const first = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "one", first);
    await driveTurn(harness, agent, "first reply");

    const second = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "two", second);
    await driveTurn(harness, agent, "second reply");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(agent.followup).toHaveBeenCalledTimes(2);
    expect(first.events).toHaveLength(5);
    expect(second.events).toHaveLength(5);
  });

  it("queues a mid-turn send with queued{position} and auto-runs it at turn end", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const first = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "first", first);

    // Turn one is mid-flight (no idle yet): the second send must enqueue.
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    const second = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "second", second);
    await flush();

    expect(second.events.map(payloadOf)).toEqual(["queued"]);
    expect(second.events[0]?.queued?.position).toBe(1);
    expect(second.ended).toBe(false);
    expect(agent.followup).toHaveBeenCalledTimes(1);

    // Settling turn one auto-runs the queued message on the same stream.
    emit(harness, "session/event", agent.session, assistantMessage([{ type: "text", text: "reply one" }]));
    emit(harness, "session/event", agent.session, { type: "turn/end", data: { turn: 1, reason: { kind: "completed" } } });
    emit(harness, "agent/status", { agent, status: "idle" });
    await flush();

    expect(agent.followup).toHaveBeenCalledTimes(2);
    const secondFollowup = (agent.followup as ReturnType<typeof vi.fn>).mock.calls[1][0];
    expect(secondFollowup.content).toEqual([{ type: "text", text: "second" }]);
    expect(first.events.map(payloadOf)).toEqual(["turnStart", "turnEnd"]);
    expect(first.ended).toBe(true);

    await driveTurn(harness, agent, "reply two");
    const secondPayloads = second.events.map(payloadOf);
    // The queued stream: queued → (takeover) turn_start first → block frames
    // → turn_end; the queued frame and the turn share one minted turn id.
    expect(secondPayloads).toEqual(["queued", "turnStart", "blockStart", "delta", "blockEnd", "turnEnd"]);
    expect(secondPayloads[1]).toBe("turnStart");
    expect(second.events.every((event) => event.turnId === second.events[0]?.turnId)).toBe(true);
    expect(second.ended).toBe(true);
  });

  it("keeps distinct sessions independent: concurrent turns do not block each other", async () => {
    const harness = createHarness();
    const agentA = fakeAgent("templates/saolei/sessions/a");
    const agentB = fakeAgent("templates/saolei/sessions/b");
    harness.agentsCreate.mockResolvedValueOnce(fakeHandle(agentA)).mockResolvedValueOnce(fakeHandle(agentB));
    harness.agentsGet.mockReturnValue(undefined);

    const sessions = new AgentSessions(harness.ctx);
    const streamA = fakeStream();
    const streamB = fakeStream();
    sessions.send("templates/saolei/sessions/a", "to a", streamA);
    sessions.send("templates/saolei/sessions/b", "to b", streamB);

    await driveTurn(harness, agentA, "reply a");
    expect(streamA.events.map(payloadOf)).toEqual(["turnStart", "blockStart", "delta", "blockEnd", "turnEnd"]);
    // B's stream holds only the host-emitted turn_start: its turn is open
    // but no dsh event has arrived yet — the sessions are independent.
    expect(streamB.events.map(payloadOf)).toEqual(["turnStart"]);

    await driveTurn(harness, agentB, "reply b");
    expect(streamB.events.map(payloadOf)).toEqual(["turnStart", "blockStart", "delta", "blockEnd", "turnEnd"]);
    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
  });

  it("maps a model failure to turn_end{ERROR} and keeps the session reusable", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const failed = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "hello", failed);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    const boom = { message: "fake-llm unreachable", code: "GLM_TRANSPORT" };
    emit(harness, "agent/error", { agent, turn: 1, step: 1, error: boom });
    emit(harness, "session/event", agent.session, {
      type: "turn/end",
      data: { turn: 1, reason: { kind: "error", error: boom } },
    });
    emit(harness, "agent/status", { agent, status: "idle" });
    await flush();

    expect(failed.events.map(payloadOf)).toEqual(["turnStart", "turnEnd"]);
    const end = failed.events[failed.events.length - 1]?.turnEnd;
    expect(end?.status).toBe("TURN_STATUS_ERROR");
    expect(end?.error?.code).toBe("GLM_TRANSPORT");
    expect(end?.error?.message).toBe("fake-llm unreachable");
    expect(failed.ended).toBe(true);

    // Edge case recovery: the next turn on the same session succeeds.
    const recovered = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "hello again", recovered);
    await driveTurn(harness, agent, "back online");
    expect(recovered.events[recovered.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });

  it("appends user messages to history at enqueue time (also for queued ones)", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const first = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "first", first);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    const second = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "second", second);
    await flush();

    const history = await sessions.listMessages("templates/saolei/sessions/s1");
    expect(history.map((message) => message.role)).toEqual(["ROLE_USER", "ROLE_USER"]);
    expect(history[0]?.blocks[0]?.text?.content).toBe("first");
    expect(history[1]?.blocks[0]?.text?.content).toBe("second");
  });

  it("returns agent replies in history from assistant/message finality", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const stream = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "hello", stream);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });
    emit(harness, "session/event", agent.session, assistantMessage([
      { type: "reasoning", text: "thinking" },
      { type: "text", text: "answer" },
    ], { inputTokens: 1, outputTokens: 2 }));
    emit(harness, "session/event", agent.session, { type: "turn/end", data: { turn: 1, reason: { kind: "completed" } } });
    emit(harness, "agent/status", { agent, status: "idle" });
    await flush();

    const history = await sessions.listMessages("templates/saolei/sessions/s1");
    expect(history.map((message) => message.role)).toEqual(["ROLE_USER", "ROLE_AGENT"]);
    const agentBlocks = history[1]?.blocks ?? [];
    expect(agentBlocks[0]?.think?.content).toBe("thinking");
    expect(agentBlocks[1]?.text?.content).toBe("answer");
    expect(history[0]?.messageId).toBeDefined();
    expect(history[1]?.messageId).not.toBe(history[0]?.messageId);
  });
});

describe("AgentSessions.dispose", () => {
  it("aborts the in-flight turn, drops queued messages, and releases the agent", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    const handle = fakeHandle(agent);
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(handle);

    const sessions = new AgentSessions(harness.ctx);
    const inFlight = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "in flight", inFlight);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    const queued = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "queued", queued);
    await flush();
    expect(queued.events.map(payloadOf)).toEqual(["queued"]);

    await sessions.dispose("templates/saolei/sessions/s1");

    expect(inFlight.ended).toBe(true);
    const inFlightEnd = inFlight.events[inFlight.events.length - 1]?.turnEnd;
    expect(inFlightEnd?.status).toBe("TURN_STATUS_ABORTED");
    expect(queued.ended).toBe(true);
    const queuedEnd = queued.events[queued.events.length - 1]?.turnEnd;
    expect(queuedEnd?.status).toBe("TURN_STATUS_ABORTED");
    expect(handle.dispose).toHaveBeenCalledTimes(1);

    // The disposed session's history is no longer queryable: re-asking
    // get-or-creates a fresh entry with an empty record (data-model.md §2.2).
    const historyAfter = await sessions.listMessages("templates/saolei/sessions/s1");
    expect(historyAfter).toEqual([]);
    expect(handle.dispose).toHaveBeenCalledTimes(1);
  });

  it("is idempotent for an absent session", async () => {
    const harness = createHarness();
    const sessions = new AgentSessions(harness.ctx);
    await expect(sessions.dispose("templates/saolei/sessions/absent")).resolves.toBeUndefined();
    expect(harness.agentsCreate).not.toHaveBeenCalled();
  });

  it("yields a brand-new session (no stale state) for the same resource name", async () => {
    const harness = createHarness();
    const first = fakeAgent("templates/saolei/sessions/s1");
    const second = fakeAgent("templates/saolei/sessions/s1");
    const handleFirst = fakeHandle(first);
    const handleSecond = fakeHandle(second);
    // Only consulted on registry hits (staleness re-validation): both
    // post-dispose lookups should find the fresh agent live.
    harness.agentsGet.mockReturnValue(second);
    harness.agentsCreate.mockResolvedValueOnce(handleFirst).mockResolvedValueOnce(handleSecond);

    const sessions = new AgentSessions(harness.ctx);
    const streamOne = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "before dispose", streamOne);
    await driveTurn(harness, first, "old reply");
    expect(streamOne.ended).toBe(true);
    const framesBeforeDispose = streamOne.events.length;

    // The turn already settled (idle): dispose must not append a spurious
    // turn_end{ABORTED} to the completed stream.
    await sessions.dispose("templates/saolei/sessions/s1");
    expect(streamOne.events).toHaveLength(framesBeforeDispose);
    expect(streamOne.events[streamOne.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");

    const streamTwo = fakeStream();
    sessions.send("templates/saolei/sessions/s1", "after dispose", streamTwo);
    await driveTurn(harness, second, "new reply");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
    const history = await sessions.listMessages("templates/saolei/sessions/s1");
    expect(history.map((message) => message.blocks[0]?.text?.content)).toEqual([
      "after dispose",
      "new reply",
    ]);
  });

  it("aborts in-flight turns during shutdown and disposes the fiber last", async () => {
    const harness = createHarness();
    const agentA = fakeAgent("templates/saolei/sessions/a");
    const agentB = fakeAgent("templates/saolei/sessions/b");
    const handleA = fakeHandle(agentA);
    const handleB = fakeHandle(agentB);
    harness.agentsCreate.mockResolvedValueOnce(handleA).mockResolvedValueOnce(handleB);
    harness.agentsGet.mockReturnValue(undefined);

    const sessions = new AgentSessions(harness.ctx);
    const streamA = fakeStream();
    const streamB = fakeStream();
    sessions.send("templates/saolei/sessions/a", "a", streamA);
    await driveTurn(harness, agentA, "reply a");
    sessions.send("templates/saolei/sessions/b", "b", streamB);
    await driveTurn(harness, agentB, "reply b");

    const order: string[] = [];
    (handleA.dispose as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      order.push("dispose a");
    });
    (handleB.dispose as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      order.push("dispose b");
    });
    harness.fiberDispose.mockImplementation(async () => {
      order.push("fiber");
    });

    await sessions.shutdown();
    expect(order).toEqual(["dispose a", "dispose b", "fiber"]);
    expect(streamA.ended && streamB.ended).toBe(true);
  });
});

describe("AgentSessions.listMessages", () => {
  it("returns an empty history for a fresh session (get-or-create)", async () => {
    const harness = createHarness();
    const agent = fakeAgent("templates/saolei/sessions/s1");
    harness.agentsGet.mockReturnValue(undefined);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const sessions = new AgentSessions(harness.ctx);
    const history = await sessions.listMessages("templates/saolei/sessions/s1");
    expect(history).toEqual([]);
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });
});
