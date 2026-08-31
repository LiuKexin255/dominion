/**
 * history.ts — per-session conversation history and the dsh→ChatEvent turn
 * collector (specs/049-agent-v2-dsh-init/data-model.md §2.4/§2.5).
 *
 * The collector is armed once per session entry (session lifetime, not stream
 * lifetime — specs/049-agent-v2-dsh-init/research.md D10-2) and maps dsh
 * turn events to proto ChatEvents for the active turn's stream; the mapping
 * table is specs/049-agent-v2-dsh-init/contracts/conversation-api.md §4.
 * `assistant/message` appends the final content blocks to the in-memory
 * history (FR-014), which stays collected even when no stream is attached.
 */

import { randomUUID } from "node:crypto";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { ContentBlock } from "../agent_v2_types/projects/game/v2/ContentBlock.js";
import type { HistoryMessage } from "../agent_v2_types/projects/game/v2/HistoryMessage.js";
import type { Timestamp } from "../agent_v2_types/google/protobuf/Timestamp.js";
import type { BlockStartEvent } from "../agent_v2_types/projects/game/v2/BlockStartEvent.js";
import type { BlockDeltaEvent } from "../agent_v2_types/projects/game/v2/BlockDeltaEvent.js";
import type { BlockEndEvent } from "../agent_v2_types/projects/game/v2/BlockEndEvent.js";
import type { DshContext } from "./dsh.js";

/**
 * Structural subset of a dsh `session/event` payload read by the collector
 * (same pattern as the demo agent; upstream shape anchored at
 * /tmp/opencode/dsh/packages/core/agent-loop/src/agent.ts —
 * `assistant/chunk` = `{turn, step, chunk}` with chunk a raw StreamChunk).
 */
export interface DshSessionEvent {
  type: string;
  data?: unknown;
}

/** dsh StreamChunk structural subset (@deepseek-ai/dsh-llm lib/types/types.d.ts). */
export interface DshStreamChunk {
  type: string;
  index?: number;
  text?: string;
  blockType?: string;
  id?: string;
  name?: string;
  argumentsDelta?: string;
  block?: DshContentBlockView;
  usage?: DshTokenUsage;
}

/** dsh content block structural subset (ContentBlockMap in dsh-llm types). */
export interface DshContentBlockView {
  type: string;
  text?: string;
  id?: string;
  name?: string;
  arguments?: string;
}

/** dsh TokenUsage structural subset. */
export interface DshTokenUsage {
  inputTokens: number;
  outputTokens: number;
  reasoningTokens?: number;
}

/** The `assistant/message` event shape the history is appended from. */
export interface AssistantMessageEvent extends DshSessionEvent {
  type: "assistant/message";
  data: {
    message: { content: ReadonlyArray<DshContentBlockView> };
    usage?: DshTokenUsage;
  };
}

/**
 * The server-streaming write end of one Send call. server.ts adapts the
 * grpc call to this interface; tests inject plain recorders.
 */
export interface TurnStream {
  write(event: ChatEvent): void;
  end(): void;
}

/** How a turn ended: COMPLETED on idle, ERROR on failure, ABORTED on dispose. */
export interface TurnOutcome {
  status: "COMPLETED" | "ERROR" | "ABORTED";
  error?: { code: string; message: string };
}

/** A settled turn outcome plus the usage folded into turn_end (§4). */
export interface TurnSettlement extends TurnOutcome {
  usage: DshTokenUsage | undefined;
}

/** dsh chunk vocabulary → ChatEvent BlockType (conversation-api.md §4). */
const BLOCK_TYPES: Record<string, "BLOCK_TYPE_TEXT" | "BLOCK_TYPE_THINK" | "BLOCK_TYPE_TOOL_CALL"> = {
  text: "BLOCK_TYPE_TEXT",
  reasoning: "BLOCK_TYPE_THINK",
  "tool-call": "BLOCK_TYPE_TOOL_CALL",
};

function nowTimestamp(): Timestamp {
  const ms = Date.now();
  return { seconds: Math.floor(ms / 1000), nanos: (ms % 1000) * 1e6 };
}

/**
 * Map one dsh content block to a proto ContentBlock; unknown block types
 * (image/tool-result) have no display projection in this phase and yield
 * undefined (forward-compat drop; FR-006 zero tools).
 */
export function blockToContentBlock(block: DshContentBlockView): ContentBlock | undefined {
  if (block.type === "text") {
    return { text: { content: block.text ?? "" } };
  }
  if (block.type === "reasoning") {
    return { think: { content: block.text ?? "" } };
  }
  if (block.type === "tool-call") {
    // No terminal status source in this phase: the block is surfaced as
    // RUNNING (conversation-api.md §4; US3 validates richer states with
    // constructed data).
    return {
      toolCall: {
        toolId: block.id ?? "",
        name: block.name ?? "",
        argsJson: block.arguments ?? "",
        status: "TOOL_STATUS_RUNNING",
      },
    };
  }
  return undefined;
}

/**
 * Map a streamed dsh chunk to a ChatEvent payload event. `usage` chunks are
 * folded into turn_end by the collector (never a standalone frame) and
 * `finish` chunks are ignored — turn termination is driven by
 * agent/status→idle (specs/047-dsh-chat-demo/research.md D3), so both return
 * undefined here.
 */
export function chunkToChatEvent(
  chunk: DshStreamChunk,
  sessionName: string,
  turnId: string,
): ChatEvent | undefined {
  if (chunk.type === "block-start") {
    // Unknown block types have no display projection in this phase and are
    // dropped — the same forward-compat policy as blockToContentBlock.
    const blockType = BLOCK_TYPES[chunk.blockType ?? ""];
    if (blockType === undefined) {
      return undefined;
    }
    const start: BlockStartEvent = {
      index: chunk.index ?? 0,
      type: blockType,
    };
    if (chunk.blockType === "tool-call") {
      start.toolId = chunk.id ?? "";
      start.name = chunk.name ?? "";
    }
    return { session: sessionName, turnId, blockStart: start };
  }
  if (chunk.type === "text-delta" || chunk.type === "reasoning-delta") {
    const delta: BlockDeltaEvent = { index: chunk.index ?? 0, text: chunk.text ?? "" };
    return { session: sessionName, turnId, delta };
  }
  if (chunk.type === "tool-call-delta") {
    const delta: BlockDeltaEvent = { index: chunk.index ?? 0, text: chunk.argumentsDelta ?? "" };
    return { session: sessionName, turnId, delta };
  }
  if (chunk.type === "block-end") {
    const block = chunk.block ? blockToContentBlock(chunk.block) : undefined;
    if (block === undefined) {
      return undefined;
    }
    const end: BlockEndEvent = { index: chunk.index ?? 0, block };
    return { session: sessionName, turnId, blockEnd: end };
  }
  return undefined;
}

/**
 * Per-session in-memory conversation record (FR-014): user messages are
 * appended at enqueue time, agent replies at `assistant/message` finality;
 * ordering and text/think/tool-call classification are preserved
 * (specs/049-agent-v2-dsh-init/data-model.md §2.5).
 */
export class SessionHistory {
  private messages: HistoryMessage[] = [];
  private nextSeq = 1;

  /** Append the user turn recorded when its message is enqueued. */
  appendUser(text: string): void {
    this.messages.push({
      messageId: `m${this.nextSeq}`,
      role: "ROLE_USER",
      createTime: nowTimestamp(),
      blocks: [{ text: { content: text } }],
    });
    this.nextSeq += 1;
  }

  /** Append the agent reply from the round's final assistant message. */
  appendAssistant(content: ReadonlyArray<DshContentBlockView>): void {
    const blocks: ContentBlock[] = [];
    for (const block of content) {
      const mapped = blockToContentBlock(block);
      if (mapped !== undefined) {
        blocks.push(mapped);
      }
    }
    this.messages.push({
      messageId: `m${this.nextSeq}`,
      role: "ROLE_AGENT",
      createTime: nowTimestamp(),
      blocks,
    });
    this.nextSeq += 1;
  }

  /** Snapshot of the history for ListAgentMessages (defensive copy). */
  list(): HistoryMessage[] {
    return [...this.messages];
  }
}

interface ActiveTurn {
  turnId: string;
  stream: TurnStream;
  usage: DshTokenUsage | undefined;
  failure: { code: string; message: string } | undefined;
}

/**
 * Session-lifetime dsh event collector: owns the `session/event`,
 * `agent/status`, and `agent/error` subscriptions for one session entry and
 * forwards the active turn's mapped events to its sink.
 *
 * Turn termination follows the demo agent's collection pattern
 * (specs/047-dsh-chat-demo/research.md D3): failures are recorded from
 * `agent/error` / `turn/end{error}`, and the agent/status→idle transition
 * settles the turn — ERROR when a failure was observed, otherwise COMPLETED
 * with the usage folded in (conversation-api.md §4).
 */
export class TurnCollector {
  private active: ActiveTurn | undefined;
  private settle: ((settlement: TurnSettlement) => void) | undefined;
  /** Settlement observed before awaitSettled registered (idle can race the await). */
  private settlement: TurnSettlement | undefined;
  private pendingAbort = false;
  private readonly off: Array<() => void> = [];

  constructor(
    private readonly ctx: DshContext,
    private readonly agent: Agent,
    private readonly sessionName: string,
    private readonly history: SessionHistory,
  ) {
    this.off.push(
      ctx.on("session/event", (session, event) => {
        this.onSessionEvent(session.id, event as unknown as DshSessionEvent);
      }),
    );
    this.off.push(
      ctx.on("agent/status", (payload) => {
        this.onStatus(payload);
      }),
    );
    this.off.push(
      ctx.on("agent/error", (payload) => {
        this.onError(payload);
      }),
    );
  }

  /** Start collecting for a turn: mapped events stream to `stream`. */
  begin(turnId: string, stream: TurnStream): void {
    this.active = { turnId, stream, usage: undefined, failure: undefined };
    this.settlement = undefined;
    this.pendingAbort = false;
  }

  /**
   * Resolve when the turn settles (idle→COMPLETED/ERROR, abort→ABORTED).
   * A settlement (or abort) that raced ahead of the call resolves
   * immediately — the dsh lifecycle can settle the turn before the runner's
   * await registers.
   */
  awaitSettled(): Promise<TurnSettlement> {
    if (this.pendingAbort) {
      return Promise.resolve({ status: "ABORTED", usage: undefined });
    }
    if (this.settlement !== undefined) {
      return Promise.resolve(this.settlement);
    }
    return new Promise((resolve) => {
      this.settle = resolve;
    });
  }

  /**
   * Dispose path: stop forwarding and settle the in-flight turn as ABORTED.
   * Returns the in-flight turn's stream and turn id (so the caller can
   * deliver the turn_end{ABORTED} frame), or undefined when no turn is
   * running — an already-settled turn must not receive a spurious ABORTED.
   */
  abort(): { stream: TurnStream; turnId: string } | undefined {
    const inFlight = this.active;
    if (inFlight === undefined) {
      return undefined;
    }
    this.active = undefined;
    this.pendingAbort = true;
    this.resolve({ status: "ABORTED", usage: undefined });
    return { stream: inFlight.stream, turnId: inFlight.turnId };
  }

  /** Unsubscribe the session-lifetime listeners (process shutdown). */
  dispose(): void {
    for (const off of this.off) {
      off();
    }
    this.off.length = 0;
  }

  private resolve(settlement: TurnSettlement): void {
    const settle = this.settle;
    this.settle = undefined;
    if (settle !== undefined) {
      settle(settlement);
      return;
    }
    // No waiter yet: cache so a later awaitSettled observes the outcome.
    this.settlement = settlement;
  }

  private onSessionEvent(dshSessionId: string, event: DshSessionEvent): void {
    if (dshSessionId !== this.agent.session.id) {
      return;
    }
    if (event.type === "assistant/message") {
      // History collection is session-lifetime: the final blocks are
      // recorded even when no stream is attached (or already detached), so
      // refresh backfill stays consistent (research.md D10-2).
      const message = (event as AssistantMessageEvent).data.message;
      if (this.active !== undefined) {
        this.active.usage = (event as AssistantMessageEvent).data.usage ?? this.active.usage;
      }
      this.history.appendAssistant(message.content);
      return;
    }
    if (!this.active) {
      return;
    }
    if (event.type === "assistant/chunk") {
      this.onChunk(event.data as { chunk?: DshStreamChunk } | undefined);
      return;
    }
    if (event.type === "turn/end") {
      const reason = (event.data as { reason?: { kind?: string; error?: DshLlmFailureView } })
        ?.reason;
      if (reason?.kind === "error") {
        this.recordFailure(reason.error);
      }
    }
  }

  private onChunk(data: { chunk?: DshStreamChunk } | undefined): void {
    const chunk = data?.chunk;
    if (!chunk || !this.active) {
      return;
    }
    if (chunk.type === "usage") {
      // Folded into turn_end.usage; never a standalone frame (§4).
      this.active.usage = chunk.usage;
      return;
    }
    const chatEvent = chunkToChatEvent(chunk, this.sessionName, this.active.turnId);
    if (chatEvent !== undefined) {
      this.active.stream.write(chatEvent);
    }
  }

  private onStatus(payload: { agent?: Agent; status?: string }): void {
    if (payload.agent !== this.agent || payload.status !== "idle" || !this.active) {
      return;
    }
    const failure = this.active.failure;
    const usage = this.active.usage;
    // The turn is over: clear the active slot so a late abort() (dispose)
    // observes nothing in flight and does not rewrite the settlement.
    this.active = undefined;
    this.resolve(
      failure === undefined
        ? { status: "COMPLETED", usage }
        : { status: "ERROR", error: failure, usage },
    );
  }

  private onError(payload: { agent?: Agent; error?: unknown }): void {
    if (payload.agent !== this.agent || !this.active) {
      return;
    }
    this.recordFailure(toFailure(payload.error));
  }

  private recordFailure(failure: DshLlmFailureView | undefined): void {
    if (!this.active || failure === undefined) {
      return;
    }
    // First failure wins; subsequent events (turn/end, idle) are confirmations.
    this.active.failure ??= { code: failure.code ?? "UNKNOWN", message: failure.message ?? "" };
  }
}

/** dsh LlmFailure structural subset (turn/end{error}, agent/error). */
interface DshLlmFailureView {
  code?: string;
  message?: string;
}

function toFailure(error: unknown): DshLlmFailureView | undefined {
  if (error === null || error === undefined) {
    return undefined;
  }
  if (typeof error === "object") {
    const view = error as DshLlmFailureView;
    return { code: view.code, message: view.message };
  }
  return { code: "UNKNOWN", message: String(error) };
}

/** Mint a server-side turn identity (data-model.md §2.4: server-minted UUID). */
export function mintTurnId(): string {
  return randomUUID();
}
