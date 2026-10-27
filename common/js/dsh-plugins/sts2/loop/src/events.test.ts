/**
 * MainAgentEventStream tests (specs/080-host-main-sub-model/contracts/
 * sts2-loop-minimal.md §5): the main envelope shape on the log and live
 * channels, the identity filters, the detach face and the end-of-stream
 * dispose. The agent handle is a DI double recording the cordis subscriptions
 * (`style/javascript.md` Mock convention: no module interception).
 */

import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import type { TeamMemberEvent } from "@dominion/dsh-team";
import { describe, expect, it, vi } from "vitest";

import { MAIN_MEMBER_ROLE, MainAgentEventStream } from "./events.js";

type LogEvent = Extract<TeamMemberEvent, { channel: "log" }>["event"];
type LiveEvent = Extract<TeamMemberEvent, { channel: "live" }>["event"];

const SESSION_ID = "s1/main";

/** A structural session event: the stream forwards events verbatim. */
function sessionEvent(seq = 1): LogEvent {
  return {
    type: "assistant/message",
    seq,
    time: 1_700_000_000_000,
    data: {},
  } as unknown as LogEvent;
}

/** A structural assistant-stream frame: the stream forwards it verbatim. */
function assistantFrame(): Extract<LiveEvent, { kind: "assistant-stream" }>["frame"] {
  return {
    type: "start",
    attemptId: "attempt-1",
    revision: 1,
    turn: 1,
    step: 1,
  } as unknown as Extract<LiveEvent, { kind: "assistant-stream" }>["frame"];
}

type Listener = (...args: unknown[]) => void;

/** The agent-handle double: records `ctx.on` subscriptions and emits on demand. */
function createFakeHandle() {
  const listeners = new Map<string, Set<Listener>>();
  const offs = vi.fn((_name: string) => {});
  const agent = {
    id: SESSION_ID,
    ctx: {
      on: (name: string, listener: Listener) => {
        let set = listeners.get(name);
        if (set === undefined) {
          set = new Set();
          listeners.set(name, set);
        }
        set.add(listener);
        return () => {
          set.delete(listener);
          offs(name);
        };
      },
    },
  } as unknown as Agent;
  const handle = {
    agent,
    dispose: vi.fn(async () => {}),
  } as unknown as AgentHandle;

  return {
    handle,
    agent,
    offs,
    listenerCount(name: string): number {
      return listeners.get(name)?.size ?? 0;
    },
    emit(name: string, ...args: unknown[]): void {
      for (const listener of [...(listeners.get(name) ?? [])]) {
        listener(...args);
      }
    },
  };
}

describe("MainAgentEventStream", () => {
  it("subscribes the main agent's log and live faces once", () => {
    const fake = createFakeHandle();
    new MainAgentEventStream(fake.handle);

    expect(fake.listenerCount("session/event")).toBe(1);
    expect(fake.listenerCount("agent/assistant-stream")).toBe(1);
    expect(fake.listenerCount("agent/status")).toBe(1);
    expect(fake.listenerCount("agent/error")).toBe(1);
  });

  it("wraps this session's events in the main log envelope", () => {
    // Given a subscribed stream and one log production of the main session.
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const received: TeamMemberEvent[] = [];
    stream.subscribe((event) => received.push(event));
    const event = sessionEvent();

    // When the session publishes it.
    fake.emit("session/event", { id: SESSION_ID }, event);

    // Then the envelope names the main role and the agent session id.
    expect(received).toEqual([
      { role: MAIN_MEMBER_ROLE, memberId: SESSION_ID, channel: "log", event },
    ]);
  });

  it("drops another session's log events", () => {
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const received: TeamMemberEvent[] = [];
    stream.subscribe((event) => received.push(event));

    fake.emit("session/event", { id: "s1/other" }, sessionEvent());

    expect(received).toEqual([]);
  });

  it("wraps the live faces in the main live envelope", () => {
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const received: TeamMemberEvent[] = [];
    stream.subscribe((event) => received.push(event));
    const frame = assistantFrame();
    const error = new Error("boom");

    fake.emit("agent/assistant-stream", { agent: fake.agent, frame });
    fake.emit("agent/status", { agent: fake.agent, status: "running" });
    fake.emit("agent/error", { agent: fake.agent, error });

    expect(received).toEqual([
      {
        role: MAIN_MEMBER_ROLE,
        memberId: SESSION_ID,
        channel: "live",
        event: { kind: "assistant-stream", frame },
      },
      {
        role: MAIN_MEMBER_ROLE,
        memberId: SESSION_ID,
        channel: "live",
        event: { kind: "status", status: "running" },
      },
      {
        role: MAIN_MEMBER_ROLE,
        memberId: SESSION_ID,
        channel: "live",
        event: { kind: "error", error },
      },
    ]);
  });

  it("drops live productions reported for another agent", () => {
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const received: TeamMemberEvent[] = [];
    stream.subscribe((event) => received.push(event));

    const other = { id: "s1/other" };
    fake.emit("agent/assistant-stream", { agent: other, frame: assistantFrame() });
    fake.emit("agent/status", { agent: other, status: "idle" });
    fake.emit("agent/error", { agent: other, error: new Error("boom") });

    expect(received).toEqual([]);
  });

  it("fans one envelope out to every consumer", () => {
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const first: TeamMemberEvent[] = [];
    const second: TeamMemberEvent[] = [];
    stream.subscribe((event) => first.push(event));
    stream.subscribe((event) => second.push(event));

    fake.emit("session/event", { id: SESSION_ID }, sessionEvent(7));

    expect(first).toHaveLength(1);
    expect(second).toEqual(first);
  });

  it("detaches one consumer without ending the stream", () => {
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const detached = vi.fn();
    const retained = vi.fn();
    const off = stream.subscribe(detached);
    stream.subscribe(retained);

    off();
    fake.emit("session/event", { id: SESSION_ID }, sessionEvent());

    expect(detached).not.toHaveBeenCalled();
    expect(retained).toHaveBeenCalledTimes(1);
  });

  it("dispose ends the stream: unsubscribes every face and drops consumers", () => {
    const fake = createFakeHandle();
    const stream = new MainAgentEventStream(fake.handle);
    const consumer = vi.fn();
    stream.subscribe(consumer);
    expect(fake.offs).not.toHaveBeenCalled();

    stream.dispose();

    // Every face subscription is released exactly once; dispose is idempotent.
    expect(fake.offs).toHaveBeenCalledTimes(4);
    expect(fake.listenerCount("session/event")).toBe(0);
    expect(fake.listenerCount("agent/assistant-stream")).toBe(0);
    expect(fake.listenerCount("agent/status")).toBe(0);
    expect(fake.listenerCount("agent/error")).toBe(0);
    stream.dispose();
    expect(fake.offs).toHaveBeenCalledTimes(4);

    // Late productions no longer reach anybody, and subscribing after the end
    // is a no-op detach.
    fake.emit("session/event", { id: SESSION_ID }, sessionEvent());
    expect(consumer).not.toHaveBeenCalled();
    const late = vi.fn();
    const off = stream.subscribe(late);
    fake.emit("session/event", { id: SESSION_ID }, sessionEvent(2));
    off();
    expect(late).not.toHaveBeenCalled();
  });
});
