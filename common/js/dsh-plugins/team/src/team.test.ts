/**
 * TeamService tests: section registration and refresh (order band, content,
 * no first-person identity), idempotent registration, scope/agent cleanup,
 * reference relay (anchor order, sender exclusion, transient relay removal),
 * drain rendering and consumption marks, and derived rebuild (self-heal /
 * exactly-once). Contract: specs/059-agent-v2-team-mode/contracts/dsh-plugins.md
 * §1; derived model: survey/deepseek-harness-team-mode.md §4.4a.
 *
 * Pattern (style/javascript.md Mock convention): the host is a real cordis
 * Context; member agent scopes are captured doubles for `systemPrompt.section`
 * and `on` (recording sections and listeners), and tests drive the dsh event
 * sequence by pushing into the member session log and invoking the captured
 * listeners — no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import { createScope } from "@deepseek-ai/dsh-scope";
import {
  CallId,
  createAssistantMessage,
  createToolResultMessage,
} from "@deepseek-ai/dsh-llm";
import type {
  AssistantMessage,
  ContentBlock,
  UserMessage,
} from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import { describe, expect, it, vi } from "vitest";

import { renderTeamSection } from "./section.js";
import {
  TEAM_SECTION_NAME,
  TEAM_SECTION_ORDER,
  TeamService,
} from "./team.js";

interface SectionRecord {
  readonly name: string;
  readonly order: number;
  readonly text: string | ((context: unknown) => string);
  readonly dispose: ReturnType<typeof vi.fn>;
}

type Listener = (session: { id: unknown }, event: SessionEvent) => void;

interface FakeMember {
  readonly id: ReturnType<typeof SessionId>;
  readonly agent: Agent;
  readonly handle: AgentHandle;
  readonly events: SessionEvent[];
  readonly sections: SectionRecord[];
  readonly listeners: Listener[];
  readonly offs: Array<ReturnType<typeof vi.fn>>;
}

function createMember(name: string): FakeMember {
  const id = SessionId(name);
  const events: SessionEvent[] = [];
  const sections: SectionRecord[] = [];
  const listeners: Listener[] = [];
  const offs: Array<ReturnType<typeof vi.fn>> = [];
  const ctx = {
    systemPrompt: {
      section: vi.fn((value: { name: string; order: number; text: string }) => {
        const dispose = vi.fn();
        sections.push({ ...value, dispose });
        return dispose;
      }),
    },
    on: vi.fn((_name: string, listener: Listener) => {
      listeners.push(listener);
      const off = vi.fn(() => {
        const index = listeners.indexOf(listener);
        if (index >= 0) {
          listeners.splice(index, 1);
        }
      });
      offs.push(off);
      return off;
    }),
  };
  const session = { id, events };
  const agent = { id, session, ctx } as unknown as Agent;
  const handle: AgentHandle = { agent, dispose: vi.fn(async () => {}) };
  return { id, agent, handle, events, sections, listeners, offs };
}

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

function speechEvent(text: string): { event: SessionEvent; message: AssistantMessage } {
  const message = speechMessage(text);
  return {
    event: {
      type: "assistant/message",
      ...order(),
      data: { turn: 1, step: 1, message },
    } as unknown as SessionEvent,
    message,
  };
}

function toolCallEvent(callId: string, name: string, args: string): SessionEvent {
  return {
    type: "tool/call",
    ...order(),
    data: { turn: 1, step: 1, callId: CallId(callId), name, arguments: args },
  } as unknown as SessionEvent;
}

function toolResultEvent(callId: string, result: string): SessionEvent {
  return {
    type: "tool/result",
    ...order(),
    data: {
      turn: 1,
      step: 1,
      message: createToolResultMessage({
        callId: CallId(callId),
        content: [{ type: "text", text: result }],
        isError: false,
      }),
    },
  } as unknown as SessionEvent;
}

/** Push into the log and invoke the captured member listeners (a live event). */
function emit(member: FakeMember, event: SessionEvent): void {
  member.events.push(event);
  for (const listener of [...member.listeners]) {
    listener(member.agent.session as unknown as { id: unknown }, event);
  }
}

/** Push into the log only (a relay the subscription never saw). */
function append(member: FakeMember, event: SessionEvent): void {
  member.events.push(event);
}

/** A speech production event plus its log-only append. */
function produceSpeech(member: FakeMember, text: string): AssistantMessage {
  const { event, message } = speechEvent(text);
  emit(member, event);
  return message;
}

/** A paired tool production: the call and its result, both live. */
function produceTool(member: FakeMember, callId: string, result: string): void {
  emit(member, toolCallEvent(callId, "saolei_operate", `{"call":"${callId}"}`));
  emit(member, toolResultEvent(callId, result));
}

/** The consumption closure: the injected broadcast lands in the receiver log. */
function consume(member: FakeMember, message: UserMessage): void {
  append(member, {
    type: "user/message",
    ...order(),
    data: message,
  } as unknown as SessionEvent);
}

function textOf(message: UserMessage): string {
  const block: ContentBlock | undefined = message.content[0];
  return block !== undefined && block.type === "text" ? block.text : "";
}

function createTeam(): { ctx: Context; team: TeamService } {
  const ctx = new Context();
  return { ctx, team: new TeamService(ctx) };
}

describe("TeamService.register", () => {
  it("mounts as the class-form plugin row and exposes ctx.team", async () => {
    const ctx = new Context();
    const fiber = await ctx.plugin(TeamService);

    expect(ctx.team).toBeInstanceOf(TeamService);

    await fiber.dispose();
  });

  it("registers the shared team section on each member's agent scope with the roster and format convention", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    team.register({
      goal: "尽量高的胜率",
      context: "game #3",
      members: [
        { agent: player.handle, role: "player", summary: "执行操作并独占桌面控制" },
        { agent: planner.handle, role: "planner", summary: "复盘与制定策略，不操作" },
      ],
    });

    const expected = renderTeamSection({
      goal: "尽量高的胜率",
      members: [
        { role: "player", summary: "执行操作并独占桌面控制" },
        { role: "planner", summary: "复盘与制定策略，不操作" },
      ],
    });
    for (const member of [player, planner]) {
      expect(member.sections).toHaveLength(1);
      const section = member.sections[0];
      expect(section?.name).toBe(TEAM_SECTION_NAME);
      expect(section?.order).toBe(TEAM_SECTION_ORDER);
      expect(section?.order).toBeGreaterThanOrEqual(1);
      expect(section?.order).toBeLessThanOrEqual(49);
      expect(section?.text).toBe(expected);
      expect(section?.text).toContain("[player] 执行操作并独占桌面控制");
      expect(section?.text).toContain("[planner] 复盘与制定策略，不操作");
      expect(section?.text).not.toContain("你是 player");
      expect(section?.text).not.toContain("你是 planner");
      expect(member.listeners).toHaveLength(1);
    }
  });

  it("is idempotent: a repeated registration keeps one section and one listener per member", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    const members = [
      { agent: player.handle, role: "player", summary: "执行操作" },
      { agent: planner.handle, role: "planner", summary: "制定策略" },
    ];
    const first = team.register({ goal: "目标一", members });
    const second = team.register({ goal: "目标一", members });

    expect(player.sections).toHaveLength(1);
    expect(planner.sections).toHaveLength(1);
    expect(player.sections[0]?.dispose).not.toHaveBeenCalled();
    expect(player.listeners).toHaveLength(1);

    second.dispose();
    expect(player.sections[0]?.dispose).toHaveBeenCalledOnce();
    expect(player.offs[0]).toHaveBeenCalledOnce();
    expect(() => team.drain(player.handle)).toThrow(/not a registered member/);
    first.dispose();
  });

  it("re-registers the section only when the team facts changed", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const members = [{ agent: player.handle, role: "player", summary: "执行操作" }];
    team.register({ goal: "目标一", members });
    team.register({ goal: "目标二", members });

    expect(player.sections).toHaveLength(2);
    expect(player.sections[0]?.dispose).toHaveBeenCalledOnce();
    expect(player.sections[1]?.text).toContain("目标二");
    expect(player.listeners).toHaveLength(1);
  });

  it("drops a member that is absent from a later registration", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    team.register({
      goal: "g",
      members: [
        { agent: player.handle, role: "player", summary: "a" },
        { agent: planner.handle, role: "planner", summary: "b" },
      ],
    });
    team.register({
      goal: "g",
      members: [{ agent: player.handle, role: "player", summary: "a" }],
    });

    expect(planner.sections[0]?.dispose).toHaveBeenCalledOnce();
    expect(() => team.drain(planner.handle)).toThrow(/not a registered member/);
    expect(team.drain(player.handle)).toEqual([]);
  });

  it("drops the member entries and buffers on agent disposal", () => {
    const { ctx, team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    team.register({
      goal: "g",
      members: [
        { agent: player.handle, role: "player", summary: "a" },
        { agent: planner.handle, role: "planner", summary: "b" },
      ],
    });

    ctx.emit("agent/disposed", { agent: player.agent });

    expect(player.sections[0]?.dispose).toHaveBeenCalledOnce();
    expect(player.offs[0]).toHaveBeenCalledOnce();
    expect(() => team.drain(player.handle)).toThrow(/not a registered member/);
    expect(team.drain(planner.handle)).toEqual([]);
  });

  it("unwinds the section with the member agent scope (cordis scope cleanup)", async () => {
    const ctx = new Context();
    const sectionDispose = vi.fn();
    const scope = createScope(ctx, { member: "player" });
    const section = vi.fn(() => scope.ctx.effect(() => () => sectionDispose()));
    ctx.provide("systemPrompt", { section } as never);
    const id = SessionId("templates/saolei/sessions/s1/player");
    const agent = {
      id,
      session: { id, events: [] },
      ctx: scope.ctx,
    } as unknown as Agent;
    const team = new TeamService(ctx);
    team.register({
      goal: "g",
      members: [
        { agent: { agent, dispose: vi.fn(async () => {}) }, role: "player", summary: "a" },
      ],
    });

    expect(section).toHaveBeenCalledOnce();
    await scope.dispose();
    expect(sectionDispose).toHaveBeenCalledOnce();
  });
});

describe("TeamService.drain", () => {
  it("delivers each member's speech and paired tool units to every other member in production order", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    const observer = createMember("templates/saolei/sessions/s1/observer");
    team.register({
      goal: "尽量高的胜率",
      context: "game #3",
      members: [
        { agent: player.handle, role: "player", summary: "执行操作" },
        { agent: planner.handle, role: "planner", summary: "制定策略" },
        { agent: observer.handle, role: "observer", summary: "旁观" },
      ],
    });

    const first = produceSpeech(player, "我将点击中心");
    produceTool(player, "call-1", "已揭示，周边 2 雷");
    produceSpeech(player, "继续");

    expect(team.drain(player.handle)).toEqual([]);
    const plannerView = team.drain(planner.handle);
    expect(plannerView).toHaveLength(3);
    expect(textOf(plannerView[0]!)).toBe(
      "[player] 我将点击中心\n<player-message>\n我将点击中心\n</player-message>",
    );
    expect(plannerView[0]?.source).toMatchObject({
      kind: "team-broadcast",
      role: "player",
      senderSessionId: player.id,
      messageId: String(first.id),
      form: "relay",
      context: "game #3",
    });
    expect(textOf(plannerView[1]!)).toBe(
      '[player] 工具调用 saolei_operate (game #3)\n' +
        '<player-tool-call>\ntool: saolei_operate\nargs: {"call":"call-1"}\n' +
        "result: 已揭示，周边 2 雷\n</player-tool-call>",
    );
    expect(plannerView[1]?.source).toMatchObject({ messageId: "call-1" });
    expect(textOf(plannerView[2]!)).toContain("继续");

    expect(team.drain(observer.handle)).toHaveLength(3);
  });

  it("does not relay an incomplete tool call and drops the transient entry once the result lands", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    team.register({
      goal: "g",
      members: [
        { agent: player.handle, role: "player", summary: "a" },
        { agent: planner.handle, role: "planner", summary: "b" },
      ],
    });

    emit(player, toolCallEvent("call-1", "saolei_operate", '{"x":1}'));
    expect(team.drain(planner.handle)).toEqual([]);

    emit(player, toolResultEvent("call-1", "clicked"));
    const units = team.drain(planner.handle);
    expect(units).toHaveLength(1);
    expect(textOf(units[0]!)).toContain("result: clicked");
    expect(team.drain(planner.handle)).toEqual([]);
  });

  it("marks drained anchors consumed: a second drain before the injection lands is empty", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    team.register({
      goal: "g",
      members: [
        { agent: player.handle, role: "player", summary: "a" },
        { agent: planner.handle, role: "planner", summary: "b" },
      ],
    });

    produceSpeech(player, "one");
    expect(team.drain(planner.handle)).toHaveLength(1);
    expect(team.drain(planner.handle)).toEqual([]);
  });

  it("does not permanently exclude a unit whose message construction failed (retry on the next drain)", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    team.register({
      goal: "g",
      members: [
        { agent: player.handle, role: "player", summary: "a" },
        { agent: planner.handle, role: "planner", summary: "b" },
      ],
    });
    produceSpeech(player, "retry me");

    // A transient build failure: the seam stands in for a renderer fault
    // while the sender log stays intact. The failed unit must stay
    // un-consumed, so the rebuild on the next drain returns it.
    interface BuildSeam {
      buildMessage: (unit: unknown, team: unknown) => UserMessage;
    }
    const seam = team as unknown as BuildSeam;
    const failing = vi.fn(() => {
      throw new Error("team broadcast: transient build failure");
    });
    seam.buildMessage = failing;
    expect(() => team.drain(planner.handle)).toThrow(/transient build failure/);
    expect(failing).toHaveBeenCalledOnce();

    delete (team as unknown as { buildMessage?: unknown }).buildMessage;
    const retried = team.drain(planner.handle);
    expect(retried).toHaveLength(1);
    expect(textOf(retried[0]!)).toContain("retry me");
  });

  it("rebuilds from sender logs: a lost relay self-heals, consumed anchors stay single", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    const members = [
      { agent: player.handle, role: "player", summary: "a" },
      { agent: planner.handle, role: "planner", summary: "b" },
    ];
    team.register({ goal: "g", members });

    // A relay the subscription never saw (log-only append) is restored.
    const missed = speechEvent("missed");
    append(player, missed.event);
    const first = team.drain(planner.handle);
    expect(first).toHaveLength(1);
    expect(textOf(first[0]!)).toContain("missed");

    // The durable consumption anchor excludes the delivered broadcast even
    // after a fresh registration rebuilds the pending list.
    consume(planner, first[0]!);
    team.register({ goal: "g", members });
    expect(team.drain(planner.handle)).toEqual([]);

    // The next lost relay is still picked up (the consumed anchor did not
    // wedge the member).
    const next = speechEvent("next");
    append(player, next.event);
    const second = team.drain(planner.handle);
    expect(second).toHaveLength(1);
    expect(textOf(second[0]!)).toContain("next");
  });

  it("keeps a drained-but-unlogged anchor excluded across a rebuild", () => {
    const { team } = createTeam();
    const player = createMember("templates/saolei/sessions/s1/player");
    const planner = createMember("templates/saolei/sessions/s1/planner");
    const members = [
      { agent: player.handle, role: "player", summary: "a" },
      { agent: planner.handle, role: "planner", summary: "b" },
    ];
    team.register({ goal: "g", members });

    produceSpeech(player, "one");
    expect(team.drain(planner.handle)).toHaveLength(1);
    // No consumption closure yet: the drain mark must still suppress it.
    team.register({ goal: "g", members });
    expect(team.drain(planner.handle)).toEqual([]);
  });

  it("rejects a member that was never registered", () => {
    const { team } = createTeam();
    const stranger = createMember("templates/saolei/sessions/s1/stranger");
    expect(() => team.drain(stranger.handle)).toThrow(/not a registered member/);
  });
});
