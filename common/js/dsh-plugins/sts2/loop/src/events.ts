/**
 * MainAgentEventStream — the sts2 minimal orchestration's main-member event
 * envelope (specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md §5).
 *
 * It subscribes the main agent's own production faces and forwards each
 * production under the team's unified envelope vocabulary
 * (`TeamMemberEvent`, common/js/dsh-plugins/team/src/team.ts:154):
 *
 * - `session/event` for this agent's session → `channel: "log"`;
 * - `agent/assistant-stream` / `agent/status` / `agent/error` for this agent →
 *   `channel: "live"`.
 *
 * The envelope is always `{role: "main", memberId: <agent session id>, channel,
 * event}`. `@dominion/dsh-team` is a type-only import: the host's
 * `MemberCollector` consumes the same envelope shape it consumes for saolei
 * members, so no team registration, relay or pending list is involved
 * (contract §3 "不做").
 *
 * The stream is live-only (transient productions), like the team's own
 * unified stream: a consumer attaches after materialization and detaches at
 * teardown. {@link MainAgentEventStream.dispose} ends it (the orchestrator
 * calls it together with the agent release).
 */

import type { AgentHandle } from "@deepseek-ai/dsh-agent";
import type {
  TeamEventStream,
  TeamLiveEvent,
  TeamMemberEvent,
} from "@dominion/dsh-team";

/** The single face role of the sts2 main agent (contract §1/§5). */
export const MAIN_MEMBER_ROLE = "main";

/**
 * The main member's unified event stream: one agent's log/live productions
 * labelled with the main envelope. Constructed once per materialization with
 * the live handle the orchestrator created.
 */
export class MainAgentEventStream implements TeamEventStream {
  private readonly memberId: string;
  private readonly consumers = new Set<(event: TeamMemberEvent) => void>();
  private readonly offs: Array<() => void> = [];
  private disposed = false;

  constructor(handle: AgentHandle) {
    const agent = handle.agent;
    // An agent's id is its session-backed identity
    // (common/js/dsh-plugins/team/src/team.ts agentMemberSource uses the same
    // value for the member identity).
    this.memberId = String(agent.id);
    this.offs.push(
      agent.ctx.on("session/event", (session, event) => {
        if (session.id !== agent.id) {
          return;
        }
        this.notifyLog(event);
      }),
      agent.ctx.on("agent/assistant-stream", (payload) => {
        if (payload.agent !== agent) {
          return;
        }
        this.notifyLive({ kind: "assistant-stream", frame: payload.frame });
      }),
      agent.ctx.on("agent/status", (payload) => {
        if (payload.agent !== agent) {
          return;
        }
        this.notifyLive({ kind: "status", status: payload.status });
      }),
      agent.ctx.on("agent/error", (payload) => {
        if (payload.agent !== agent) {
          return;
        }
        this.notifyLive({ kind: "error", error: payload.error });
      }),
    );
  }

  /**
   * Join the stream. After dispose the stream has ended and the returned
   * detach is a no-op (the team stream's end-of-registration semantics).
   */
  subscribe(consumer: (event: TeamMemberEvent) => void): () => void {
    if (this.disposed) {
      return () => {};
    }
    this.consumers.add(consumer);
    return () => {
      this.consumers.delete(consumer);
    };
  }

  /**
   * End the stream: unsubscribe the agent faces and drop the consumers
   * (idempotent). The orchestrator calls this on dispose/rollback; the agent
   * handle's own teardown would also unwind the subscriptions.
   */
  dispose(): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    for (const off of this.offs.splice(0)) {
      off();
    }
    this.consumers.clear();
  }

  /** Fan one log production out under the main envelope. */
  private notifyLog(event: Extract<TeamMemberEvent, { channel: "log" }>["event"]): void {
    this.publish({ role: MAIN_MEMBER_ROLE, memberId: this.memberId, channel: "log", event });
  }

  /** Fan one live production out under the main envelope. */
  private notifyLive(event: TeamLiveEvent): void {
    this.publish({ role: MAIN_MEMBER_ROLE, memberId: this.memberId, channel: "live", event });
  }

  private publish(envelope: TeamMemberEvent): void {
    for (const consumer of [...this.consumers]) {
      consumer(envelope);
    }
  }
}
