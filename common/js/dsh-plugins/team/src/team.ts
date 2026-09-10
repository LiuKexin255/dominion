/**
 * TeamService — the `ctx.team` group-chat primitive: member registration with
 * the team section, `session/event` output collection, reference relay (anchor
 * delivery, no content copies), and drain (read-back + render + consumption).
 * The scenario-agnostic contract is
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §1; the derived-rebuild
 * model is survey/deepseek-harness-team-mode.md §4.4a (decisions ⑦⑧⑮).
 *
 * The service is host-row state over member AgentHandles: it never drives a
 * member (no `followup`/`steer`/`inject`/`cancel`) and holds no game concept.
 * Buffer entries are anchors only — a unit is one anchor plus its sender role
 * and log order key; the content is read from the sender's live session log at
 * drain, so there is no copy to keep consistent. The per-receiver pending list
 * is a derived cache: `reconcile` recomputes it as
 * `sender log productions − receiver consumption anchors`, which makes the
 * list self-healing after a missed relay and exactly-once by construction.
 */

import { Service } from "@deepseek-ai/cordis";
import type { Context } from "@deepseek-ai/cordis";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import type { SessionEvent, SessionId } from "@deepseek-ai/dsh-session";

import {
  buildBroadcastMessage,
  compareUnits,
  consumedAnchors,
  deriveUnits,
  messageBody,
} from "./broadcast.js";
import type { BroadcastUnit } from "./broadcast.js";
import { renderTeamSection } from "./section.js";

export type { TeamBroadcastSource } from "./broadcast.js";

/**
 * The team section order: 1–49 (after the persona slot at 0, before tool
 * guidance at 100–199) — contracts/dsh-plugins.md §1 item 1; band rationale
 * survey/deepseek-harness-team-mode.md §4.5.
 */
export const TEAM_SECTION_ORDER = 10;

/** Section name; unique per agent scope (re-registration disposes the old one). */
export const TEAM_SECTION_NAME = "team:roster";

/** One registered member: the owned member agent, its role label, its one-line roster summary. */
export interface TeamMemberRegistration {
  readonly agent: AgentHandle;
  /** Open role string (saolei supplies "player" / "planner"). */
  readonly role: string;
  /** Third-person one-line duty summary rendered into the roster (R2 boundary: an index, not details). */
  readonly summary: string;
}

/** The `register` parameters: team facts shared by every member. */
export interface TeamRegistration {
  /** Team goal text. */
  readonly goal: string;
  readonly members: readonly TeamMemberRegistration[];
  /** Generalized correlation key (saolei passes the game id; team never interprets it). */
  readonly context?: string;
}

/** The registration's teardown capability (idempotent). */
export interface TeamHandle {
  dispose(): void;
}

/** One pending entry: an anchor plus the sender identity needed to read it back. */
interface PendingUnit extends BroadcastUnit {
  readonly senderSessionId: SessionId;
  readonly senderRole: string;
}

interface MemberState {
  registration: TeamMemberRegistration;
  /** The exact section text currently registered on this member's scope. */
  sectionText: string;
  sectionOff: () => void;
  eventOff: () => void;
  pending: PendingUnit[];
  /** Anchors popped by drain but not yet observed in this member's log. */
  drained: Set<string>;
  /** Tool calls relayed-in-wait: callId → producing event order key; removed the moment the result lands. */
  pendingCalls: Map<string, { time: number; seq: number }>;
}

interface TeamState {
  goal: string;
  context?: string;
  members: Map<string, MemberState>;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    team: TeamService;
  }
}

/**
 * The host-row team service. Constructed by the class-form plugin row; a
 * member's registrations go through that member's `agent.ctx`, so the team
 * section and the `session/event` subscription are agent-scope effects that
 * unwind with the member even when the team handle is never disposed.
 */
export class TeamService extends Service {
  /** Every registered member's team, keyed by member session id. */
  private readonly byMember = new Map<string, TeamState>();

  constructor(ctx: Context) {
    super(ctx, "team");
    // Member teardown drops the member's entries and buffers; the host-level
    // listener survives the member scope unwind that precedes
    // `agent/disposed` (https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md).
    ctx.on("agent/disposed", (payload) => {
      this.dropMember(String(payload.agent.id));
    });
  }

  /**
   * Register (or refresh) a team. Idempotent per member: a repeated
   * registration of the same agent refreshes its role/summary and section in
   * place without duplicating effects, and drops members absent from the new
   * roster. Member roles are open strings; the return handle disposes exactly
   * this team.
   */
  register(registration: TeamRegistration): TeamHandle {
    if (registration.members.length === 0) {
      throw new Error("team register: at least one member is required");
    }
    const ids = new Set<string>();
    for (const member of registration.members) {
      const id = String(member.agent.agent.id);
      if (ids.has(id)) {
        throw new Error(`team register: member "${id}" is listed twice`);
      }
      ids.add(id);
    }

    const team = this.resolveTeam(ids);
    team.goal = registration.goal;
    team.context = registration.context;
    const sectionText = renderTeamSection({
      goal: registration.goal,
      members: registration.members.map((member) => ({
        role: member.role,
        summary: member.summary,
      })),
    });

    for (const member of registration.members) {
      const id = String(member.agent.agent.id);
      const existing = team.members.get(id);
      if (existing === undefined) {
        team.members.set(id, this.createMember(member, sectionText));
      } else {
        this.refreshMember(existing, member, sectionText);
      }
      this.byMember.set(id, team);
    }
    for (const id of [...team.members.keys()]) {
      if (!ids.has(id)) {
        this.dropMember(id);
      }
    }
    for (const member of team.members.values()) {
      this.reconcile(member);
    }
    return { dispose: () => this.disposeTeam(team) };
  }

  /**
   * Take this member's unconsumed broadcasts as injection-ready user messages
   * and mark them consumed. The pending list is reconciled against the sender
   * logs first (the derivation is the read authority — a relay missed between
   * events is restored, 自愈; everything already drained or present in the
   * member's own log stays excluded, exactly-once). Anchors and read paths
   * stay internal; callers receive messages only.
   *
   * Consumption is marked per unit and only after that unit's message was
   * built: a failing build leaves its anchor unmarked, so the next drain's
   * rebuild retries it instead of excluding it forever.
   */
  drain(member: AgentHandle | Agent): UserMessage[] {
    const agent = "dispose" in member ? member.agent : member;
    const id = String(agent.id);
    const team = this.byMember.get(id);
    const state = team?.members.get(id);
    if (team === undefined || state === undefined) {
      throw new Error(`team drain: agent "${id}" is not a registered member`);
    }
    this.reconcile(state);
    const units = state.pending;
    state.pending = [];
    const messages: UserMessage[] = [];
    for (const unit of units) {
      const message = this.buildMessage(unit, team);
      state.drained.add(unit.anchor);
      messages.push(message);
    }
    return messages;
  }

  /**
   * Build one injection-ready message for a pending unit: look the sender up
   * and read its live log back by the anchor, then render. A missing source
   * event is corruption, so the read fails loud rather than fabricating
   * content. Protected so tests can substitute a transient failing double and
   * assert the drain loop's consume-mark ordering.
   */
  protected buildMessage(unit: PendingUnit, team: TeamState): UserMessage {
    const sender = team.members.get(String(unit.senderSessionId));
    return buildBroadcastMessage(
      unit.senderRole,
      unit,
      unit.senderSessionId,
      sender?.registration.agent.agent.session.events ?? [],
      team.context,
    );
  }

  /** The team already holding one of these members, or a fresh team state. */
  private resolveTeam(ids: ReadonlySet<string>): TeamState {
    let found: TeamState | undefined;
    for (const id of ids) {
      const team = this.byMember.get(id);
      if (team === undefined) {
        continue;
      }
      if (found !== undefined && found !== team) {
        throw new Error("team register: members of different teams cannot be mixed");
      }
      found = team;
    }
    return found ?? { goal: "", members: new Map() };
  }

  /** Create a member's effects and buffer; the section is the shared team text. */
  private createMember(
    registration: TeamMemberRegistration,
    sectionText: string,
  ): MemberState {
    const { agent } = registration.agent;
    const state: MemberState = {
      registration,
      sectionText,
      sectionOff: () => {},
      eventOff: () => {},
      pending: [],
      drained: new Set(),
      pendingCalls: new Map(),
    };
    state.sectionOff = this.registerSection(registration, sectionText);
    state.eventOff = agent.ctx.on("session/event", (session, event) => {
      if (session.id === agent.id) {
        this.onMemberEvent(state, agent, event);
      }
    });
    return state;
  }

  /** Refresh identity fields in place; re-register the section only when its text changed. */
  private refreshMember(
    state: MemberState,
    registration: TeamMemberRegistration,
    sectionText: string,
  ): void {
    state.registration = registration;
    if (state.sectionText !== sectionText) {
      state.sectionOff();
      state.sectionOff = this.registerSection(registration, sectionText);
      state.sectionText = sectionText;
    }
  }

  /** Register the section on the member's agent scope (per-member ownership). */
  private registerSection(
    registration: TeamMemberRegistration,
    text: string,
  ): () => void {
    return registration.agent.agent.ctx.systemPrompt.section({
      name: TEAM_SECTION_NAME,
      order: TEAM_SECTION_ORDER,
      text,
    });
  }

  /** Drop one member from its team (agent disposal or roster refresh). */
  private dropMember(id: string): void {
    const team = this.byMember.get(id);
    if (team === undefined) {
      return;
    }
    this.byMember.delete(id);
    const state = team.members.get(id);
    if (state === undefined) {
      return;
    }
    team.members.delete(id);
    state.sectionOff();
    state.eventOff();
    state.pending = [];
    state.drained.clear();
    state.pendingCalls.clear();
  }

  private disposeTeam(team: TeamState): void {
    for (const id of [...team.members.keys()]) {
      this.dropMember(id);
    }
  }

  /** Collect one member output event into the relay (anchors only, no copies). */
  private onMemberEvent(state: MemberState, agent: Agent, event: SessionEvent): void {
    const id = String(agent.id);
    const team = this.byMember.get(id);
    if (team === undefined || team.members.get(id) !== state) {
      return;
    }
    if (event.type === "assistant/message") {
      if (messageBody(event.data.message) === "") {
        return;
      }
      this.relay(team, state, {
        kind: "message",
        anchor: String(event.data.message.id),
        time: event.time,
        seq: event.seq,
      });
      return;
    }
    if (event.type === "tool/call") {
      state.pendingCalls.set(String(event.data.callId), {
        time: event.time,
        seq: event.seq,
      });
      return;
    }
    if (event.type === "tool/result") {
      const callId = event.data.message.content[0]?.toolCallId;
      if (callId === undefined) {
        return;
      }
      const call = state.pendingCalls.get(String(callId));
      if (call === undefined) {
        return;
      }
      this.relay(team, state, {
        kind: "tool",
        anchor: String(callId),
        time: call.time,
        seq: call.seq,
      });
      // The transient waiting entry leaves the relay once the unit is complete
      // and its anchor has entered every receiver list (contract §1 item 3:
      // no relayed item lingers).
      state.pendingCalls.delete(String(callId));
    }
  }

  /**
   * Append one unit's anchor to every member's pending list except the
   * sender's — the live event path that keeps each member's arriving order
   * (contracts/dsh-plugins.md §1 item 3: every receiver's list gets the anchor
   * before the transient `pendingCalls` entry is dropped).
   *
   * Read authority is the derivation, not this append: `drain` reconciles
   * against the sender logs before consuming, and the rebuilt list (derived
   * order) is what callers see. The two paths converge on the same set in
   * normal operation — every relayed anchor is a sender-log production — and
   * the rebuild additionally supplies anything this path missed, which is why
   * the read face prefers it over the live list.
   */
  private relay(team: TeamState, sender: MemberState, unit: BroadcastUnit): void {
    const senderId = sender.registration.agent.agent.id;
    for (const [id, receiver] of team.members) {
      if (id === String(senderId) || receiver.drained.has(unit.anchor)) {
        continue;
      }
      receiver.pending.push({
        ...unit,
        senderSessionId: senderId,
        senderRole: sender.registration.role,
      });
    }
  }

  /**
   * Rebuild one member's pending list from the fact source — the read
   * authority over the live relay list:
   * `sender log productions − receiver consumption anchors − locally drained`,
   * ordered by the producing events. Consumed anchors close the drain mark, so
   * an already-logged broadcast never returns; anchors never logged return
   * only when no drain has claimed them (lost-relay self-heal).
   */
  private reconcile(member: MemberState): void {
    const id = String(member.registration.agent.agent.id);
    const team = this.byMember.get(id);
    if (team === undefined) {
      return;
    }
    const consumed = consumedAnchors(member.registration.agent.agent.session.events);
    for (const anchor of consumed) {
      member.drained.delete(anchor);
    }
    const pending: PendingUnit[] = [];
    for (const sender of team.members.values()) {
      if (sender === member) {
        continue;
      }
      for (const unit of deriveUnits(sender.registration.agent.agent.session.events)) {
        if (consumed.has(unit.anchor) || member.drained.has(unit.anchor)) {
          continue;
        }
        pending.push({
          ...unit,
          senderSessionId: sender.registration.agent.agent.id,
          senderRole: sender.registration.role,
        });
      }
    }
    pending.sort(compareUnits);
    member.pending = pending;
  }
}

export default TeamService;
