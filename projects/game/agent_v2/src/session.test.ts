import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentSessionError, AgentSessions } from "./session.js";
import type { TurnStream } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";

/**
 * Unit tests for the materialization registry, per-session FIFO queue, and
 * the UpdateAgent semantics (specs/051-agent-v2-dsh-migration/data-model.md
 * §2.2/§2.3; contracts/conversation-api.md §3 event-order invariants).
 * Materialization is explicit — Send has no lazy creation and fails
 * FAILED_PRECONDITION before any frame. The cordis Context is a hand-rolled
 * fake whose `on` captures listeners; tests drive the dsh event sequence
 * (running → turn/start → assistant/chunk → assistant/message → turn/end →
 * idle, https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/
 * agent-lifecycle.md) by emitting into the captured listeners — no module
 * interception (style/javascript.md Mock convention).
 */

type Listener = (...args: never[]) => void;

interface Harness {
  ctx: DshContext;
  sessions: AgentSessions;
  agentsGet: ReturnType<typeof vi.fn>;
  agentsCreate: ReturnType<typeof vi.fn>;
  fiberDispose: ReturnType<typeof vi.fn>;
  listeners: Map<string, Listener[]>;
}

const S1 = "templates/saolei/sessions/s1";
const P1 = "templates/saolei/presets/p1";

function fakeAgent(id: string) {
  return {
    id,
    session: { id },
    followup: vi.fn(),
    whenIdle: vi.fn(async () => {}),
    cancel: vi.fn(),
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
  const sessions = new AgentSessions(ctx);
  return { ctx, sessions, agentsGet, agentsCreate, fiberDispose, listeners };
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
  if (event.toolResult !== undefined) return "toolResult";
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

/**
 * Materialize a session on the harness and leave the registry lookup
 * pointing at the created agent (live-entry re-validation).
 */
async function materializeSession(
  harness: Harness,
  session: string,
  agent: Agent,
  options: { preset?: string; model?: string; persona?: string } = {},
): Promise<unknown> {
  harness.agentsGet.mockReturnValue(agent);
  const handle = fakeHandle(agent);
  harness.agentsCreate.mockResolvedValueOnce(handle);
  const view = await harness.sessions.materialize(session, {
    preset: options.preset ?? P1,
    ...(options.model === undefined ? {} : { model: options.model }),
    persona: options.persona ?? "player persona",
  });
  await flush();
  return view;
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("AgentSessions.materialize", () => {
  it("creates the dsh agent with the provider, model, and persona snapshot", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    const view = (await harness.sessions.materialize(S1, {
      preset: P1,
      model: "glm-5.5",
      persona: "careful player",
    })) as { name: string; preset: string; model: string; createTime: Date; updateTime: Date };

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(harness.agentsCreate).toHaveBeenCalledWith({
      sessionId: S1,
      agentOptions: { provider: "glm-responses", model: "glm-5.5", persona: "careful player" },
    });
    expect(view.name).toBe(`${S1}/agent`);
    expect(view.preset).toBe(P1);
    expect(view.model).toBe("glm-5.5");
    expect(view.createTime).toBeInstanceOf(Date);
    expect(view.updateTime).toBeInstanceOf(Date);
  });

  it("falls back to the process default model when none is given", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    harness.agentsGet.mockReturnValue(agent);
    harness.agentsCreate.mockResolvedValue(fakeHandle(agent));

    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });

    expect(harness.agentsCreate).toHaveBeenCalledWith({
      sessionId: S1,
      agentOptions: { provider: "glm-responses", model: "glm-5.2", persona: "p" },
    });
  });

  it("re-materializing tears down the old agent and yields a clean one (refresh folded in)", async () => {
    const harness = createHarness();
    const first = fakeAgent(S1);
    const second = fakeAgent(S1);
    const firstHandle = fakeHandle(first);
    const secondHandle = fakeHandle(second);
    harness.agentsCreate.mockResolvedValueOnce(firstHandle).mockResolvedValueOnce(secondHandle);
    harness.agentsGet.mockReturnValue(first);

    await harness.sessions.materialize(S1, { preset: P1, persona: "old persona" });
    const stream = fakeStream();
    harness.sessions.send(S1, "first message", stream);
    await flush();
    emit(harness, "agent/status", { agent: first, status: "running" });
    emit(harness, "session/event", first.session, { type: "turn/start", data: { turn: 1 } });
    expect(stream.ended).toBe(false);

    // Re-materialize with an unchanged configuration: the in-flight turn is
    // aborted and the agent is disposed and rebuilt regardless (refresh).
    const view = (await harness.sessions.materialize(S1, {
      preset: P1,
      persona: "old persona",
    })) as { preset: string };
    expect(view.preset).toBe(P1);
    expect(firstHandle.dispose).toHaveBeenCalledTimes(1);
    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
    expect(harness.agentsCreate).toHaveBeenLastCalledWith({
      sessionId: S1,
      agentOptions: { provider: "glm-responses", model: "glm-5.2", persona: "old persona" },
    });

    // The in-flight stream received exactly one terminal ABORTED frame.
    expect(stream.ended).toBe(true);
    expect(stream.events.map(payloadOf)).toEqual(["turnStart", "turnEnd"]);
    expect(stream.events[stream.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_ABORTED");

    // The new entry is clean: history restarts empty.
    harness.agentsGet.mockReturnValue(second);
    expect(await harness.sessions.listMessages(S1)).toEqual([]);
  });

  it("aborts queued messages when re-materializing mid-queue", async () => {
    const harness = createHarness();
    const first = fakeAgent(S1);
    const second = fakeAgent(S1);
    const firstHandle = fakeHandle(first);
    harness.agentsCreate
      .mockResolvedValueOnce(firstHandle)
      .mockResolvedValueOnce(fakeHandle(second));
    harness.agentsGet.mockReturnValue(first);

    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });
    const inFlight = fakeStream();
    harness.sessions.send(S1, "in flight", inFlight);
    await flush();
    emit(harness, "agent/status", { agent: first, status: "running" });
    emit(harness, "session/event", first.session, { type: "turn/start", data: { turn: 1 } });

    const queued = fakeStream();
    harness.sessions.send(S1, "queued", queued);
    await flush();
    expect(queued.events.map(payloadOf)).toEqual(["queued"]);

    harness.agentsGet.mockReturnValue(second);
    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });

    expect(queued.ended).toBe(true);
    expect(queued.events[queued.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_ABORTED");
    expect(firstHandle.dispose).toHaveBeenCalledTimes(1);
  });

  it("preserves create_time across re-materialization and refreshes update_time", async () => {
    const harness = createHarness();
    const first = fakeAgent(S1);
    const second = fakeAgent(S1);
    harness.agentsCreate
      .mockResolvedValueOnce(fakeHandle(first))
      .mockResolvedValueOnce(fakeHandle(second));
    harness.agentsGet.mockReturnValue(first);

    const firstView = (await harness.sessions.materialize(S1, {
      preset: P1,
      persona: "p",
    })) as { createTime: Date; updateTime: Date };
    harness.agentsGet.mockReturnValue(second);
    const secondView = (await harness.sessions.materialize(S1, {
      preset: P1,
      persona: "p",
    })) as { createTime: Date; updateTime: Date };

    expect(secondView.createTime).toBe(firstView.createTime);
    expect(secondView.updateTime.getTime()).toBeGreaterThanOrEqual(firstView.updateTime.getTime());
  });

  it("keeps the settled stream's terminal frame and EOF across a re-materialization", async () => {
    const harness = createHarness();
    const first = fakeAgent(S1);
    const second = fakeAgent(S1);
    harness.agentsCreate
      .mockResolvedValueOnce(fakeHandle(first))
      .mockResolvedValueOnce(fakeHandle(second));
    harness.agentsGet.mockReturnValue(first);

    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });
    const stream = fakeStream();
    harness.sessions.send(S1, "hello", stream);
    await flush();
    emit(harness, "agent/status", { agent: first, status: "running" });
    emit(harness, "session/event", first.session, { type: "turn/start", data: { turn: 1 } });
    emit(harness, "session/event", first.session, assistantMessage([{ type: "text", text: "reply" }]));
    emit(harness, "session/event", first.session, { type: "turn/end", data: { turn: 1, reason: { kind: "completed" } } });
    emit(harness, "agent/status", { agent: first, status: "idle" });
    // Behavioral assertion: once the idle transition has settled the turn, a
    // re-materialization must leave the client stream with its terminal
    // turn_end{COMPLETED} frame AND the EOF. The narrower interleaving that
    // motivated the unconditional end() in runTurn's finally (teardown
    // landing between the terminal frame and the finally) is not
    // constructible in this harness — the runner always drains before the
    // teardown's microtasks here — and stays a known test blind spot; the
    // unconditional end() itself is the guard.
    harness.agentsGet.mockReturnValue(second);
    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });
    await flush();

    const payloads = stream.events.map(payloadOf);
    expect(payloads).toEqual(["turnStart", "turnEnd"]);
    expect(stream.events[payloads.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
    expect(stream.ended).toBe(true);
  });

  it("serializes concurrent materializations of one session", async () => {
    const harness = createHarness();
    const first = fakeAgent(S1);
    const second = fakeAgent(S1);
    const firstHandle = fakeHandle(first);
    const secondHandle = fakeHandle(second);
    let resolveCreate: ((handle: AgentHandle) => void) | undefined;
    harness.agentsCreate.mockImplementation(
      () =>
        new Promise<AgentHandle>((resolve) => {
          resolveCreate = resolve;
        }),
    );

    const jobOne = harness.sessions.materialize(S1, { preset: P1, persona: "one" });
    await flush();
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);

    // The second materialization joins the per-session chain: no second
    // create fires while the first one is still in flight.
    const jobTwo = harness.sessions.materialize(S1, { preset: P1, persona: "two" });
    await flush();
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);

    resolveCreate?.(firstHandle);
    await flush();
    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
    resolveCreate?.(secondHandle);
    await Promise.all([jobOne, jobTwo]);

    expect(harness.agentsCreate).toHaveBeenCalledTimes(2);
    expect(secondHandle.dispose).not.toHaveBeenCalled();
  });
});

describe("AgentSessions.send", () => {
  it("streams mapped block events and closes with turn_end{COMPLETED} carrying usage", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const stream = fakeStream();
    harness.sessions.send(S1, "hello", stream);
    await driveTurn(harness, agent, "Hi there!");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
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

  it("rejects an unmaterialized session with FAILED_PRECONDITION before any frame", () => {
    const harness = createHarness();

    const stream = fakeStream();
    expect(() => harness.sessions.send(S1, "hello", stream)).toThrow(AgentSessionError);
    try {
      harness.sessions.send(S1, "hello", stream);
    } catch (err) {
      expect((err as AgentSessionError).code).toBe("FAILED_PRECONDITION");
      expect((err as Error).message).toContain("UpdateAgent");
    }
    expect(stream.events).toEqual([]);
    expect(stream.ended).toBe(false);
    expect(harness.agentsCreate).not.toHaveBeenCalled();
  });

  it("treats a stale entry (agent gone from the registry) as unmaterialized", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    // The loop-level reload disposed the agent behind our record.
    harness.agentsGet.mockReturnValue(undefined);
    expect(() => harness.sessions.send(S1, "hello", fakeStream())).toThrow(AgentSessionError);
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });

  it("reuses the live agent for the same session without re-creating", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const first = fakeStream();
    harness.sessions.send(S1, "one", first);
    await driveTurn(harness, agent, "first reply");

    const second = fakeStream();
    harness.sessions.send(S1, "two", second);
    await driveTurn(harness, agent, "second reply");

    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
    expect(agent.followup).toHaveBeenCalledTimes(2);
    expect(first.events).toHaveLength(5);
    expect(second.events).toHaveLength(5);
  });

  it("queues a mid-turn send with queued{position} and auto-runs it at turn end", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const first = fakeStream();
    harness.sessions.send(S1, "first", first);

    // Turn one is mid-flight (no idle yet): the second send must enqueue.
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    const second = fakeStream();
    harness.sessions.send(S1, "second", second);
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
    await materializeSession(harness, "templates/saolei/sessions/a", agentA);
    await materializeSession(harness, "templates/saolei/sessions/b", agentB);
    // live-entry re-validation resolves each session to its own agent.
    harness.agentsGet.mockImplementation((id: unknown) =>
      id === "templates/saolei/sessions/a" ? agentA : agentB,
    );

    const streamA = fakeStream();
    const streamB = fakeStream();
    harness.sessions.send("templates/saolei/sessions/a", "to a", streamA);
    harness.sessions.send("templates/saolei/sessions/b", "to b", streamB);

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
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const failed = fakeStream();
    harness.sessions.send(S1, "hello", failed);
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
    harness.sessions.send(S1, "hello again", recovered);
    await driveTurn(harness, agent, "back online");
    expect(recovered.events[recovered.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });

  it("appends user messages to history at enqueue time (also for queued ones)", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const first = fakeStream();
    harness.sessions.send(S1, "first", first);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    const second = fakeStream();
    harness.sessions.send(S1, "second", second);
    await flush();

    const history = await harness.sessions.listMessages(S1);
    expect(history.map((message) => message.role)).toEqual(["ROLE_USER", "ROLE_USER"]);
    expect(history[0]?.blocks[0]?.text?.content).toBe("first");
    expect(history[1]?.blocks[0]?.text?.content).toBe("second");
  });

  it("returns agent replies in history from assistant/message finality", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const stream = fakeStream();
    harness.sessions.send(S1, "hello", stream);
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

    const history = await harness.sessions.listMessages(S1);
    expect(history.map((message) => message.role)).toEqual(["ROLE_USER", "ROLE_AGENT"]);
    const agentBlocks = history[1]?.blocks ?? [];
    expect(agentBlocks[0]?.think?.content).toBe("thinking");
    expect(agentBlocks[1]?.text?.content).toBe("answer");
    expect(history[0]?.messageId).toBeDefined();
    expect(history[1]?.messageId).not.toBe(history[0]?.messageId);
  });
});

describe("AgentSessions.getAgent / listMessages on unmaterialized sessions", () => {
  it("answers NOT_FOUND for GetAgent and ListAgentMessages and never creates", async () => {
    const harness = createHarness();

    expect(() => harness.sessions.getAgent(S1)).toThrow(AgentSessionError);
    try {
      harness.sessions.getAgent(S1);
    } catch (err) {
      expect((err as AgentSessionError).code).toBe("NOT_FOUND");
    }
    await expect(harness.sessions.listMessages(S1)).rejects.toMatchObject({ code: "NOT_FOUND" });
    expect(harness.agentsCreate).not.toHaveBeenCalled();
  });

  it("returns the materialized configuration from getAgent", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent, { preset: P1, model: "glm-5.5" });

    const view = harness.sessions.getAgent(S1) as { name: string; preset: string; model: string };
    expect(view.name).toBe(`${S1}/agent`);
    expect(view.preset).toBe(P1);
    expect(view.model).toBe("glm-5.5");
  });
});

describe("AgentSessions.cancel", () => {
  it("cancels the in-flight turn: turn_end{CANCELED} lands synchronously (SC-004 ≤5s) and late dsh events are not forwarded", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const stream = fakeStream();
    harness.sessions.send(S1, "hello", stream);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });
    emit(harness, "session/event", agent.session, chunk({ type: "block-start", index: 0, blockType: "text" }));
    emit(harness, "session/event", agent.session, chunk({ type: "text-delta", index: 0, text: "partial" }));

    // SC-004 (终止后回合停止 ≤5 秒): the CANCELED terminal frame is written
    // synchronously by the cancel call itself — no wait, let alone five
    // seconds, passes before the stream holds its terminal frame.
    harness.sessions.cancel(S1);
    expect(stream.ended).toBe(true);
    expect(stream.events.map(payloadOf)).toEqual(["turnStart", "blockStart", "delta", "turnEnd"]);
    expect(stream.events[stream.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_CANCELED");
    // Cancellation propagates to the dsh driver (LLM stream + in-flight tools).
    expect(agent.cancel).toHaveBeenCalledWith({ kind: "user" });

    // The collector settled before Agent.cancel: the driver's cancellation
    // converges to an idle status and late chunks must not re-open the slot.
    await flush();
    emit(harness, "session/event", agent.session, chunk({ type: "text-delta", index: 0, text: "late" }));
    emit(harness, "agent/status", { agent, status: "idle" });
    await flush();
    expect(stream.events.map(payloadOf)).toEqual(["turnStart", "blockStart", "delta", "turnEnd"]);
  });

  it("lands queued messages on cancel: queue cleared without a turn, history keeps the user messages", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const inFlight = fakeStream();
    harness.sessions.send(S1, "in flight", inFlight);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    const queued = fakeStream();
    harness.sessions.send(S1, "queued", queued);
    await flush();
    expect(queued.events.map(payloadOf)).toEqual(["queued"]);

    harness.sessions.cancel(S1);
    await flush();

    // The queued stream learns its message will not run through the existing
    // turn_end vocabulary and closes; no followup fires for it.
    expect(queued.ended).toBe(true);
    expect(queued.events.map(payloadOf)).toEqual(["queued", "turnEnd"]);
    expect(queued.events[queued.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_CANCELED");
    expect(agent.followup).toHaveBeenCalledTimes(1);

    // Landing: the user messages (in-flight + queued) stay in history —
    // enqueue-time appendUser already fixed them (data-model.md §3).
    const history = await harness.sessions.listMessages(S1);
    expect(history.map((message) => message.role)).toEqual(["ROLE_USER", "ROLE_USER"]);
    expect(history[1]?.blocks[0]?.text?.content).toBe("queued");
  });

  it("is a no-op success with no in-flight turn and an empty queue", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    expect(() => harness.sessions.cancel(S1)).not.toThrow();
    expect(agent.cancel).not.toHaveBeenCalled();
    expect(agent.followup).not.toHaveBeenCalled();
    expect((await harness.sessions.listMessages(S1)).map((m) => m.role)).toEqual([]);
  });

  it("accepts a new Send immediately after a cancel (no cooldown, direct turn start)", async () => {
    const harness = createHarness();
    const agent = fakeAgent(S1);
    await materializeSession(harness, S1, agent);

    const first = fakeStream();
    harness.sessions.send(S1, "first", first);
    await flush();
    emit(harness, "agent/status", { agent, status: "running" });
    emit(harness, "session/event", agent.session, { type: "turn/start", data: { turn: 1 } });

    harness.sessions.cancel(S1);
    await flush();

    // busy was reset: the next send starts a turn directly instead of
    // receiving a queued frame.
    const next = fakeStream();
    harness.sessions.send(S1, "next", next);
    await flush();
    expect(next.events.map(payloadOf)).toEqual(["turnStart"]);
    expect(agent.followup).toHaveBeenCalledTimes(2);

    await driveTurn(harness, agent, "recovered");
    const payloads = next.events.map(payloadOf);
    expect(payloads[payloads.length - 1]).toBe("turnEnd");
    expect(next.events[next.events.length - 1]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
    expect(harness.agentsCreate).toHaveBeenCalledTimes(1);
  });

  it("cancel racing a re-materialization leaves no half-cleaned state", async () => {
    const harness = createHarness();
    const first = fakeAgent(S1);
    const second = fakeAgent(S1);
    const firstHandle = fakeHandle(first);
    harness.agentsCreate
      .mockResolvedValueOnce(firstHandle)
      .mockResolvedValueOnce(fakeHandle(second));
    harness.agentsGet.mockReturnValue(first);

    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });
    const stream = fakeStream();
    harness.sessions.send(S1, "in flight", stream);
    await flush();
    emit(harness, "agent/status", { agent: first, status: "running" });
    emit(harness, "session/event", first.session, { type: "turn/start", data: { turn: 1 } });

    // Cancel first, then re-materialize: the cancel already settled the
    // in-flight turn, so the teardown finds nothing in flight and adds no
    // second terminal frame.
    harness.sessions.cancel(S1);
    harness.agentsGet.mockReturnValue(second);
    await harness.sessions.materialize(S1, { preset: P1, persona: "p" });
    await flush();

    expect(stream.ended).toBe(true);
    expect(stream.events.map(payloadOf)).toEqual(["turnStart", "turnEnd"]);
    expect(stream.events[1]?.turnEnd?.status).toBe("TURN_STATUS_CANCELED");
    expect(firstHandle.dispose).toHaveBeenCalledTimes(1);

    // The new entry is clean; cancel resolves against it as a no-op.
    harness.sessions.cancel(S1);
    expect(await harness.sessions.listMessages(S1)).toEqual([]);
  });

  it("fails FAILED_PRECONDITION on an unmaterialized session and never creates", () => {
    const harness = createHarness();

    expect(() => harness.sessions.cancel(S1)).toThrow(AgentSessionError);
    try {
      harness.sessions.cancel(S1);
    } catch (err) {
      expect((err as AgentSessionError).code).toBe("FAILED_PRECONDITION");
    }
    expect(harness.agentsCreate).not.toHaveBeenCalled();
  });
});

describe("AgentSessions.shutdown", () => {
  it("aborts in-flight turns during shutdown and disposes the fiber last", async () => {
    const harness = createHarness();
    const agentA = fakeAgent("templates/saolei/sessions/a");
    const agentB = fakeAgent("templates/saolei/sessions/b");
    const handleA = fakeHandle(agentA);
    const handleB = fakeHandle(agentB);
    harness.agentsCreate.mockResolvedValueOnce(handleA).mockResolvedValueOnce(handleB);
    await harness.sessions.materialize("templates/saolei/sessions/a", { preset: P1, persona: "p" });
    await harness.sessions.materialize("templates/saolei/sessions/b", { preset: P1, persona: "p" });
    harness.agentsGet.mockImplementation((id: unknown) =>
      id === "templates/saolei/sessions/a" ? agentA : agentB,
    );

    const streamA = fakeStream();
    const streamB = fakeStream();
    harness.sessions.send("templates/saolei/sessions/a", "a", streamA);
    harness.sessions.send("templates/saolei/sessions/b", "b", streamB);
    await flush();

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

    await harness.sessions.shutdown();
    expect(order).toEqual(["dispose a", "dispose b", "fiber"]);
    expect(streamA.ended && streamB.ended).toBe(true);
  });
});
