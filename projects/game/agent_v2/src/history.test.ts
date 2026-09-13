import { describe, expect, it, vi } from "vitest";
import {
  blockToContentBlock,
  broadcastSender,
  chunkToChatEvent,
  MemberCollector,
  TeamHistory,
} from "./history.js";
import type { DshStreamChunk, UserMessageEvent } from "./history.js";
import type { DshContext } from "./dsh.js";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { TeamMergeEntry } from "./history.js";

/**
 * Unit tests for the team history projections and the member collector
 * (specs/059-agent-v2-team-mode/data-model.md §2; contracts/team-api.md
 * §3.2/§5): the merged sequence with its monotonic seq anchor, the
 * per-member view with sender annotations, the member-labelled ChatEvent
 * mapping, and the turn lifecycle settlement. Streams are plain recorders
 * and the dsh events are emitted into a hand-rolled fake ctx — no module
 * interception (style/javascript.md Mock convention).
 */

type Listener = (...args: never[]) => void;

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

/** The payload discriminator of a constructed ChatEvent (oneof arms by presence). */
function payloadOf(event: ChatEvent): string {
  if (event.queued !== undefined) return "queued";
  if (event.turnStart !== undefined) return "turnStart";
  if (event.blockStart !== undefined) return "blockStart";
  if (event.delta !== undefined) return "delta";
  if (event.blockEnd !== undefined) return "blockEnd";
  if (event.turnEnd !== undefined) return "turnEnd";
  if (event.toolResult !== undefined) return "toolResult";
  if (event.teamMessage !== undefined) return "teamMessage";
  if (event.memberView !== undefined) return "memberView";
  return "";
}

const SESSION = "templates/saolei/sessions/s1";

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
});

describe("broadcastSender", () => {
  it("passes role strings through and degrades a missing role to the reserved user value", () => {
    expect(broadcastSender("player")).toBe("player");
    expect(broadcastSender("planner")).toBe("planner");
    expect(broadcastSender("scene-custom-role")).toBe("scene-custom-role");
    expect(broadcastSender(undefined)).toBe("user");
    expect(broadcastSender("")).toBe("user");
  });
});

describe("TeamHistory", () => {
  function createHistory() {
    const frames: ChatEvent[] = [];
    const history = new TeamHistory(SESSION, (event) => frames.push(event));
    return { history, frames };
  }

  it("assigns monotonic seq across user and member appends and fans out team_message frames", () => {
    const { history, frames } = createHistory();

    const user = history.appendUser("开始一局");
    const player = history.appendMemberOutput("player", [{ type: "text", text: "点击 (0,0)" }]);
    const planner = history.appendMemberOutput("planner", [{ type: "text", text: "复盘" }]);

    expect([user.seq, player?.seq, planner?.seq]).toEqual([1, 2, 3]);
    expect(history.listTeamMessages().map((entry) => [entry.member, entry.seq])).toEqual([
      ["user", 1],
      ["player", 2],
      ["planner", 3],
    ]);
    // The frame payload IS the projection entry: same seq value, same
    // producer label, same message object (contract §3.2 — 同源同值).
    expect(frames.map(payloadOf)).toEqual(["teamMessage", "teamMessage", "teamMessage"]);
    expect(frames[0]?.teamMessage?.member).toBe("user");
    expect(frames[0]?.teamMessage?.seq).toBe("1");
    expect(frames[0]?.teamMessage?.message).toBe(user.message);
    expect(frames[1]?.teamMessage?.member).toBe("player");
    expect(frames[1]?.teamMessage?.message).toBe(player?.message);
    expect(frames[1]?.member).toBeUndefined();
  });

  it("projects a member's own output into that member's view with sender = the member", () => {
    const { history } = createHistory();
    const player = history.appendMemberOutput("player", [
      { type: "reasoning", text: "thinking" },
      { type: "text", text: "answer" },
      { type: "image" },
    ]);

    const view = history.listMemberMessages("player");
    expect(view).toHaveLength(1);
    expect(view[0]?.message).toBe(player?.message);
    expect(view[0]?.sender).toBe("player");
    expect(view[0]?.message.blocks).toHaveLength(2);
    expect(history.listMemberMessages("planner")).toEqual([]);
  });

  it("skips empty member output and never allocates a seq for it", () => {
    const { history } = createHistory();
    expect(history.appendMemberOutput("player", [{ type: "image" }])).toBeUndefined();
    expect(history.listTeamMessages()).toEqual([]);
  });

  it("annotates a relayed member message as a sender-labelled user entry in the view", () => {
    const { history } = createHistory();
    // A `user/message` event stores the complete UserMessage as its data
    // (content/source at the data top level — dsh-session README).
    const broadcast: UserMessageEvent = {
      type: "user/message",
      data: {
        id: "m-broadcast",
        content: [{ type: "text", text: "[player] 已点击" }],
        source: { kind: "team-broadcast", role: "player" },
      },
    };
    const direct: UserMessageEvent = {
      type: "user/message",
      data: {
        id: "m-direct",
        content: [{ type: "text", text: "用户消息" }],
        source: { kind: "user" },
      },
    };

    history.appendMemberViewUser("planner", broadcast);
    history.appendMemberViewUser("planner", direct);

    const view = history.listMemberMessages("planner");
    expect(view.map((entry) => entry.sender)).toEqual(["player", "user"]);
    expect(view[0]?.message.role).toBe("ROLE_USER");
    expect(view[0]?.message.blocks[0]?.text?.content).toBe("[player] 已点击");
  });

  it("fans out a member_view frame when a user input enters the member's view", () => {
    const { history, frames } = createHistory();
    const direct: UserMessageEvent = {
      type: "user/message",
      data: {
        id: "m-direct",
        content: [{ type: "text", text: "用户消息" }],
        source: { kind: "user" },
      },
    };

    history.appendMemberViewUser("planner", direct);

    // Team-level frame (no outer member); the payload is the same view
    // element object ListMemberMessages serves (同源同值), so the live view
    // and the backfill agree (specs/060-agent-v2-team-optimize/contracts/
    // team-api.md §2).
    expect(frames.map(payloadOf)).toEqual(["memberView"]);
    const frame = frames[0];
    expect(frame?.member).toBeUndefined();
    expect(frame?.memberView?.member).toBe("planner");
    expect(frame?.memberView?.sender).toBe("user");
    expect(frame?.memberView?.message.role).toBe("ROLE_USER");
    expect(frame?.memberView?.message).toBe(history.listMemberMessages("planner")[0]?.message);
  });

  it("fans out a member_view frame for a broadcast relay with the sender role annotation", () => {
    const { history, frames } = createHistory();
    const broadcast: UserMessageEvent = {
      type: "user/message",
      data: {
        id: "m-broadcast",
        content: [{ type: "text", text: "[player] 已点击" }],
        source: { kind: "team-broadcast", role: "player" },
      },
    };

    history.appendMemberViewUser("planner", broadcast);

    expect(frames.map(payloadOf)).toEqual(["memberView"]);
    const frame = frames[0];
    expect(frame?.memberView?.member).toBe("planner");
    expect(frame?.memberView?.sender).toBe("player");
    expect(frame?.memberView?.message.blocks[0]?.text?.content).toBe("[player] 已点击");
    expect(frame?.memberView?.message).toBe(history.listMemberMessages("planner")[0]?.message);
  });

  it("settles the tool-call block shared by the merge entry and the member view", () => {
    const { history } = createHistory();
    history.appendMemberOutput("player", [
      { type: "text", text: "calling" },
      { type: "tool-call", id: "call-1", name: "saolei_init", arguments: "{}" },
    ]);

    expect(history.settleToolResult("player", "call-1", "TOOL_STATUS_SUCCEEDED", "new game started")).toBe(true);

    const entry = history.listTeamMessages()[0] as TeamMergeEntry;
    const block = entry.message.blocks[1]?.toolCall;
    expect(block?.status).toBe("TOOL_STATUS_SUCCEEDED");
    expect(block?.result).toBe("new game started");
    // The member view shares the message object — the settlement is visible.
    expect(history.listMemberMessages("player")[0]?.message.blocks[1]?.toolCall?.status).toBe(
      "TOOL_STATUS_SUCCEEDED",
    );
  });

  it("ignores tool results without an unsettled matching member block", () => {
    const { history } = createHistory();
    history.appendMemberOutput("planner", [
      { type: "tool-call", id: "call-p", name: "memory", arguments: "{}" },
    ]);

    // Another member's id and an already-unknown id never fabricate history.
    expect(history.settleToolResult("player", "call-p", "TOOL_STATUS_FAILED", "x")).toBe(false);
    expect(history.settleToolResult("planner", "call-unknown", "TOOL_STATUS_FAILED", "x")).toBe(false);
    expect(history.settleToolResult("planner", "call-p", "TOOL_STATUS_SUCCEEDED", "ok")).toBe(true);
    expect(history.settleToolResult("planner", "call-p", "TOOL_STATUS_FAILED", "again")).toBe(false);
  });
});

describe("MemberCollector", () => {
  const PLAYER_ID = `${SESSION}/player`;

  function chunkEvent(chunk: DshStreamChunk, step = 1) {
    return { type: "assistant/chunk", data: { turn: 1, step, chunk } };
  }

  function assistantMessageEvent(blocks: Array<Record<string, unknown>>, step = 1, usage?: Record<string, number>) {
    return {
      type: "assistant/message",
      data: { turn: 1, step, message: { content: blocks }, ...(usage ? { usage } : {}) },
    };
  }

  function createCollector(agentId = PLAYER_ID) {
    const { ctx, listeners } = fakeCtx();
    const agent = fakeAgent(agentId);
    const frames: ChatEvent[] = [];
    const history = new TeamHistory(SESSION, (event) => frames.push(event));
    const collector = new MemberCollector(ctx, agent, "player", SESSION, history, (event) =>
      frames.push(event),
    );
    return { listeners, agent, collector, history, frames };
  }

  it("latches a turn on the running transition and emits member-labelled frames through idle", () => {
    const { listeners, agent, collector, frames, history } = createCollector();

    emit(listeners, "agent/status", { agent, status: "running" });
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "hello" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 0, block: { type: "text", text: "hello" } }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "usage", usage: { inputTokens: 7, outputTokens: 9 } }));
    emit(listeners, "session/event", agent.session, assistantMessageEvent([{ type: "text", text: "hello" }], 1, { inputTokens: 7, outputTokens: 9 }));
    emit(listeners, "agent/status", { agent, status: "idle" });

    // Member event frames carry the outer member label and one turn id.
    const memberFrames = frames.filter(
      (frame) => frame.teamMessage === undefined && frame.memberView === undefined,
    );
    expect(memberFrames.map(payloadOf)).toEqual(["turnStart", "blockStart", "delta", "blockEnd", "turnEnd"]);
    expect(memberFrames.every((frame) => frame.member === "player")).toBe(true);
    const turnId = memberFrames[0]?.turnId;
    expect(turnId).toBeTruthy();
    expect(memberFrames.every((frame) => frame.turnId === turnId)).toBe(true);
    expect(memberFrames[4]?.turnEnd?.status).toBe("TURN_STATUS_COMPLETED");
    expect(memberFrames[4]?.turnEnd?.usage?.inputTokens).toBe("7");

    // The assistant finality entered the merge sequence as a member entry.
    const entry = history.listTeamMessages()[0] as TeamMergeEntry;
    expect(entry.member).toBe("player");
    expect(entry.message.blocks[0]?.text?.content).toBe("hello");
    collector.dispose();
  });

  it("emits a tool_result member frame and settles the matching history block", () => {
    const { listeners, agent, collector, frames, history } = createCollector();

    emit(listeners, "agent/status", { agent, status: "running" });
    emit(listeners, "session/event", agent.session, assistantMessageEvent([
      { type: "tool-call", id: "call-1", name: "saolei_init", arguments: "{}" },
    ]));
    emit(listeners, "session/event", agent.session, {
      type: "tool/result",
      data: {
        turn: 1,
        step: 1,
        message: {
          content: [{ type: "tool-result", toolCallId: "call-1", content: [{ type: "text", text: "new game started" }] }],
        },
      },
    });

    const result = frames.find((frame) => frame.toolResult !== undefined);
    expect(result?.member).toBe("player");
    expect(result?.toolResult).toEqual({
      toolId: "call-1",
      status: "TOOL_STATUS_SUCCEEDED",
      result: "new game started",
    });
    const entry = history.listTeamMessages()[0] as TeamMergeEntry;
    expect(entry.message.blocks[0]?.toolCall?.status).toBe("TOOL_STATUS_SUCCEEDED");
    collector.dispose();
  });

  it("appends the streamed prefix as an interrupted member entry when a provider failure settles the turn", () => {
    const { listeners, agent, collector, history } = createCollector();

    emit(listeners, "agent/status", { agent, status: "running" });
    // The reasoning prefix streams as bare deltas (no block-end before the failure).
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "reasoning-delta", index: 0, text: "Thinking about " }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "reasoning-delta", index: 0, text: "the request" }));
    emit(listeners, "agent/error", { agent, error: { message: "provider failure", code: "TRANSPORT" } });
    emit(listeners, "agent/status", { agent, status: "idle" });

    const entries = history.listTeamMessages();
    expect(entries).toHaveLength(1);
    expect(entries[0]?.member).toBe("player");
    expect(entries[0]?.message.interrupted).toBe(true);
    expect(entries[0]?.message.blocks[0]?.think?.content).toBe("Thinking about the request");
    collector.dispose();
  });

  it("settles CANCELED/ABORTED when the session marks the in-flight turn", () => {
    for (const [status, expected] of [
      ["CANCELED", "TURN_STATUS_CANCELED"],
      ["ABORTED", "TURN_STATUS_ABORTED"],
    ] as const) {
      const { listeners, agent, collector, frames } = createCollector();
      emit(listeners, "agent/status", { agent, status: "running" });
      collector.markOutcome({ status });
      emit(listeners, "agent/status", { agent, status: "idle" });

      const end = frames.find((frame) => frame.turnEnd !== undefined);
      expect(end?.turnEnd?.status).toBe(expected);
      collector.dispose();
    }
  });

  it("remaps per-step block indexes onto one turn-global sequence and stamps the step", () => {
    const { listeners, agent, collector, frames } = createCollector();

    emit(listeners, "agent/status", { agent, status: "running" });
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "calling" }));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-end", index: 0, block: { type: "text", text: "calling" } }));
    // Step 2 restarts the provider index at 0 — it must not collide.
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "block-start", index: 0, blockType: "text" }, 2));
    emit(listeners, "session/event", agent.session, chunkEvent({ type: "text-delta", index: 0, text: "board" }, 2));

    const blockFrames = frames.filter(
      (frame) => frame.blockStart !== undefined || frame.blockEnd !== undefined || frame.delta !== undefined,
    );
    expect(blockFrames.map((frame) => frame.blockStart?.index ?? frame.blockEnd?.index ?? frame.delta?.index)).toEqual([
      0, 0, 0, 1, 1,
    ]);
    expect(blockFrames[3]?.blockStart?.step).toBe(2);
    expect(blockFrames[4]?.delta?.step).toBe(2);
    collector.dispose();
  });

  it("ignores events of other members and detaches on dispose", () => {
    const { listeners, agent, collector, frames } = createCollector();

    const other = { id: `${SESSION}/planner` };
    const otherAgent = fakeAgent(`${SESSION}/planner`);
    emit(listeners, "session/event", other, chunkEvent({ type: "text-delta", index: 0, text: "foreign" }));
    emit(listeners, "agent/status", { agent: otherAgent, status: "idle" });
    emit(listeners, "agent/error", { agent: otherAgent, error: { message: "foreign", code: "X" } });
    expect(frames).toEqual([]);

    emit(listeners, "agent/status", { agent, status: "running" });
    expect(frames.map(payloadOf)).toEqual(["turnStart"]);
    collector.dispose();
    emit(listeners, "agent/status", { agent, status: "idle" });
    expect(frames.map(payloadOf)).toEqual(["turnStart"]);
  });
});
