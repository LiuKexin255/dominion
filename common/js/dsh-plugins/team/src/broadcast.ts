/**
 * Broadcast units and their rendered form: a member's logged output becomes a
 * 1:1 relay unit (one speech, or one paired tool call+result), and drain
 * renders that unit into the injection-ready group-chat text. Anchor identity
 * and the read-back-from-log model are
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §1 (items 2–5); the
 * wire format is the single tag-pair form of
 * specs/060-agent-v2-team-optimize/contracts/team-api.md §4 (no head line;
 * think never enters the broadcast).
 *
 * Content is never copied into a unit: a unit is the anchor plus its sender-log
 * order key, and every render reads the actual events back from the sender's
 * session log (survey §4.4a reference model).
 *
 * dsh-llm message/message-source shapes:
 * https://unpkg.com/@deepseek-ai/dsh-llm@0.1.1-rc.2/lib/types/message.d.ts
 */

import { createUserMessage, MessageId } from "@deepseek-ai/dsh-llm";
import type {
  AssistantMessage,
  ContentBlock,
  ToolResultMessage,
  UserMessage,
} from "@deepseek-ai/dsh-llm";
import type { SessionEvent, SessionId } from "@deepseek-ai/dsh-session";

/**
 * Durable provenance of an injected broadcast: `MessageSourceMap`'s merge
 * extension (declaration merge, official `subagent-settled` precedent —
 * https://unpkg.com/@deepseek-ai/dsh-llm@0.1.1-rc.2/lib/types/message.d.ts).
 * The `form: 'relay'` reuses the official "a message another agent addressed
 * to this one" form; `messageId` doubles as the 1:1 correlation and
 * consumption anchor — the original speech's MessageId, or the tool call's
 * CallId for a tool unit (contracts/dsh-plugins.md §1 item 6; survey §5.3).
 */
export interface TeamBroadcastSource {
  readonly kind: "team-broadcast";
  readonly role: string;
  readonly senderSessionId: SessionId;
  readonly messageId: MessageId;
  readonly context?: string;
  readonly form: "relay";
}

declare module "@deepseek-ai/dsh-llm" {
  interface MessageSourceMap {
    "team-broadcast": TeamBroadcastSource;
  }
}

/**
 * One broadcast unit anchor: a speech (assistant message id) or a completed
 * tool unit (the tool call id pairing one `tool/call` with its `tool/result`).
 */
export interface BroadcastUnit {
  /** Which logged production the anchor names. */
  readonly kind: "message" | "tool";
  /** Stable anchor string (MessageId for a speech, CallId for a tool unit). */
  readonly anchor: string;
  /** Sender-log event time of the producing event (cross-member order key). */
  readonly time: number;
  /** Sender-log seq of the producing event (intra-log tie-breaker). */
  readonly seq: number;
}

/**
 * The role-derived wrapper tag stem (`<player-message>`, `<player-tool-call>`).
 * Role is an open string, so the stem is normalized to a safe tag token; the
 * team section declares this vocabulary to every member.
 */
export function tagName(role: string): string {
  const normalized = role
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return normalized === "" ? "member" : normalized;
}

/**
 * The speech text of one assistant message: its text blocks in order.
 * Tool-call blocks are excluded because the paired tool unit carries the
 * complete invocation (args + result) verbatim — including them here would
 * duplicate the same arguments; reasoning is excluded because the broadcast
 * carries no think content
 * (specs/060-agent-v2-team-optimize/contracts/team-api.md §4).
 */
export function messageBody(message: AssistantMessage): string {
  const parts: string[] = [];
  for (const block of message.content) {
    if (block.type === "text") {
      parts.push(block.text);
    }
  }
  return parts.join("\n\n");
}

/**
 * The result text of one tool-result message: its nested result blocks in
 * order — text verbatim; any other block type keeps its lossless JSON form so
 * the render never silently drops content
 * (specs/060-agent-v2-team-optimize/contracts/team-api.md §4: 全文，不截断、不摘要).
 */
export function toolResultBody(message: ToolResultMessage): string {
  return (message.content[0]?.content ?? [])
    .map((block: ContentBlock) =>
      block.type === "text" ? block.text : JSON.stringify(block),
    )
    .join("\n");
}

/**
 * Derive the ordered broadcast units of one sender log. Speeches take their
 * own event position; a tool unit takes the position of its `tool/call` (the
 * model's output order — results commit in model order, dsh-tools scheduler).
 * An unpaired call produces no unit (the unit is complete only with its
 * result), and an assistant message whose text content is empty produces none
 * either (its tool units carry the whole output).
 */
export function deriveUnits(events: readonly SessionEvent[]): BroadcastUnit[] {
  const units: BroadcastUnit[] = [];
  const calls = new Map<string, { time: number; seq: number }>();
  for (const event of events) {
    if (event.type === "assistant/message") {
      if (messageBody(event.data.message) === "") {
        continue;
      }
      units.push({
        kind: "message",
        anchor: String(event.data.message.id),
        time: event.time,
        seq: event.seq,
      });
      continue;
    }
    if (event.type === "tool/call") {
      calls.set(String(event.data.callId), { time: event.time, seq: event.seq });
      continue;
    }
    if (event.type === "tool/result") {
      const callId = event.data.message.content[0]?.toolCallId;
      if (callId === undefined) {
        continue;
      }
      const call = calls.get(String(callId));
      if (call === undefined) {
        continue;
      }
      calls.delete(String(callId));
      units.push({ kind: "tool", anchor: String(callId), time: call.time, seq: call.seq });
    }
  }
  return units;
}

/**
 * The anchors a receiver log already consumed: the `messageId` of every
 * `user/message` carrying a `team-broadcast` source (contracts/dsh-plugins.md
 * §1 item 5 — the durable consumption closure).
 */
export function consumedAnchors(events: readonly SessionEvent[]): Set<string> {
  const anchors = new Set<string>();
  for (const event of events) {
    if (event.type !== "user/message") {
      continue;
    }
    const source = event.data.source;
    if (source.kind !== "team-broadcast") {
      continue;
    }
    anchors.add(String(source.messageId));
  }
  return anchors;
}

/** Cross-member unit order: event time first, sender-log seq as tie-breaker. */
export function compareUnits(a: BroadcastUnit, b: BroadcastUnit): number {
  return a.time - b.time || a.seq - b.seq;
}

function findAssistantMessage(
  events: readonly SessionEvent[],
  anchor: string,
): AssistantMessage | undefined {
  for (const event of events) {
    if (event.type === "assistant/message" && String(event.data.message.id) === anchor) {
      return event.data.message;
    }
  }
  return undefined;
}

function findToolCall(
  events: readonly SessionEvent[],
  anchor: string,
): { name: string; arguments: string } | undefined {
  for (const event of events) {
    if (event.type === "tool/call" && String(event.data.callId) === anchor) {
      return { name: event.data.name, arguments: event.data.arguments };
    }
  }
  return undefined;
}

function findToolResult(
  events: readonly SessionEvent[],
  anchor: string,
): ToolResultMessage | undefined {
  for (const event of events) {
    if (
      event.type === "tool/result" &&
      String(event.data.message.content[0]?.toolCallId) === anchor
    ) {
      return event.data.message;
    }
  }
  return undefined;
}

/**
 * Render one unit into the group-chat wire form
 * (specs/060-agent-v2-team-optimize/contracts/team-api.md §4):
 * one tag pair around the verbatim body, no head line — a speech is the
 * `<role-message>` pair; a tool unit is the `<role-tool-call>` pair around the
 * optional `context:` line and the `tool:`/`args:`/`result:` lines, args and
 * result verbatim.
 *
 * A missing source event is corruption (the anchor names a logged fact), so
 * reading fails loud rather than fabricating content.
 */
export function renderBroadcast(
  role: string,
  unit: BroadcastUnit,
  senderEvents: readonly SessionEvent[],
  context?: string,
): string {
  const tag = tagName(role);
  if (unit.kind === "message") {
    const message = findAssistantMessage(senderEvents, unit.anchor);
    if (message === undefined) {
      throw new Error(
        `team broadcast: sender log has no assistant/message for anchor "${unit.anchor}"`,
      );
    }
    const body = messageBody(message);
    return `<${tag}-message>\n${body}\n</${tag}-message>`;
  }
  const call = findToolCall(senderEvents, unit.anchor);
  const result = findToolResult(senderEvents, unit.anchor);
  if (call === undefined || result === undefined) {
    throw new Error(
      `team broadcast: sender log has no complete tool unit for anchor "${unit.anchor}"`,
    );
  }
  const lines = [
    ...(context === undefined ? [] : [`context: ${context}`]),
    `tool: ${call.name}`,
    `args: ${call.arguments}`,
    `result: ${toolResultBody(result)}`,
  ];
  return `<${tag}-tool-call>\n${lines.join("\n")}\n</${tag}-tool-call>`;
}

/**
 * Build the injection-ready broadcast user message for one unit: rendered
 * text plus the durable `team-broadcast` provenance (`form: 'relay'` — the
 * official "another agent addressed this one" form) whose `messageId` doubles
 * as the 1:1 correlation and consumption anchor (survey §5.3 layer 2).
 */
export function buildBroadcastMessage(
  role: string,
  unit: BroadcastUnit,
  senderSessionId: SessionId,
  senderEvents: readonly SessionEvent[],
  context?: string,
): UserMessage {
  return createUserMessage({
    content: [{ type: "text", text: renderBroadcast(role, unit, senderEvents, context) }],
    source: {
      kind: "team-broadcast",
      role,
      senderSessionId,
      messageId: MessageId(unit.anchor),
      form: "relay",
      ...(context === undefined ? {} : { context }),
    },
  });
}
