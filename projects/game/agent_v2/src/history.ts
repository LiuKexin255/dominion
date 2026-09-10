/**
 * history.ts — the agent_v2 team history projections and the dsh→ChatEvent
 * member collector.
 *
 * The team model has two projections derived from the same member events
 * (specs/059-agent-v2-team-mode/data-model.md §2/§3):
 *
 * - the merged team sequence (`TeamMergeEntry`): user input and every
 *   member's native output, ordered by a monotonically assigned `seq`
 *   anchor. It is the single source shared by ListTeamMessages and the
 *   `team_message` stream frame (same entry object → same seq value,
 *   specs/059-agent-v2-team-mode/contracts/team-api.md §3.2);
 * - the per-member view (`MemberViewEntry`): the message as that member saw
 *   it, with the sender annotation (USER input, or another member's relayed
 *   broadcast rendered as `user: [sender]…`).
 *
 * {@link MemberCollector} owns one materialized member's subscriptions:
 * `session/event` (turn/step/chunk/tool events), `agent/status`, and
 * `agent/error`. It latches a member turn on the running transition (or the
 * first turn-scoped event), emits the member-labelled ChatEvent frames
 * (turn_start/block_start/delta/block_end/tool_result/turn_end), and writes
 * the history projections. Dsh chunk indexes are per-step (every model
 * request restarts at 0), so the collector remaps them onto one turn-global
 * monotonic sequence, resetting the step-local table at every step boundary
 * (specs/049-agent-v2-dsh-init/contracts/conversation-api.md §4; specs/
 * 051-agent-v2-dsh-migration/data-model.md §2.4).
 *
 * Event vocabulary anchors: dsh-session SessionEventMap
 * (node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_a4e4bb24/lib/types/
 * types.d.ts).
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
import type { TeamMessage as TeamMessageProto } from "../agent_v2_types/projects/game/v2/TeamMessage.js";
import type { DshContext } from "./dsh.js";

/**
 * Structural subset of a dsh `session/event` payload read by the collector.
 * `assistant/chunk` is `{turn, step, chunk}` with chunk a raw StreamChunk.
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
    // True on the interrupted fixation the loop appends when a stream
    // fails or is cancelled before the step settles — recorded on the
    // history message so List consumers can exclude the prefix from
    // final-answer folding (specs/054-agent-v2-bugfixes/data-model.md §1.5).
    interrupted?: boolean;
  };
}

/**
 * The `user/message` event shape the member-view history reads. The event
 * stores the complete `UserMessage` as its data (dsh-session README: "A
 * `user/message` stores the complete `UserMessage` directly … its typed
 * `source` is the only channel that tells them apart"), so content/source/id
 * live at the data top level.
 */
export interface UserMessageEvent extends DshSessionEvent {
  type: "user/message";
  data: {
    id?: string;
    content: ReadonlyArray<DshContentBlockView>;
    source: { kind: string; role?: string };
  };
}

/**
 * The `tool/call` event shape (dsh-session SessionEventMap; the loop appends
 * one per model-requested call).
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
 * The `tool/result` event shape (dsh-session SessionEventMap). The message's
 * single tool-result block carries the outcome (`toolCallId`, `isError`) and
 * the tool's rendered model-facing content.
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
  /**
   * Terminate the stream with an orchestration-level error instead of a
   * clean EOF (optional; the gRPC adapter maps it onto an INTERNAL status,
   * contracts/team-api.md §6). Recorders without it observe `end()` as the
   * fallback.
   */
  fail?(error: { code: string; message: string }): void;
}

/** How a turn ended: COMPLETED on idle, ERROR on failure, ABORTED on refresh, CANCELED on user cancel. */
export interface TurnOutcome {
  status: "COMPLETED" | "ERROR" | "ABORTED" | "CANCELED";
  error?: { code: string; message: string };
}

/** The two materialized team members; team roles are open strings upstream. */
export type MemberRole = "player" | "planner";

/**
 * The merge-sequence producer label: the reserved value `"user"` for user
 * input, or a member role (scene vocabulary). The wire form is the string
 * itself — the session face is a scene-agnostic primitive and carries no
 * role enum (2026-09-10 user ruling).
 */
export type TeamRoleLabel = "user" | MemberRole;

/**
 * The sender annotation of a relayed broadcast: the source role string
 * unchanged; a source without a role degrades to the reserved user value.
 */
export function broadcastSender(role: string | undefined): string {
  return role === undefined || role === "" ? "user" : role;
}

/** One entry of the merged team sequence (ListTeamMessages / team_message frame). */
export interface TeamMergeEntry {
  /** Producer role string: the reserved `"user"` value or a member role. */
  readonly member: string;
  readonly message: HistoryMessage;
  readonly seq: number;
}

/** One entry of a member's view history (ListMemberMessages). */
export interface MemberViewEntry {
  readonly message: HistoryMessage;
  /** Sender annotation: the reserved `"user"` value or a member role string. */
  readonly sender: string;
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

/** Map a dsh usage view onto the proto TurnUsage (int64 fields as strings). */
export function usageToProto(usage: DshTokenUsage | undefined): {
  inputTokens: string;
  outputTokens: string;
  reasoningTokens?: string;
} | undefined {
  if (usage === undefined) {
    return undefined;
  }
  const mapped: { inputTokens: string; outputTokens: string; reasoningTokens?: string } = {
    inputTokens: String(usage.inputTokens),
    outputTokens: String(usage.outputTokens),
  };
  if (usage.reasoningTokens !== undefined) {
    mapped.reasoningTokens = String(usage.reasoningTokens);
  }
  return mapped;
}

/**
 * The session's team history projections (specs/059-agent-v2-team-mode/
 * data-model.md §2): the merged team sequence plus the two member views.
 * Every appended merge entry fans out as a `team_message` frame carrying the
 * same entry object, so the stream anchor and the List projection can never
 * diverge (same source, same seq).
 *
 * The projection is session-lifetime state held by the materialized team
 * entry: a refresh builds a fresh TeamHistory, which IS the short-term memory
 * clear (data-model.md §2 lifecycle).
 */
export class TeamHistory {
  private readonly merge: TeamMergeEntry[] = [];
  private readonly views: Record<MemberRole, MemberViewEntry[]> = { player: [], planner: [] };
  private nextSeq = 1;
  private nextMessageId = 1;

  constructor(
    private readonly sessionName: string,
    private readonly sink: (event: ChatEvent) => void,
  ) {}

  /**
   * Append the user message at Send acceptance (enqueue-time fixation, also
   * for queued sends — contracts/team-api.md §3) and fan out its
   * `team_message{member=USER}` frame.
   */
  appendUser(text: string): TeamMergeEntry {
    const message = this.newMessage("ROLE_USER", [{ text: { content: text } }]);
    return this.appendMerge("user", message);
  }

  /**
   * Append one member `assistant/message` finality to the merge sequence and
   * to that member's own view (sender = the member). Empty display content
   * produces no entry (nothing model-visible in this projection). The
   * interrupted flag is carried through for List folding semantics
   * (specs/054-agent-v2-bugfixes/data-model.md §1.5).
   */
  appendMemberOutput(
    role: MemberRole,
    content: ReadonlyArray<DshContentBlockView>,
    interrupted = false,
  ): TeamMergeEntry | undefined {
    const blocks: ContentBlock[] = [];
    for (const block of content) {
      const mapped = blockToContentBlock(block);
      if (mapped !== undefined) {
        blocks.push(mapped);
      }
    }
    if (blocks.length === 0) {
      return undefined;
    }
    const message = this.newMessage("ROLE_AGENT", blocks, interrupted);
    const entry = this.appendMerge(role, message);
    this.views[role].push({ message, sender: role });
    return entry;
  }

  /**
   * Append one user message recorded in a member's own log to that member's
   * view: source `user` = the user's input; a `team-broadcast` source = a
   * relayed other-member message annotated with its sender (contracts/
   * team-api.md §5). Other source kinds have no view projection.
   */
  appendMemberViewUser(role: MemberRole, event: UserMessageEvent): void {
    const source = event.data.source;
    if (source.kind !== "user" && source.kind !== "team-broadcast") {
      return;
    }
    const blocks: ContentBlock[] = [];
    for (const block of event.data.content) {
      if (block.type === "text") {
        blocks.push({ text: { content: block.text ?? "" } });
      }
    }
    if (blocks.length === 0) {
      return;
    }
    const message = this.newMessage("ROLE_USER", blocks);
    this.views[role].push({
      message,
      sender: source.kind === "team-broadcast" ? broadcastSender(source.role) : "user",
    });
  }

  /**
   * Settle the most recent RUNNING ToolCallBlock carrying `toolId` in the
   * given member's merge entries with the tool's terminal status and
   * rendered result. The message object is shared with the member view, so
   * both projections observe the settlement. A result with no unsettled
   * matching block is ignored (returns false) — never fabricated.
   */
  settleToolResult(role: MemberRole, toolId: string, status: ToolStatus, result: string): boolean {
    for (let i = this.merge.length - 1; i >= 0; i -= 1) {
      const entry = this.merge[i];
      if (entry === undefined || entry.member !== role) {
        continue;
      }
      const blocks = entry.message.blocks ?? [];
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

  /** Snapshot of the merged sequence for ListTeamMessages (defensive copy). */
  listTeamMessages(): TeamMergeEntry[] {
    return [...this.merge];
  }

  /** Snapshot of one member's view for ListMemberMessages (defensive copy). */
  listMemberMessages(role: MemberRole): MemberViewEntry[] {
    return [...this.views[role]];
  }

  /** Build one projected message with its server-assigned id and timestamp. */
  private newMessage(
    role: "ROLE_USER" | "ROLE_AGENT",
    blocks: ContentBlock[],
    interrupted = false,
  ): HistoryMessage {
    const message: HistoryMessage = {
      messageId: `m${this.nextMessageId}`,
      role,
      createTime: nowTimestamp(),
      blocks,
    };
    this.nextMessageId += 1;
    if (interrupted) {
      message.interrupted = true;
    }
    return message;
  }

  /** Append one entry and fan out its team_message frame (single source). */
  private appendMerge(member: TeamRoleLabel, message: HistoryMessage): TeamMergeEntry {
    const entry: TeamMergeEntry = { member, message, seq: this.nextSeq };
    this.nextSeq += 1;
    this.merge.push(entry);
    const frame: TeamMessageProto = {
      member,
      message,
      seq: String(entry.seq),
    };
    this.sink({ session: this.sessionName, teamMessage: frame });
    return entry;
  }
}

interface ActiveMemberTurn {
  turnId: string;
  usage: DshTokenUsage | undefined;
  failure: { code: string; message: string } | undefined;
  /** The step number of the last seen event; a change resets the local table. */
  step: number | undefined;
  /** Step-local → turn-global block index assignments (data-model.md §2.4). */
  localIndexes: Map<number, number>;
  /** The next turn-global block index to hand out. */
  nextIndex: number;
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
 * Session-lifetime collector for one materialized member: owns that member's
 * `session/event`, `agent/status`, and `agent/error` subscriptions, writes
 * the member-labelled stream frames and the team/member history projections
 * (shared {@link TeamHistory}), and settles member turns on the idle
 * transition — the same quiescence anchor the orchestrator's switching logic
 * uses.
 */
export class MemberCollector {
  private active: ActiveMemberTurn | undefined;
  /** A cancel/refresh outcome recorded while this member's turn is in flight. */
  private pendingOutcome: TurnOutcome | undefined;
  private readonly off: Array<() => void> = [];

  constructor(
    ctx: DshContext,
    private readonly agent: Agent,
    private readonly role: MemberRole,
    private readonly sessionName: string,
    private readonly history: TeamHistory,
    private readonly sink: (event: ChatEvent) => void,
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

  /**
   * Record the terminal outcome for the member's in-flight turn (Cancel →
   * CANCELED, refresh/shutdown → ABORTED). The caller only invokes this when
   * the orchestrator reports the member active, so the outcome belongs to a
   * turn that WILL settle.
   *
   * Known race window (documented, not closed here): when the tombstone
   * lands after the orchestrator started the drive but before this collector
   * observed the running transition, the mark attaches to a turn that has
   * not latched yet; it is consumed by the first idle that finds an active
   * turn. If the cancellation converges without this collector ever seeing a
   * turn, the pending mark is simply not consumed — the CANCELED terminal
   * frame is then delivered by whichever settle path the turn takes.
   */
  markOutcome(outcome: TurnOutcome): void {
    this.pendingOutcome = outcome;
  }

  /** Unsubscribe the session-lifetime listeners (team teardown). */
  dispose(): void {
    for (const off of this.off) {
      off();
    }
    this.off.length = 0;
    this.active = undefined;
  }

  private onSessionEvent(dshSessionId: string, event: DshSessionEvent): void {
    if (dshSessionId !== this.agent.session.id) {
      return;
    }
    if (event.type === "user/message") {
      // Member-view projection only: the user input or a relayed broadcast
      // enters this member's view exactly when the orchestrator drives it
      // (survey/deepseek-harness-team-mode.md §4.4).
      this.history.appendMemberViewUser(this.role, event as UserMessageEvent);
      return;
    }
    if (event.type === "assistant/message") {
      // History collection is session-lifetime: the final blocks are
      // recorded even when no stream is attached, so refresh backfill stays
      // consistent. The loop's interrupted fixation flag rides along
      // (specs/054-agent-v2-bugfixes/data-model.md §1.5).
      const assistantEvent = event as AssistantMessageEvent;
      const message = assistantEvent.data.message;
      const active = this.ensureActive();
      active.usage = assistantEvent.data.usage ?? active.usage;
      active.pending = [];
      this.history.appendMemberOutput(
        this.role,
        message.content,
        assistantEvent.data.interrupted === true,
      );
      return;
    }
    if (event.type === "tool/call") {
      // Display frames already streamed as tool-call chunks; the result
      // settles the matching block (wire invariant: the loop pairs every
      // call with a result).
      return;
    }
    if (event.type === "tool/result") {
      this.onToolResult(event as ToolResultMessageEvent);
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
   * Map one `tool/result` session event to the `tool_result` member frame and
   * settle the matching history block. The frame is keyed by tool_id — a web
   * client finalizes its matching block on arrival — while the history
   * settlement is session-lifetime. A result with no unsettled matching
   * block is ignored on the history side, never fabricated.
   */
  private onToolResult(event: ToolResultMessageEvent): void {
    const block = event.data.message.content[0];
    const toolId = block?.toolCallId ?? "";
    const status: ToolStatus = block?.isError ? "TOOL_STATUS_FAILED" : "TOOL_STATUS_SUCCEEDED";
    const result = (block?.content ?? [])
      .map((candidate) => (candidate.type === "text" ? candidate.text ?? "" : ""))
      .join("");
    this.history.settleToolResult(this.role, toolId, status, result);
    const active = this.ensureActive();
    const toolResult: ToolResultEvent = { toolId, status, result };
    this.sink({
      session: this.sessionName,
      turnId: active.turnId,
      member: this.role,
      toolResult,
    });
  }

  private onChunk(
    data: { turn?: number; step?: number; chunk?: DshStreamChunk } | undefined,
  ): void {
    const chunk = data?.chunk;
    if (!chunk) {
      return;
    }
    const active = this.ensureActive();
    if (chunk.type === "usage") {
      // Folded into turn_end.usage; never a standalone frame (§4).
      active.usage = chunk.usage;
      return;
    }
    // Step-boundary reset (data-model.md §2.4): dsh chunk indexes restart at
    // 0 on every model request, so a new step number drops the step-local
    // table and the pending interrupted-prefix blocks; the turn-global
    // counter keeps monotonic across steps.
    const step = data?.step;
    if (step !== undefined && step !== active.step) {
      active.step = step;
      active.localIndexes = new Map();
      active.pending = [];
    }
    const index = chunk.index ?? 0;
    if (chunk.type === "reasoning-delta" || chunk.type === "text-delta") {
      const view = this.findPending(index) ?? this.createPending(index, chunk.type === "reasoning-delta" ? "reasoning" : "text");
      view.text += chunk.text ?? "";
    } else if (chunk.type === "block-end" && chunk.block !== undefined) {
      const existing = this.findPending(index);
      if (existing !== undefined) {
        active.pending = active.pending.filter((entry) => entry.index !== index);
      }
      active.pending.push({ index, view: chunk.block });
    }
    const chatEvent = chunkToChatEvent(
      chunk,
      this.sessionName,
      active.turnId,
      (localIndex) => this.remapIndex(active, localIndex),
      // Step numbers reach clients on every block frame for the display
      // segmentation (specs/054-agent-v2-bugfixes/data-model.md §1.1); a dsh
      // event without one degrades to step 0.
      step ?? 0,
    );
    if (chatEvent !== undefined) {
      chatEvent.member = this.role;
      this.sink(chatEvent);
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
  private remapIndex(active: ActiveMemberTurn, localIndex: number): number {
    const assigned = active.localIndexes.get(localIndex);
    if (assigned !== undefined) {
      return assigned;
    }
    const global = active.nextIndex;
    active.nextIndex += 1;
    active.localIndexes.set(localIndex, global);
    return global;
  }

  /**
   * Latch the member turn: the running transition (or the first turn-scoped
   * event) opens a fresh turn id and emits its `turn_start` member frame
   * before any block frame.
   */
  private ensureActive(): ActiveMemberTurn {
    if (this.active !== undefined) {
      return this.active;
    }
    const active: ActiveMemberTurn = {
      turnId: mintTurnId(),
      usage: undefined,
      failure: undefined,
      step: undefined,
      localIndexes: new Map(),
      nextIndex: 0,
      pending: [],
    };
    this.active = active;
    this.sink({
      session: this.sessionName,
      turnId: active.turnId,
      member: this.role,
      turnStart: {},
    });
    return active;
  }

  private onStatus(payload: { agent?: Agent; status?: string }): void {
    if (payload.agent !== this.agent) {
      return;
    }
    if (payload.status === "running") {
      this.ensureActive();
      return;
    }
    if (payload.status !== "idle" || this.active === undefined) {
      return;
    }
    const active = this.active;
    const outcome =
      this.pendingOutcome ??
      (active.failure === undefined
        ? { status: "COMPLETED" as const }
        : { status: "ERROR" as const, error: active.failure });
    const pendingBlocks = active.pending.map((entry) => entry.view);
    // The turn is over: clear the active slot so a late markOutcome (cancel
    // racing the idle) observes nothing in flight.
    this.active = undefined;
    this.pendingOutcome = undefined;
    if (active.failure !== undefined && outcome.status === "ERROR" && pendingBlocks.length > 0) {
      // Provider failure with a streamed prefix: the loop layer dropped it
      // (it only solidifies interrupted prefixes on cancellation), so the
      // collector appends the interrupted history entry here — the specs/
      // 054 backfill semantics the large tests assert.
      this.history.appendMemberOutput(this.role, pendingBlocks, true);
    }
    const usage =
      outcome.status === "COMPLETED" ? usageToProto(active.usage) : undefined;
    this.sink({
      session: this.sessionName,
      turnId: active.turnId,
      member: this.role,
      turnEnd: {
        status: `TURN_STATUS_${outcome.status}`,
        ...(outcome.error === undefined ? {} : { error: outcome.error }),
        ...(usage === undefined ? {} : { usage }),
      },
    });
  }

  private onError(payload: { agent?: Agent; error?: unknown }): void {
    if (payload.agent !== this.agent) {
      return;
    }
    this.recordFailure(toFailure(payload.error));
  }

  private recordFailure(failure: DshLlmFailureView | undefined): void {
    if (this.active === undefined || failure === undefined) {
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

/** Mint a server-side member-turn identity (data-model.md §2.4: server-minted UUID). */
export function mintTurnId(): string {
  return randomUUID();
}
