import { describe, expect, it, vi } from "vitest";
import { blockToContentBlock, chunkToChatEvent, SessionHistory, TurnCollector } from "./history.js";
import type { DshStreamChunk } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";

/**
 * Unit tests for the dsh→ChatEvent mapping and the session-lifetime turn
 * collector (mapping table: specs/049-agent-v2-dsh-init/contracts/
 * conversation-api.md §4; event-order invariants: §3). Streams are plain
 * recorders and the dsh events are emitted into a hand-rolled fake ctx —
 * no module interception (style/javascript.md Mock convention).
 */

type Listener = (...args: never[]) => void;

interface Recorder {
  events: ChatEvent[];
  ended: boolean;
  write(event: ChatEvent): void;
  end(): void;
}

function recorder(): Recorder {
  const r = {
    events: [] as ChatEvent[],
    ended: false,
    write(event: ChatEvent) {
      r.events.push(event);
    },
    end() {
      r.ended = true;
    },
  };
  return r;
}

function fakeCtx() {
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
  return { ctx: { on } as unknown as DshContext, listeners };
}

function fakeAgent(id: string): Agent {
  return { id, session: { id } } as unknown as Agent;
}

function emit(
  listeners: Map<string, Listener[]>,
  name: string,
  ...args: unknown[]
): void {
  for (const listener of [...(listeners.get(name) ?? [])]) {
    (listener as (...emitArgs: unknown[]) => void)(...args);
  }
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

describe("blockToContentBlock", () => {
  it("maps text/reasoning/tool-call blocks and drops unknown ones", () => {
    expect(blockToContentBlock({ type: "text", text: "hi" })).toEqual({ text: { content: "hi" } });
    expect(blockToContentBlock({ type: "reasoning", text: "hmm" })).toEqual({ think: { content: "hmm" } });
    expect(blockToContentBlock({ type: "tool-call", id: "call-1", name: "bash", arguments: "{}" })).toEqual({
      toolCall: { toolId: "call-1", name: "bash", argsJson: "{}", status: "TOOL_STATUS_RUNNING" },
    });
    expect(blockToContentBlock({ type: "image" })).toBeUndefined();
  });
});

describe("chunkToChatEvent", () => {
  const SESSION = "templates/saolei/sessions/s1";
  const TURN = "turn-1";

  it("maps block-start with the BlockType vocabulary and tool fields", () => {
    expect(chunkToChatEvent({ type: "block-start", index: 0, blockType: "text" }, SESSION, TURN)?.blockStart).toEqual({
      index: 0,
      type: "BLOCK_TYPE_TEXT",
    });
    const think = chunkToChatEvent({ type: "block-start", index: 1, blockType: "reasoning" }, SESSION, TURN)?.blockStart;
    expect(think?.type).toBe("BLOCK_TYPE_THINK");
    const tool = chunkToChatEvent(
      { type: "block-start", index: 2, blockType: "tool-call", id: "call-1", name: "bash" },
      SESSION,
      TURN,
    )?.blockStart;
    expect(tool).toEqual({ index: 2, type: "BLOCK_TYPE_TOOL_CALL", toolId: "call-1", name: "bash" });
  });

  it("drops a block-start with an unknown blockType (no TEXT fallback)", () => {
    // Forward-compat policy: unknown block types have no display projection
    // in this phase and are dropped, exactly like unknown block-end content.
    expect(
      chunkToChatEvent({ type: "block-start", index: 0, blockType: "hologram" }, SESSION, TURN),
    ).toBeUndefined();
  });

  it("unifies the three delta vocabularies into delta{text}", () => {
    expect(chunkToChatEvent({ type: "text-delta", index: 0, text: "a" }, SESSION, TURN)?.delta).toEqual({
      index: 0,
      text: "a",
    });
    expect(chunkToChatEvent({ type: "reasoning-delta", index: 1, text: "b" }, SESSION, TURN)?.delta).toEqual({
      index: 1,
      text: "b",
    });
    expect(
      chunkToChatEvent({ type: "tool-call-delta", index: 2, argumentsDelta: "{\"x" }, SESSION, TURN)?.delta,
    ).toEqual({ index: 2, text: "{\"x" });
  });

  it("maps block-end with the terminal ContentBlock projection", () => {
    const event = chunkToChatEvent(
      { type: "block-end", index: 0, block: { type: "text", text: "done" } },
      SESSION,
      TURN,
    );
    expect(event?.blockEnd).toEqual({ index: 0, block: { text: { content: "done" } } });
  });

  it("never frames usage or finish chunks (folded into turn_end / idle-driven)", () => {
    expect(chunkToChatEvent({ type: "usage", usage: { inputTokens: 1, outputTokens: 2 } }, SESSION, TURN)).toBeUndefined();
    expect(chunkToChatEvent({ type: "finish" }, SESSION, TURN)).toBeUndefined();
    expect(chunkToChatEvent({ type: "response.created" }, SESSION, TURN)).toBeUndefined();
  });

  it("stamps every mapped event with session and turn id", () => {
    const event = chunkToChatEvent({ type: "text-delta", index: 0, text: "x" }, SESSION, TURN);
    expect(event?.session).toBe(SESSION);
    expect(event?.turnId).toBe(TURN);
  });
});

describe("SessionHistory", () => {
  it("appends user and agent messages with ordered sequence ids and classified blocks", () => {
    const history = new SessionHistory();
    history.appendUser("question");
    history.appendAssistant([
      { type: "reasoning", text: "thinking" },
      { type: "text", text: "answer" },
      { type: "image" },
    ]);

    const messages = history.list();
    expect(messages.map((message) => message.messageId)).toEqual(["m1", "m2"]);
    expect(messages.map((message) => message.role)).toEqual(["ROLE_USER", "ROLE_AGENT"]);
    expect(messages[0]?.blocks).toEqual([{ text: { content: "question" } }]);
    expect(messages[1]?.blocks[0]?.think?.content).toBe("thinking");
    expect(messages[1]?.blocks[1]?.text?.content).toBe("answer");
    expect(messages[1]?.blocks).toHaveLength(2);
    expect(messages[0]?.createTime?.seconds).toBeTypeOf("number");
  });

  it("returns a defensive snapshot (the record itself lives with the session entry)", () => {
    const history = new SessionHistory();
    history.appendUser("q");
    const snapshot = history.list();
    snapshot.pop();
    expect(history.list()).toHaveLength(1);
  });
});

describe("TurnCollector", () => {
  const SESSION = "templates/saolei/sessions/s1";

  function chunkEvent(chunk: DshStreamChunk) {
    return { type: "assistant/chunk", data: { turn: 1, step: 1, chunk } };
  }

  it("streams mapped events for the active turn and settles COMPLETED with usage on idle", async () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const collector = new TurnCollector(ctx, agent, SESSION, new SessionHistory());
    const stream = recorder();

    collector.begin("turn-1", stream);
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "hello" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "usage", usage: { inputTokens: 7, outputTokens: 9 } }));
    emit(listeners, "agent/status", { agent, status: "running" });
    emit(listeners, "agent/status", { agent, status: "idle" });

    expect(stream.events.map(payloadOf)).toEqual(["blockStart", "delta"]);
    const settlement = await collector.awaitSettled();
    expect(settlement.status).toBe("COMPLETED");
    expect(settlement.usage).toEqual({ inputTokens: 7, outputTokens: 9 });
  });

  it("settles ERROR when agent/error or turn/end{error} preceded idle", async () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const collector = new TurnCollector(ctx, agent, SESSION, new SessionHistory());
    collector.begin("turn-1", recorder());

    emit(listeners, "agent/error", { agent, turn: 1, step: 1, error: { message: "boom", code: "GLM_HTTP_500" } });
    emit(listeners, "session/event", agent.session, {
      type: "turn/end",
      data: { turn: 1, reason: { kind: "error", error: { message: "boom", code: "GLM_HTTP_500" } } },
    });
    emit(listeners, "agent/status", { agent, status: "idle" });

    const settlement = await collector.awaitSettled();
    expect(settlement.status).toBe("ERROR");
    expect(settlement.error).toEqual({ code: "GLM_HTTP_500", message: "boom" });
  });

  it("aborts the in-flight turn and hands back the stream for the ABORTED frame", async () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const collector = new TurnCollector(ctx, agent, SESSION, new SessionHistory());
    const stream = recorder();
    collector.begin("turn-1", stream);
    const settled = collector.awaitSettled();

    const inFlight = collector.abort();
    expect(inFlight?.turnId).toBe("turn-1");
    expect(inFlight?.stream).toBe(stream);
    await expect(settled).resolves.toEqual({ status: "ABORTED", usage: undefined });

    // Events after abort are no longer forwarded.
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "late" }));
    expect(stream.events).toEqual([]);

    // An abort racing before awaitSettled resolves immediately.
    collector.begin("turn-2", recorder());
    collector.abort();
    await expect(collector.awaitSettled()).resolves.toEqual({ status: "ABORTED", usage: undefined });
  });

  it("ignores events of other sessions and agents", async () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const collector = new TurnCollector(ctx, agent, SESSION, new SessionHistory());
    const stream = recorder();
    collector.begin("turn-1", stream);

    const otherSession = { id: "templates/saolei/sessions/other" };
    const otherAgent = fakeAgent("templates/saolei/sessions/other");
    emit(listeners, "session/event", otherSession, chunkEvent({ type: "text-delta", index: 0, text: "foreign" }));
    emit(listeners, "agent/status", { agent: otherAgent, status: "idle" });
    emit(listeners, "agent/error", { agent: otherAgent, error: { message: "foreign", code: "X" } });

    expect(stream.events).toEqual([]);
    collector.abort();
  });

  it("appends assistant/message finality blocks to the injected history (session lifetime)", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    new TurnCollector(ctx, agent, SESSION, history);
    // No begin()/active turn: history collection is session-lifetime, not
    // stream-lifetime (research.md D10-2).
    emit(listeners, "session/event", agent.session, {
      type: "assistant/message",
      data: { turn: 1, step: 1, message: { content: [{ type: "text", text: "final" }] } },
    });
    const messages = history.list();
    expect(messages).toHaveLength(1);
    expect(messages[0]?.role).toBe("ROLE_AGENT");
    expect(messages[0]?.blocks[0]?.text?.content).toBe("final");
  });

  it("detaches its listeners on dispose", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const collector = new TurnCollector(ctx, agent, SESSION, new SessionHistory());
    collector.dispose();
    const stream = recorder();
    collector.begin("turn-1", stream);
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "z" }));
    expect(stream.events).toEqual([]);
  });
});
