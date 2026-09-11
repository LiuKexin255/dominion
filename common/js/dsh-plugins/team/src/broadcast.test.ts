/**
 * Broadcast unit tests: unit derivation (pairing, order, completeness), the
 * consumption-anchor reader, and the rendered group-chat format — the wire
 * shape of specs/060-agent-v2-team-optimize/contracts/team-api.md §4 (single
 * tag pair, no head line, no think content, verbatim body, no truncation).
 *
 * Pattern (style/javascript.md Mock convention): pure functions over real
 * dsh message values — no interception.
 */

import {
  CallId,
  createAssistantMessage,
  createToolResultMessage,
  createUserMessage,
  MessageId,
} from "@deepseek-ai/dsh-llm";
import type {
  AssistantMessage,
  ContentBlock,
  UserMessage,
} from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { describe, expect, it } from "vitest";

import {
  buildBroadcastMessage,
  consumedAnchors,
  deriveUnits,
  messageBody,
  renderBroadcast,
  tagName,
} from "./broadcast.js";
import { renderTeamSection } from "./section.js";

const SENDER = SessionId("templates/saolei/sessions/s1/player");

let cursor = 0;

function order(): { seq: number; time: number } {
  cursor += 1;
  return { seq: cursor, time: 1_000_000 + cursor };
}

function speechMessage(text: string): AssistantMessage {
  return createAssistantMessage({
    content: [{ type: "text", text }],
    source: { provider: "fake", model: "fake" },
  });
}

function speechEvent(message: AssistantMessage): SessionEvent {
  return {
    type: "assistant/message",
    ...order(),
    data: { turn: 1, step: 1, message },
  } as unknown as SessionEvent;
}

function toolCallEvent(callId: string, name: string, args: string): SessionEvent {
  return {
    type: "tool/call",
    ...order(),
    data: { turn: 1, step: 1, callId: CallId(callId), name, arguments: args },
  } as unknown as SessionEvent;
}

function toolResultEvent(callId: string, content: ContentBlock[]): SessionEvent {
  return {
    type: "tool/result",
    ...order(),
    data: {
      turn: 1,
      step: 1,
      message: createToolResultMessage({
        callId: CallId(callId),
        content,
        isError: false,
      }),
    },
  } as unknown as SessionEvent;
}

function userMessageEvent(message: UserMessage): SessionEvent {
  return { type: "user/message", ...order(), data: message } as unknown as SessionEvent;
}

describe("tagName", () => {
  it("normalizes an open role string into a safe tag stem", () => {
    expect(tagName("player")).toBe("player");
    expect(tagName("Planner 2")).toBe("planner-2");
    expect(tagName("")).toBe("member");
  });
});

describe("messageBody", () => {
  it("keeps text blocks in order and drops reasoning and tool-call blocks", () => {
    const message = createAssistantMessage({
      content: [
        { type: "reasoning", text: "thinking" },
        { type: "text", text: "answer" },
        { type: "tool-call", id: CallId("c1"), name: "saolei_operate", arguments: '{"x":1}' },
      ],
      source: { provider: "fake", model: "fake" },
    });
    expect(messageBody(message)).toBe("answer");
  });
});

describe("deriveUnits", () => {
  it("orders speech and paired tool units by production and pairs tool calls by callId", () => {
    const first = speechMessage("first");
    const second = speechMessage("second");
    const events = [
      speechEvent(first),
      toolCallEvent("call-1", "saolei_operate", '{"x":1}'),
      toolResultEvent("call-1", [{ type: "text", text: "clicked" }]),
      speechEvent(second),
    ];

    const units = deriveUnits(events);

    expect(units.map((unit) => [unit.kind, unit.anchor])).toEqual([
      ["message", String(first.id)],
      ["tool", "call-1"],
      ["message", String(second.id)],
    ]);
  });

  it("drops an unpaired tool call and pairs out-of-order results by callId", () => {
    const events = [
      toolCallEvent("call-1", "a", "{}"),
      toolCallEvent("call-2", "b", "{}"),
      toolResultEvent("call-2", [{ type: "text", text: "two" }]),
    ];

    expect(deriveUnits(events).map((unit) => unit.anchor)).toEqual(["call-2"]);
  });

  it("drops a tool-only assistant message (the tool unit carries its output)", () => {
    const toolOnly = createAssistantMessage({
      content: [{ type: "tool-call", id: CallId("call-1"), name: "a", arguments: "{}" }],
      source: { provider: "fake", model: "fake" },
    });
    const events = [
      speechEvent(toolOnly),
      toolCallEvent("call-1", "a", "{}"),
      toolResultEvent("call-1", [{ type: "text", text: "ok" }]),
    ];

    expect(deriveUnits(events).map((unit) => unit.kind)).toEqual(["tool"]);
  });

  it("drops a reasoning-only assistant message (think never broadcasts)", () => {
    const thinkingOnly = createAssistantMessage({
      content: [{ type: "reasoning", text: "chain of thought" }],
      source: { provider: "fake", model: "fake" },
    });

    expect(deriveUnits([speechEvent(thinkingOnly)])).toEqual([]);
  });
});

describe("consumedAnchors", () => {
  it("collects messageId only from team-broadcast user messages", () => {
    const injected = createUserMessage({
      content: [{ type: "text", text: "[player] hi" }],
      source: {
        kind: "team-broadcast",
        role: "player",
        senderSessionId: SENDER,
        messageId: MessageId("m-1"),
        form: "relay",
      },
    });
    const events = [
      userMessageEvent(injected),
      {
        type: "user/message",
        ...order(),
        data: { ...injected, source: { kind: "user" } },
      } as unknown as SessionEvent,
      { type: "assistant/message", ...order(), data: {} } as unknown as SessionEvent,
    ];

    expect([...consumedAnchors(events)]).toEqual(["m-1"]);
  });
});

describe("renderBroadcast", () => {
  it("renders a speech as the tag-wrapped verbatim body with no head line", () => {
    const message = speechMessage("line one\nline two");
    const text = renderBroadcast(
      "player",
      { kind: "message", anchor: String(message.id), time: 0, seq: 0 },
      [speechEvent(message)],
    );

    expect(text).toBe("<player-message>\nline one\nline two\n</player-message>");
  });

  it("renders a tool unit with the context line inside the wrapper and verbatim args/result", () => {
    const args = `{"operations":[${'{"type":"click","x":1,"y":2},'.repeat(3)}]}`;
    const result = "已揭示，周边 2 雷；剩余 38 格".repeat(40);
    const events = [
      toolCallEvent("call-1", "saolei_operate", args),
      toolResultEvent("call-1", [{ type: "text", text: result }]),
    ];

    const text = renderBroadcast(
      "player",
      { kind: "tool", anchor: "call-1", time: 0, seq: 0 },
      events,
      "game #3",
    );

    expect(text).toBe(
      `<player-tool-call>\ncontext: game #3\ntool: saolei_operate\n` +
        `args: ${args}\nresult: ${result}\n</player-tool-call>`,
    );
    expect(text).toContain(result);
  });

  it("omits the context line when the registration has none", () => {
    const events = [
      toolCallEvent("call-1", "saolei_remain", "{}"),
      toolResultEvent("call-1", [{ type: "text", text: "ok" }]),
    ];
    const text = renderBroadcast(
      "planner",
      { kind: "tool", anchor: "call-1", time: 0, seq: 0 },
      events,
    );
    expect(text).toBe(
      "<planner-tool-call>\ntool: saolei_remain\nargs: {}\nresult: ok\n</planner-tool-call>",
    );
  });

  it("never carries think content and states the body exactly once", () => {
    const message = createAssistantMessage({
      content: [
        { type: "reasoning", text: "hidden chain" },
        { type: "text", text: "visible answer" },
      ],
      source: { provider: "fake", model: "fake" },
    });
    const text = renderBroadcast(
      "player",
      { kind: "message", anchor: String(message.id), time: 0, seq: 0 },
      [speechEvent(message)],
    );

    expect(text).not.toContain("hidden chain");
    expect(text.match(/visible answer/g)).toHaveLength(1);
    expect(text).toBe("<player-message>\nvisible answer\n</player-message>");
  });

  it("fails loud when the sender log lacks the anchored production", () => {
    expect(() =>
      renderBroadcast("player", { kind: "message", anchor: "missing", time: 0, seq: 0 }, []),
    ).toThrow(/no assistant\/message/);
  });
});

describe("buildBroadcastMessage", () => {
  it("carries the team-broadcast provenance with a relay form and the anchor", () => {
    const message = speechMessage("hello");
    const userMessage = buildBroadcastMessage(
      "player",
      { kind: "message", anchor: String(message.id), time: 0, seq: 0 },
      SENDER,
      [speechEvent(message)],
      "game #3",
    );

    expect(userMessage.role).toBe("user");
    expect(userMessage.source).toEqual({
      kind: "team-broadcast",
      role: "player",
      senderSessionId: SENDER,
      messageId: String(message.id),
      form: "relay",
      context: "game #3",
    });
  });
});

describe("renderTeamSection", () => {
  const section = renderTeamSection({
    goal: "尽量高的胜率",
    members: [
      { role: "player", summary: "执行扫雷操作并独占桌面控制" },
      { role: "planner", summary: "复盘与制定策略，不操作桌面" },
    ],
  });

  it("renders the goal, the third-person roster, and the broadcast format convention", () => {
    expect(section).toContain("尽量高的胜率");
    expect(section).toContain("[player] 执行扫雷操作并独占桌面控制");
    expect(section).toContain("[planner] 复盘与制定策略，不操作桌面");
    expect(section).toContain("<角色-message>");
    expect(section).toContain("<角色-tool-call>");
    expect(section).toContain("context: 局 id");
    expect(section).not.toContain("[角色] 摘要");
    expect(section).not.toContain("[角色] 工具调用");
  });

  it("states no member's first-person identity (R2 ownership boundary)", () => {
    expect(section).not.toContain("你是 player");
    expect(section).not.toContain("你是 planner");
  });
});
