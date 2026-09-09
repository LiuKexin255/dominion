/**
 * history.ts — per-session conversation history and the dsh→ChatEvent turn
 * collector (specs/049-agent-v2-dsh-init/data-model.md §2.4/§2.5;
 * tool-result extension: specs/051-agent-v2-dsh-migration/data-model.md
 * §2.3/§2.4, research.md D10).
 *
 * The collector is armed once per session entry (session lifetime, not stream
 * lifetime — specs/049-agent-v2-dsh-init/research.md D10-2) and maps dsh
 * turn events to proto ChatEvents for the active turn's stream; the mapping
 * table is specs/049-agent-v2-dsh-init/contracts/conversation-api.md §4 plus
 * the `tool_result` frame of specs/051-agent-v2-dsh-migration/data-model.md
 * §2.4. dsh chunk indexes are per-step (every model request restarts at 0),
 * so the collector remaps them onto one turn-global monotonic sequence,
 * resetting the step-local table at every step boundary. `assistant/message`
 * appends the final content blocks to the in-memory history
 * (specs/049-agent-v2-dsh-init/spec.md FR-014), which stays collected even
 * when no stream is attached; `tool/result` settles the matching
 * ToolCallBlock by tool_id in that same history.
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
import type { ToolResultEvent } from "../agent_v2_types/projects/game/v2/ToolResultEvent.js";
import type { ToolStatus } from "../agent_v2_types/projects/game/v2/ToolStatus.js";
import type { DshContext } from "./dsh.js";

/**
 * Structural subset of a dsh `session/event` payload read by the collector.
 * The event-type vocabulary and payload shapes anchor at the dsh-session
 * SessionEventMap (node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_
 * a4e4bb24a1f3580ac25e11cfa3c6b8cc/node_modules/@deepseek-ai/dsh-session/
 * lib/types/types.d.ts) — `assistant/chunk` = `{turn, step, chunk}` with
 * chunk a raw StreamChunk.
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
    // True on the interrupted fixation the driver appends when a stream
    // fails or is cancelled before the step settles (saolei-loop driver
    // appendInterrupted) — recorded on the history message so List
    // consumers can exclude the prefix from final-answer folding
    // (specs/054-agent-v2-bugfixes/data-model.md §1.5).
    interrupted?: boolean;
  };
}

/**
 * The `tool/call` event shape (dsh-session SessionEventMap; the loop appends
 * one per model-requested call — common/js/dsh-plugins/saolei-loop/src/
 * driver.ts appendToolCall).
 */
export interface ToolCallEvent extends DshSessionEvent {
  type: "tool/call";
  data: {
    turn: number;
    step: number;
    callId: string;
    name: string;
    arguments: string;
  };
}

/**
 * The `tool/result` event shape (dsh-session SessionEventMap; the loop
 * appends one per settled call — driver.ts appendToolResult). The message's
 * single tool-result block carries the outcome (`toolCallId`, `isError`) and
 * the tool's rendered model-facing content (saolei tools render one text
 * block with the board text).
 */
export interface ToolResultMessageEvent extends DshSessionEvent {
  type: "tool/result";
  data: {
    turn: number;
    step: number;
    message: {
      content: ReadonlyArray<{
        type: "tool-result";
        toolCallId?: string;
        isError?: boolean;
        content?: ReadonlyArray<DshContentBlockView>;
      }>;
    };
    error?: { name: string; code: string };
    meta?: unknown;
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

/** How a turn ended: COMPLETED on idle, ERROR on failure, ABORTED on dispose, CANCELED on user cancel. */
export interface TurnOutcome {
  status: "COMPLETED" | "ERROR" | "ABORTED" | "CANCELED";
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
 * Map one dsh content block to a proto ContentBlock. The display projection
 * covers exactly text/reasoning/tool-call; other block types (image,
 * tool-result — the latter reaches clients through the `tool_result` event
 * instead) have no block projection and yield undefined, the same
 * forward-compat drop as unknown oneof branches on the consumer side
 * (specs/049-agent-v2-dsh-init/contracts/conversation-api.md §2).
 */
export function blockToContentBlock(block: DshContentBlockView): ContentBlock | undefined {
  if (block.type === "text") {
    return { text: { content: block.text ?? "" } };
  }
  if (block.type === "reasoning") {
    return { think: { content: block.text ?? "" } };
  }
  if (block.type === "tool-call") {
    // The block is recorded RUNNING; the loop's `tool/result` session event
    // settles it by tool_id (specs/051-agent-v2-dsh-migration/data-model.md
    // §2.3 — history and live stream share that terminal source).
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
 *
 * `remapIndex` projects the chunk's per-step provider index onto the
 * turn-global block sequence (specs/051-agent-v2-dsh-migration/data-model.md
 * §2.4); omitted, the index passes through unchanged.
 *
 * `step` is the turn's model-output step the block belongs to (specs/
 * 054-agent-v2-bugfixes/data-model.md §1.1: the display segmentation
 * dimension, monotonic within the turn); the collector passes the
 * ActiveTurn-tracked number and defaults to 0 when the dsh event carries no
 * step.
 */
export function chunkToChatEvent(
  chunk: DshStreamChunk,
  sessionName: string,
  turnId: string,
  remapIndex: (index: number) => number = (index) => index,
  step = 0,
): ChatEvent | undefined {
  if (chunk.type === "block-start") {
    // Unknown block types have no display projection in this phase and are
    // dropped — the same forward-compat policy as blockToContentBlock.
    const blockType = BLOCK_TYPES[chunk.blockType ?? ""];
    if (blockType === undefined) {
      return undefined;
    }
    const start: BlockStartEvent = {
      index: remapIndex(chunk.index ?? 0),
      type: blockType,
      step,
    };
    if (chunk.blockType === "tool-call") {
      start.toolId = chunk.id ?? "";
      start.name = chunk.name ?? "";
    }
    return { session: sessionName, turnId, blockStart: start };
  }
  if (chunk.type === "text-delta" || chunk.type === "reasoning-delta") {
    const delta: BlockDeltaEvent = { index: remapIndex(chunk.index ?? 0), text: chunk.text ?? "", step };
    return { session: sessionName, turnId, delta };
  }
  if (chunk.type === "tool-call-delta") {
    const delta: BlockDeltaEvent = { index: remapIndex(chunk.index ?? 0), text: chunk.argumentsDelta ?? "", step };
    return { session: sessionName, turnId, delta };
  }
  if (chunk.type === "block-end") {
    const block = chunk.block ? blockToContentBlock(chunk.block) : undefined;
    if (block === undefined) {
      return undefined;
    }
    const end: BlockEndEvent = { index: remapIndex(chunk.index ?? 0), block, step };
    return { session: sessionName, turnId, blockEnd: end };
  }
  return undefined;
}

/**
 * Per-session in-memory conversation record (specs/049-agent-v2-dsh-init/
 * spec.md FR-014): user messages are appended at enqueue time, agent replies
 * at `assistant/message` finality; ordering and text/think/tool-call
 * classification are preserved
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

  /**
   * Append the agent reply from the round's final assistant message. An
   * interrupted append records the sparse flag on the history message so
   * List consumers can exclude the prefix from final-answer folding
   * (specs/054-agent-v2-bugfixes/data-model.md §1.5).
   */
  appendAssistant(
    content: ReadonlyArray<DshContentBlockView>,
    interrupted = false,
  ): void {
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
      ...(interrupted ? { interrupted: true } : {}),
    });
    this.nextSeq += 1;
  }

  /** Snapshot of the history for ListAgentMessages (defensive copy). */
  list(): HistoryMessage[] {
    return [...this.messages];
  }

  /**
   * Settle the most recent RUNNING ToolCallBlock carrying `toolId` with the
   * tool's terminal status and rendered result (specs/051-agent-v2-dsh-
   * migration/data-model.md §2.3: the turn's tool ids are unique, so the
   * newest match is the block). A result with no unsettled matching block is
   * ignored (returns false) — never fabricated into history.
   */
  settleToolResult(toolId: string, status: ToolStatus, result: string): boolean {
    for (let i = this.messages.length - 1; i >= 0; i -= 1) {
      const blocks = this.messages[i]?.blocks ?? [];
      for (let j = blocks.length - 1; j >= 0; j -= 1) {
        // The oneof arm is nullable in the generated projection.
        const block = blocks[j]?.toolCall ?? null;
        if (block !== null && block.toolId === toolId && block.status === "TOOL_STATUS_RUNNING") {
          block.status = status;
          block.result = result;
          return true;
        }
      }
    }
    return false;
  }
}

interface ActiveTurn {
  turnId: string;
  stream: TurnStream;
  usage: DshTokenUsage | undefined;
  failure: { code: string; message: string } | undefined;
  /** The step number of the last seen event; a change resets the local table. */
  step: number | undefined;
  /** Step-local → turn-global block index assignments (data-model.md §2.4). */
  localIndexes: Map<number, number>;
  /** The next turn-global block index to hand out. */
  nextIndex: number;
  /** Tool ids with a `tool/call` and no `tool/result` yet this turn. */
  pendingTools: Set<string>;
  /**
   * Streamed-but-unfinalized display blocks of the CURRENT step, in arrival
   * order, keyed per provider block index. Reasoning chunks stream as bare
   * deltas (their block view only materializes at `block-end`), so both
   * deltas and block-end views accumulate here. The official loop
   * solidifies an interrupted prefix into an `assistant/message` only when
   * its abort signal fired (cancellation); a provider failure drops the
   * prefix at the loop layer, so the collector carries it here and appends
   * the interrupted history entry itself when the turn settles ERROR
   * (specs/054-agent-v2-bugfixes semantics preserved through the loop
   * pivot).
   */
  pending: Array<{ index: number; view: DshContentBlockView }>;
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
  /** Abort outcome observed before awaitSettled registered (abort can race the await). */
  private pendingAbort: TurnOutcome | undefined;
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
    this.active = {
      turnId,
      stream,
      usage: undefined,
      failure: undefined,
      step: undefined,
      localIndexes: new Map(),
      nextIndex: 0,
      pendingTools: new Set(),
      pending: [],
    };
    this.settlement = undefined;
    this.pendingAbort = undefined;
  }

  /**
   * Resolve when the turn settles (idle→COMPLETED/ERROR, abort→its outcome).
   * A settlement (or abort) that raced ahead of the call resolves
   * immediately — the dsh lifecycle can settle the turn before the runner's
   * await registers.
   */
  awaitSettled(): Promise<TurnSettlement> {
    if (this.pendingAbort !== undefined) {
      return Promise.resolve({ ...this.pendingAbort, usage: undefined });
    }
    if (this.settlement !== undefined) {
      return Promise.resolve(this.settlement);
    }
    return new Promise((resolve) => {
      this.settle = resolve;
    });
  }

  /**
   * Settle the in-flight turn with `outcome` (default ABORTED on the dispose
   * path; the user cancel passes CANCELED) and stop forwarding. The caller
   * owns the in-flight turn's terminal frame — it receives the stream and
   * turn id so it can deliver turn_end — or undefined when no turn is
   * running (an already-settled turn must not receive a spurious frame).
   * Whatever settles here wins over the agent/status→idle transition, so
   * callers must abort BEFORE stopping the turn at its source (teardown's
   * dispose, cancel's Agent.cancel): the driver's cancellation converges to
   * an idle status that would otherwise settle the slot COMPLETED.
   */
  abort(outcome: TurnOutcome = { status: "ABORTED" }): { stream: TurnStream; turnId: string } | undefined {
    const inFlight = this.active;
    if (inFlight === undefined) {
      return undefined;
    }
    this.active = undefined;
    this.pendingAbort = outcome;
    this.resolve({ ...outcome, usage: undefined });
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
      // refresh backfill stays consistent (research.md D10-2). The driver's
      // interrupted fixation flag rides along onto the history message
      // (specs/054-agent-v2-bugfixes/data-model.md §1.5).
      const assistantEvent = event as AssistantMessageEvent;
      const message = assistantEvent.data.message;
      if (this.active !== undefined) {
        this.active.usage = assistantEvent.data.usage ?? this.active.usage;
        this.active.pending = [];
      }
      this.history.appendAssistant(
        message.content,
        assistantEvent.data.interrupted === true,
      );
      return;
    }
    if (event.type === "tool/call") {
      // Wire invariant (specs/051-agent-v2-dsh-migration/data-model.md §4-2):
      // the loop pairs every call with a result — the pending set is the
      // collector's in-turn bookkeeping of that pairing; the call's own
      // display frames already streamed as tool-call chunks.
      const call = event as ToolCallEvent;
      if (this.active !== undefined) {
        this.active.pendingTools.add(call.data.callId);
      }
      return;
    }
    if (event.type === "tool/result") {
      this.onToolResult(event as ToolResultMessageEvent);
      return;
    }
    if (!this.active) {
      return;
    }
    if (event.type === "assistant/chunk") {
      this.onChunk(event.data as { turn?: number; step?: number; chunk?: DshStreamChunk } | undefined);
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

  /**
   * Map one `tool/result` session event to the `tool_result` ChatEvent frame
   * (specs/051-agent-v2-dsh-migration/data-model.md §2.4) and settle the
   * matching history block. The frame is keyed by tool_id — a web client
   * finalizes its matching block on arrival — so it emits whenever the turn
   * stream is live, while the history settlement is session-lifetime like
   * every history projection. A result with no unsettled matching block is
   * ignored on the history side (data-model.md §2.3), never fabricated.
   */
  private onToolResult(event: ToolResultMessageEvent): void {
    const block = event.data.message.content[0];
    const toolId = block?.toolCallId ?? "";
    const status: ToolStatus = block?.isError ? "TOOL_STATUS_FAILED" : "TOOL_STATUS_SUCCEEDED";
    const result = (block?.content ?? [])
      .map((candidate) => (candidate.type === "text" ? candidate.text ?? "" : ""))
      .join("");
    this.history.settleToolResult(toolId, status, result);
    if (this.active === undefined) {
      return;
    }
    this.active.pendingTools.delete(toolId);
    const toolResult: ToolResultEvent = { toolId, status, result };
    this.active.stream.write({ session: this.sessionName, turnId: this.active.turnId, toolResult });
  }

  private onChunk(
    data: { turn?: number; step?: number; chunk?: DshStreamChunk } | undefined,
  ): void {
    const chunk = data?.chunk;
    if (!chunk || !this.active) {
      return;
    }
    if (chunk.type === "usage") {
      // Folded into turn_end.usage; never a standalone frame (§4).
      this.active.usage = chunk.usage;
      return;
    }
    // Step-boundary reset (data-model.md §2.4): dsh chunk indexes restart at
    // 0 on every model request, so a new step number drops the step-local
    // table and the pending interrupted-prefix blocks; the turn-global
    // counter keeps monotonic across steps.
    const step = data?.step;
    if (step !== undefined && step !== this.active.step) {
      this.active.step = step;
      this.active.localIndexes = new Map();
      this.active.pending = [];
    }
    const index = chunk.index ?? 0;
    if (chunk.type === "reasoning-delta" || chunk.type === "text-delta") {
      const view = this.findPending(index) ?? this.createPending(index, chunk.type === "reasoning-delta" ? "reasoning" : "text");
      view.text += chunk.text ?? "";
    } else if (chunk.type === "block-end" && chunk.block !== undefined) {
      const existing = this.findPending(index);
      if (existing !== undefined) {
        this.active.pending = this.active.pending.filter((entry) => entry.index !== index);
      }
      this.active.pending.push({ index, view: chunk.block });
    }
    const chatEvent = chunkToChatEvent(
      chunk,
      this.sessionName,
      this.active.turnId,
      (index) => this.remapIndex(index),
      // Step numbers reach clients on every block frame for the display
      // segmentation (specs/054-agent-v2-bugfixes/data-model.md §1.1); a dsh
      // event without one degrades to step 0.
      step ?? 0,
    );
    if (chatEvent !== undefined) {
      this.active.stream.write(chatEvent);
    }
  }

  /** The pending prefix entry for a provider block index, if any. */
  private findPending(index: number): DshContentBlockView | undefined {
    return this.active?.pending.find((entry) => entry.index === index)?.view;
  }

  private createPending(index: number, type: "text" | "reasoning"): DshContentBlockView {
    const view: DshContentBlockView = { type, text: "" };
    this.active?.pending.push({ index, view });
    return view;
  }

  /** Assign the turn-global index for a step-local one, first sight wins. */
  private remapIndex(localIndex: number): number {
    const active = this.active;
    if (active === undefined) {
      return localIndex;
    }
    const assigned = active.localIndexes.get(localIndex);
    if (assigned !== undefined) {
      return assigned;
    }
    const global = active.nextIndex;
    active.nextIndex += 1;
    active.localIndexes.set(localIndex, global);
    return global;
  }

  private onStatus(payload: { agent?: Agent; status?: string }): void {
    if (payload.agent !== this.agent || payload.status !== "idle" || !this.active) {
      return;
    }
    const failure = this.active.failure;
    const usage = this.active.usage;
    const pendingBlocks = this.active.pending.map((entry) => entry.view);
    // The turn is over: clear the active slot so a late abort() (dispose)
    // observes nothing in flight and does not rewrite the settlement.
    this.active = undefined;
    if (failure !== undefined && pendingBlocks.length > 0) {
      // Provider failure with a streamed prefix: the loop layer dropped it
      // (it only solidifies interrupted prefixes on cancellation), so the
      // collector appends the interrupted history entry here — the specs/
      // 054 backfill semantics the large tests assert.
      this.history.appendAssistant(pendingBlocks, true);
    }
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
