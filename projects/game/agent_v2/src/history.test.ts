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
    expect(
      chunkToChatEvent({ type: "block-start", index: 0, blockType: "text" }, SESSION, TURN)?.blockStart,
    ).toEqual({ index: 0, type: "BLOCK_TYPE_TEXT", step: 0 });
    const think = chunkToChatEvent({ type: "block-start", index: 1, blockType: "reasoning" }, SESSION, TURN)?.blockStart;
    expect(think?.type).toBe("BLOCK_TYPE_THINK");
    const tool = chunkToChatEvent(
      { type: "block-start", index: 2, blockType: "tool-call", id: "call-1", name: "bash" },
      SESSION,
      TURN,
    )?.blockStart;
    expect(tool).toEqual({ index: 2, type: "BLOCK_TYPE_TOOL_CALL", toolId: "call-1", name: "bash", step: 0 });
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
      step: 0,
    });
    expect(chunkToChatEvent({ type: "reasoning-delta", index: 1, text: "b" }, SESSION, TURN)?.delta).toEqual({
      index: 1,
      text: "b",
      step: 0,
    });
    expect(
      chunkToChatEvent({ type: "tool-call-delta", index: 2, argumentsDelta: "{\"x" }, SESSION, TURN)?.delta,
    ).toEqual({ index: 2, text: "{\"x", step: 0 });
  });

  it("maps block-end with the terminal ContentBlock projection", () => {
    const event = chunkToChatEvent(
      { type: "block-end", index: 0, block: { type: "text", text: "done" } },
      SESSION,
      TURN,
    );
    expect(event?.blockEnd).toEqual({ index: 0, block: { text: { content: "done" } }, step: 0 });
  });

  it("stamps the given step onto every mapped block frame", () => {
    // specs/054-agent-v2-bugfixes/data-model.md §1.1: the step number rides
    // on every block event so clients can segment the turn by model-output
    // step.
    expect(
      chunkToChatEvent({ type: "block-start", index: 0, blockType: "text" }, SESSION, TURN, undefined, 3)
        ?.blockStart?.step,
    ).toBe(3);
    expect(chunkToChatEvent({ type: "text-delta", index: 0, text: "a" }, SESSION, TURN, undefined, 3)?.delta?.step).toBe(3);
    expect(
      chunkToChatEvent({ type: "block-end", index: 0, block: { type: "text", text: "a" } }, SESSION, TURN, undefined, 3)
        ?.blockEnd?.step,
    ).toBe(3);
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

  it("records the interrupted flag sparsely: only a true append lands the field", () => {
    // specs/054-agent-v2-bugfixes/data-model.md §1.5: the driver's interrupted
    // fixation marks the history message so List consumers can exclude it
    // from final-answer folding; settled appends (default) stay field-free.
    const history = new SessionHistory();
    history.appendAssistant([{ type: "text", text: "settled" }]);
    history.appendAssistant([{ type: "text", text: "partial" }], true);

    const messages = history.list();
    expect(messages[0]?.interrupted).toBeUndefined();
    expect(messages[1]?.interrupted).toBe(true);
  });
});

describe("SessionHistory.settleToolResult", () => {
  function historyWithToolCall(toolId: string) {
    const history = new SessionHistory();
    history.appendUser("go");
    history.appendAssistant([
      { type: "text", text: "calling" },
      { type: "tool-call", id: toolId, name: "saolei_init", arguments: "{}" },
    ]);
    return history;
  }

  it("settles the most recent RUNNING block with the matching tool_id", () => {
    const history = historyWithToolCall("call-1");

    expect(history.settleToolResult("call-1", "TOOL_STATUS_SUCCEEDED", "board text")).toBe(true);

    const block = history.list()[1]?.blocks[1]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_SUCCEEDED");
    expect(block?.result).toBe("board text");
    // The other blocks of the message are untouched.
    expect(history.list()[1]?.blocks[0]).toEqual({ text: { content: "calling" } });
  });

  it("ignores a result with no matching tool_id (never fabricated into history)", () => {
    const history = historyWithToolCall("call-1");

    expect(history.settleToolResult("call-unknown", "TOOL_STATUS_FAILED", "err")).toBe(false);

    const block = history.list()[1]?.blocks[1]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_RUNNING");
    expect(block?.result).toBeUndefined();
  });

  it("prefers the newest match and refuses to re-settle a terminal block", () => {
    const history = historyWithToolCall("call-1");
    history.settleToolResult("call-1", "TOOL_STATUS_SUCCEEDED", "first");

    // Second turn re-uses a stale id: only RUNNING blocks are candidates, so
    // the terminal block from the earlier turn is left alone.
    expect(history.settleToolResult("call-1", "TOOL_STATUS_FAILED", "second")).toBe(false);
    const block = history.list()[1]?.blocks[1]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_SUCCEEDED");
    expect(block?.result).toBe("first");
  });
});

describe("TurnCollector interrupted-tail backfill", () => {
  const SESSION = "templates/saolei/sessions/s1";

  function chunkEvent(chunk: DshStreamChunk, step = 1) {
    return { type: "assistant/chunk", data: { turn: 1, step, chunk } };
  }

  it("appends the streamed prefix as an interrupted history entry when a provider failure settles the turn", async () => {
    // specs/054 semantics through the official loop: the loop solidifies an
    // interrupted prefix only on cancellation, so the collector carries the
    // streamed blocks and appends the interrupted history entry itself when
    // the turn settles ERROR (specs/059-agent-v2-team-mode loop pivot).
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);
    const stream = recorder();

    collector.begin("turn-1", stream);
    // The reasoning prefix streams as bare deltas (no block-end before the
    // failure) — the fail-mid wire shape.
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "reasoning-delta", index: 0, text: "Thinking about " }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "reasoning-delta", index: 0, text: "the request" }));
    // A text block that fully closed carries its authoritative view.
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 1, blockType: "text" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 1, text: "partial answer" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 1, block: { type: "text", text: "partial answer" } }));
    emit(listeners, "agent/error", { agent, error: { message: "provider failure", code: "GLM_TRANSPORT" } });
    emit(listeners, "agent/status", { agent, status: "idle" });

    const settlement = await collector.awaitSettled();
    expect(settlement.status).toBe("ERROR");

    const messages = history.list();
    expect(messages).toHaveLength(1);
    expect(messages[0]?.interrupted).toBe(true);
    expect(messages[0]?.blocks[0]?.think?.content).toBe("Thinking about the request");
    expect(messages[0]?.blocks[1]?.text?.content).toBe("partial answer");
  });

  it("appends nothing when a failed turn streamed no content", async () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);
    collector.begin("turn-1", recorder());

    emit(listeners, "agent/error", { agent, error: { message: "boom", code: "GLM_HTTP_500" } });
    emit(listeners, "agent/status", { agent, status: "idle" });
    await collector.awaitSettled();

    expect(history.list()).toEqual([]);
  });

  it("clears the pending prefix when the step finalizes normally", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);
    collector.begin("turn-1", recorder());

    // Step 1 fails to settle? No: a finalizing assistant/message clears the
    // pending prefix; a later step-2 failure must not re-append step 1.
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 0, block: { type: "text", text: "step one" } }, 1));
    emit(listeners, "session/event", agent.session, {
      type: "assistant/message",
      data: { turn: 1, step: 1, message: { content: [{ type: "text", text: "step one" }] } },
    });
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 0, block: { type: "text", text: "step two prefix" } }, 2));
    emit(listeners, "agent/error", { agent, error: { message: "boom", code: "GLM_HTTP_500" } });
    emit(listeners, "agent/status", { agent, status: "idle" });
    void collector.awaitSettled();

    const messages = history.list();
    expect(messages).toHaveLength(2);
    expect(messages[1]?.interrupted).toBe(true);
    expect(messages[1]?.blocks[0]?.text?.content).toBe("step two prefix");
  });
});

describe("TurnCollector", () => {
  const SESSION = "templates/saolei/sessions/s1";

  function chunkEvent(chunk: DshStreamChunk, step = 1) {
    return { type: "assistant/chunk", data: { turn: 1, step, chunk } };
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

  it("forwards the event's interrupted flag onto the history message", () => {
    // specs/054-agent-v2-bugfixes/data-model.md §1.5: the driver's
    // interrupted fixation (assistant/message with data.interrupted) marks
    // the history message; List 透出后 web 折叠判定据此排除中断前缀。
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    new TurnCollector(ctx, agent, SESSION, history);

    emit(listeners, "session/event", agent.session, {
      type: "assistant/message",
      data: {
        turn: 1,
        step: 1,
        message: { content: [{ type: "text", text: "partial prefix" }] },
        interrupted: true,
      },
    });

    const messages = history.list();
    expect(messages).toHaveLength(1);
    expect(messages[0]?.interrupted).toBe(true);
    expect(messages[0]?.blocks[0]?.text?.content).toBe("partial prefix");
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

  // ── multi-step tool turns (specs/051-agent-v2-dsh-migration/data-model.md
  // §2.3/§2.4): turn-global index remap + tool_result frame + backfill ──────

  function toolResultEvent(step: number, callId: string, text: string, isError = false) {
    return {
      type: "tool/result",
      data: {
        turn: 1,
        step,
        message: {
          content: [{ type: "tool-result", toolCallId: callId, isError, content: [{ type: "text", text }] }],
        },
      },
    };
  }

  function assistantMessageEvent(step: number, blocks: Array<Record<string, unknown>>) {
    return {
      type: "assistant/message",
      data: { turn: 1, step, message: { content: blocks } },
    };
  }

  it("stamps block frames with the ActiveTurn-tracked step, defaulting to 0 when absent", () => {
    // specs/054-agent-v2-bugfixes/data-model.md §1.1: the collector tracks the
    // event's step and forwards it on every mapped block frame; a dsh event
    // without a step number degrades to step 0.
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const collector = new TurnCollector(ctx, agent, SESSION, new SessionHistory());
    const stream = recorder();
    collector.begin("turn-1", stream);

    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "one" }, 2));
    emit(
      listeners,
      "session/event",
      agent.session,
      { type: "assistant/chunk", data: { turn: 1, chunk: { type: "text-delta", index: 0, text: "zero" } } },
    );
    collector.abort();

    expect(stream.events[0]?.blockStart?.step).toBe(1);
    expect(stream.events[1]?.delta?.step).toBe(2);
    // An event carrying no step number maps to step 0 (data-model.md §1.1).
    expect(stream.events[2]?.delta?.step).toBe(0);
  });

  it("remaps per-step block indexes onto one turn-global monotonic sequence", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);
    const stream = recorder();
    collector.begin("turn-1", stream);

    // Step 1: two blocks (indexes 0, 1).
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "calling" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 0, block: { type: "text", text: "calling" } }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 1, blockType: "tool-call", id: "call-1", name: "saolei_init" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "tool-call-delta", index: 1, argumentsDelta: "{}" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 1, block: { type: "tool-call", id: "call-1", name: "saolei_init", arguments: "{}" } }));
    emit(listeners, "session/event", agent.session, assistantMessageEvent(1, [
      { type: "text", text: "calling" },
      { type: "tool-call", id: "call-1", name: "saolei_init", arguments: "{}" },
    ]));

    // Step 2: the provider restarts at index 0 — it must NOT collide with
    // step 1's block 0 (data-model.md §2.4 turn-global monotonicity).
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }, 2));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "board" }, 2));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 0, block: { type: "text", text: "board" } }, 2));

    const indexes = stream.events
      .filter((event) => event.blockStart !== undefined || event.blockEnd !== undefined)
      .map((event) => event.blockStart?.index ?? event.blockEnd?.index);
    // Step 1 locals (0,1) keep globals (0,1); step 2's local 0 becomes the
    // next global (2) — no collision with step 1's block 0.
    expect(indexes).toEqual([0, 0, 1, 1, 2, 2]);
    // Deltas carry the remapped index of their block.
    expect(stream.events[1]?.delta?.index).toBe(0);
    const stepTwoDelta = stream.events[stream.events.length - 2];
    expect(stepTwoDelta?.delta?.text).toBe("board");
    expect(stepTwoDelta?.delta?.index).toBe(2);
    collector.abort();
  });

  it("emits a tool_result frame and settles the history block by tool_id", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);
    const stream = recorder();
    collector.begin("turn-1", stream);

    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "tool-call", id: "call-1", name: "saolei_init" }));
    emit(listeners, "session/event", agent.session, assistantMessageEvent(1, [
      { type: "tool-call", id: "call-1", name: "saolei_init", arguments: "{}" },
    ]));
    emit(listeners, "session/event", agent.session, { type: "tool/call", data: { turn: 1, step: 1, callId: "call-1", name: "saolei_init", arguments: "{}" } });
    emit(listeners, "session/event", agent.session, toolResultEvent(1, "call-1", "new game started"));

    const frame = stream.events[stream.events.length - 1];
    expect(frame?.toolResult).toEqual({
      toolId: "call-1",
      status: "TOOL_STATUS_SUCCEEDED",
      result: "new game started",
    });
    expect(frame?.turnId).toBe("turn-1");
    expect(frame?.session).toBe(SESSION);

    const block = history.list()[0]?.blocks[0]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_SUCCEEDED");
    expect(block?.result).toBe("new game started");
    collector.abort();
  });

  it("maps an error tool result to TOOL_STATUS_FAILED and ignores unknown tool ids in history", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);
    const stream = recorder();
    collector.begin("turn-1", stream);

    emit(listeners, "session/event", agent.session, assistantMessageEvent(1, [
      { type: "tool-call", id: "call-1", name: "saolei_operate", arguments: "{}" },
    ]));
    // Unknown id first: the history has no unsettled match for it.
    emit(listeners, "session/event", agent.session, toolResultEvent(1, "call-unknown", "boom", true));
    emit(listeners, "session/event", agent.session, { type: "tool/call", data: { turn: 1, step: 1, callId: "call-1", name: "saolei_operate", arguments: "{}" } });
    emit(listeners, "session/event", agent.session, toolResultEvent(1, "call-1", "desktop disconnected", true));

    const failed = stream.events[stream.events.length - 1]?.toolResult;
    expect(failed?.status).toBe("TOOL_STATUS_FAILED");
    expect(failed?.result).toBe("desktop disconnected");

    // The unknown id left no trace in history; the known one is settled.
    expect(history.settleToolResult("call-1", "TOOL_STATUS_RUNNING", "")).toBe(false);
    const block = history.list()[0]?.blocks[0]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_FAILED");
    collector.abort();
  });

  it("settles history from tool results even when no stream is attached", () => {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(SESSION);
    const history = new SessionHistory();
    const collector = new TurnCollector(ctx, agent, SESSION, history);

    emit(listeners, "session/event", agent.session, assistantMessageEvent(1, [
      { type: "tool-call", id: "call-1", name: "saolei_init", arguments: "{}" },
    ]));
    emit(listeners, "session/event", agent.session, toolResultEvent(1, "call-1", "new game started"));

    const block = history.list()[0]?.blocks[0]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_SUCCEEDED");
    expect(block?.result).toBe("new game started");
    collector.abort();
  });
});
