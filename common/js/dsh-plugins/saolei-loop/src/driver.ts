/**
 * The saolei agent driver: one `SaoleiLoopAgent` per dsh session, driving
 * turn/step boundaries over queued inbox input (specs/051-agent-v2-dsh-migration/
 * contracts/saolei-plugins.md §2.1, survey/deepseek-harness-agent-loop-prereq.md
 * §4.7). Design mirrors the official `ReactLoopAgent` (the dsh-agent-loop
 * 0.1.1-rc.2 package's lib/index.js, materialized in this repo's node_modules)
 * — "抄设计" per research §7.2 risk 1: same phase machine, checkpoint
 * layout, and decision points, reimplemented against the public
 * dsh-agent/dsh-session/dsh-llm/dsh-tools/dsh-scope surfaces without
 * importing the official loop package. Deltas from the official design,
 * each anchored in the composition (saolei-plugins.md §5):
 * - no runtime-context projection (the manifest mounts system-prompt with
 *   `includeRuntimeContext: false`, so `assemble()` yields no contexts);
 * - no user settings section (research §3.1: settings is redundant for the
 *   self-built loop);
 * - no config-declared agents or resume (agents are materialized by the
 *   host through `ctx.agents.create`; `resume` fails loud — A2).
 */

import {
  Inbox,
  agentEvents,
  assembleContextFor,
} from "@deepseek-ai/dsh-agent";
import type {
  Agent,
  AgentCancelCause,
  AgentOptions,
} from "@deepseek-ai/dsh-agent";
import {
  BlockAssembler,
  LlmError,
  createAssistantMessage,
  createToolResultMessage,
  deepFreeze,
  errorChain,
  markAgentLoopRequest,
} from "@deepseek-ai/dsh-llm";
import type {
  CallId,
  GenerateOptions,
  LlmCallConfig,
  PreparedLlmCall,
  ToolSchema,
  UserMessage,
} from "@deepseek-ai/dsh-llm";
import type { Context } from "@deepseek-ai/cordis";
import {
  canonicalHeader,
  headerEquals,
  type EpochHeader,
  type Session,
  type SessionId,
  type TurnEndCancelCause,
  type TurnEndReason,
} from "@deepseek-ai/dsh-session";
import { createScope } from "@deepseek-ai/dsh-scope";
import type { Scope } from "@deepseek-ai/dsh-scope";
import { renderPrompt } from "@deepseek-ai/dsh-system-prompt";
import type { PromptAssembly } from "@deepseek-ai/dsh-system-prompt";
import { TOOL_ABORTED_BEFORE_DISPATCH, TOOL_RUNTIME_SCHEDULER } from "@deepseek-ai/dsh-tools";
import type {
  ScheduledToolDispatch,
  ScheduledToolPreparation,
  ToolExecutionResult,
  ToolRunContext,
} from "@deepseek-ai/dsh-tools";

/** Default maximum in-flight parallel-safe calls per agent step (official
 * `DEFAULT_MAX_PARALLEL_TOOL_CALLS`). */
export const DEFAULT_MAX_PARALLEL_TOOL_CALLS = 10;

/** Owned scheduler cap handed to each driver by the factory. */
export interface DriverSchedulerConfig {
  maxParallelToolCalls: number;
}

/** Driver lifecycle phases; `setPhase` publishes the observable status. */
type DriverPhase =
  | { kind: "idle"; lastTurn: number }
  | {
      kind: "running";
      abort: AbortController;
      turn: number;
      step: number;
      wakeRequested: boolean;
    }
  | {
      kind: "maintenance";
      abort: AbortController;
      lastTurn: number;
      wakeRequested: boolean;
    };

/** One pre-step outcome: reject closes the turn `blocked`, enter proceeds
 * with the claimed messages plus the assembled prompt (official
 * `preStep` attaches its own assembly to the enter decision). */
type StepProposal =
  | { kind: "reject" }
  | { kind: "enter"; messages: UserMessage[]; assembly: PromptAssembly };

/** The pending `tool-call` block shape the assembler produces. */
interface PendingToolCall {
  id: CallId;
  name: string;
  arguments: string;
}

/** Parse model arguments, preserving invalid JSON as text and mapping empty
 * input to `{}` (official `parseArguments`). */
function parseArguments(raw: string): unknown {
  try {
    return raw ? JSON.parse(raw) : {};
  } catch {
    return raw;
  }
}

/** Reverse scan replacement for `Array.findLast` (ES2020 lib). */
function lastTurnNumber(events: readonly { type: string; data: unknown }[]): number {
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i];
    if (event?.type === "turn/start") {
      return (event.data as { turn: number }).turn;
    }
  }
  return 0;
}

/** Promise resolution trio (ES2020-compatible `Promise.withResolvers`). */
function withResolvers<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason?: unknown) => void;
} {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/**
 * Schedule one assistant step's tool calls (official `executeToolCalls` +
 * `runGroup`): exclusive calls form barriers, parallel calls use a bounded
 * rolling pool, results and contexts commit in model order. Abort records
 * synthetic error results for skipped calls so the "tool_call 必有
 * tool_result" wire invariant (data-model.md §4.2) also holds on the
 * cancellation path; a scheduler failure preserves recorded `tool/call`
 * events without fabricating results.
 */
async function executeToolCalls(
  loopCtx: Context,
  schedulerConfig: DriverSchedulerConfig,
  initiator: Agent,
  turn: number,
  step: number,
  toolCalls: PendingToolCall[],
  signal: AbortSignal,
  acceptContext: (context: UserMessage) => void,
): Promise<{ concluded: boolean }> {
  const { session } = initiator;
  const planned = toolCalls.map((block) => ({
    block,
    exec: {
      callId: block.id,
      name: block.name,
      arguments: parseArguments(block.arguments),
      agent: initiator,
      signal,
    },
  }));
  let next = 0;
  let concluded = false;
  while (next < planned.length) {
    const first = planned[next]!;
    const mode = loopCtx.tools.executionMode(first.exec).kind;
    const outcome = await runGroup(
      loopCtx,
      schedulerConfig,
      initiator,
      turn,
      step,
      mode === "parallel" ? planned.slice(next) : [first],
      mode,
      signal,
      acceptContext,
    );
    next += outcome.consumed;
    concluded ||= outcome.concluded;
    if (outcome.aborted) {
      for (const call of planned.slice(next)) {
        appendSkippedToolCall(session, turn, step, call.block);
      }
      return { concluded };
    }
  }
  return { concluded };
}

async function runGroup(
  loopCtx: Context,
  schedulerConfig: DriverSchedulerConfig,
  initiator: Agent,
  turn: number,
  step: number,
  group: Array<{
    block: PendingToolCall;
    exec: {
      callId: CallId;
      name: string;
      arguments: unknown;
      agent: Agent;
      signal: AbortSignal;
    };
  }>,
  mode: "parallel" | "exclusive",
  signal: AbortSignal,
  acceptContext: (context: UserMessage) => void,
): Promise<{ consumed: number; aborted: boolean; concluded: boolean }> {
  const { session } = initiator;
  const slots: Array<
    { exec: ToolRunContext; result: ToolExecutionResult; needsPost: boolean } | undefined
  > = group.map(() => undefined);
  const callSeqs = group.map(() => -1);
  let nextToStart = 0;
  let committed = 0;
  let started = 0;
  let aborted = signal.aborted;
  let concluded = false;
  let schedulerFailure: { error: unknown } | undefined;
  const scheduler = loopCtx.tools[TOOL_RUNTIME_SCHEDULER];
  const throwSchedulerFailure = () => {
    if (schedulerFailure !== undefined) {
      throw schedulerFailure.error;
    }
  };
  const commitReady = async () => {
    while (committed < group.length) {
      const slot = slots[committed];
      if (slot === undefined) {
        break;
      }
      const call = group[committed]!;
      const result = slot.needsPost
        ? await scheduler.finalize(slot.exec, slot.result)
        : scheduler.finish(slot.exec, slot.result);
      appendToolResult(session, turn, step, call.block, result, callSeqs[committed]!);
      for (const context of result.additionalContexts ?? []) {
        acceptContext(context);
      }
      concluded ||= result.concludesTurn === true;
      committed++;
    }
  };
  const inFlight = new Map<number, Promise<number>>();
  const startCall = async (index: number) => {
    const call = group[index]!;
    callSeqs[index] = appendToolCall(session, turn, step, call.block);
    started++;
    const prepared: ScheduledToolPreparation = await scheduler.prepare(call.exec);
    throwSchedulerFailure();
    switch (prepared.kind) {
      case "dispatch": {
        const promise: Promise<number> = scheduler
          .dispatch(prepared.exec)
          .then(
            (outcome: ScheduledToolDispatch) => {
              slots[index] = {
                exec: prepared.exec,
                result: outcome.result,
                needsPost: outcome.kind === "post-result",
              };
              return index;
            },
            (error: unknown) => {
              schedulerFailure ??= { error };
              return index;
            },
          );
        inFlight.set(index, promise);
        break;
      }
      case "post-result":
        slots[index] = {
          exec: prepared.exec,
          result: prepared.result,
          needsPost: true,
        };
        break;
      case "final-result":
        slots[index] = {
          exec: prepared.exec,
          result: prepared.result,
          needsPost: false,
        };
        break;
    }
  };
  const fillPool = async () => {
    while (
      !aborted &&
      nextToStart < group.length &&
      inFlight.size < schedulerConfig.maxParallelToolCalls
    ) {
      const nextCall = group[nextToStart]!;
      if (
        nextToStart > 0 &&
        mode === "parallel" &&
        loopCtx.tools.executionMode(nextCall.exec).kind !== "parallel"
      ) {
        break;
      }
      await startCall(nextToStart);
      nextToStart++;
      throwSchedulerFailure();
      await commitReady();
      throwSchedulerFailure();
      if (signal.aborted) {
        aborted = true;
      }
    }
  };
  try {
    await fillPool();
    while (inFlight.size > 0) {
      const settledIndex = await Promise.race(inFlight.values());
      inFlight.delete(settledIndex);
      throwSchedulerFailure();
      await commitReady();
      throwSchedulerFailure();
      if (signal.aborted) {
        aborted = true;
      }
      await fillPool();
    }
  } catch (error) {
    schedulerFailure ??= { error };
    await Promise.allSettled(inFlight.values());
    throw schedulerFailure.error;
  }
  if (aborted) {
    for (const call of group.slice(started)) {
      appendSkippedToolCall(session, turn, step, call.block);
    }
    return { consumed: group.length, aborted: true, concluded };
  }
  return { consumed: started, aborted: false, concluded };
}

/** Append the durable call/result pair for a model call skipped after
 * cancellation (official `appendSkippedToolCall`). */
function appendSkippedToolCall(
  session: Session,
  turn: number,
  step: number,
  block: PendingToolCall,
): void {
  const callSeq = appendToolCall(session, turn, step, block);
  appendToolResult(
    session,
    turn,
    step,
    block,
    {
      content: [{ type: "text", text: "Error: tool call aborted before dispatch" }],
      isError: true,
      error: {
        message: "tool call aborted before dispatch",
        info: { name: "AbortError", code: TOOL_ABORTED_BEFORE_DISPATCH },
      },
    },
    callSeq,
  );
}

/** Append a started call and return the event seq its result must cite. */
function appendToolCall(
  session: Session,
  turn: number,
  step: number,
  block: PendingToolCall,
): number {
  return session
    .append("tool/call", {
      turn,
      step,
      callId: block.id,
      name: block.name,
      arguments: block.arguments,
    })
    .seq;
}

/** Append a model-ordered result linked to its call event. */
function appendToolResult(
  session: Session,
  turn: number,
  step: number,
  block: PendingToolCall,
  result: ToolExecutionResult,
  callSeq: number,
): void {
  const message = createToolResultMessage({
    callId: block.id,
    content: result.content,
    isError: result.isError,
  });
  session.append(
    "tool/result",
    {
      turn,
      step,
      message,
      ...(result.error?.info ? { error: result.error.info } : {}),
      ...(result.meta !== undefined ? { meta: result.meta as never } : {}),
    },
    {
      surfaceOp: "append",
      sourceEventSeqs: [callSeq],
    },
  );
}

/**
 * The concrete driver over queued turns and step-boundary input; every
 * request is derived from the session log. Implements the full dsh `Agent`
 * interface (dsh-agent runtime-types) — factory contract saolei-plugins.md
 * §2.1.
 */
export class SaoleiLoopAgent implements Agent {
  private readonly loopCtx: Context;
  readonly id: SessionId;
  readonly options: AgentOptions;
  readonly session: Session;
  readonly inbox: Inbox;
  private phase: DriverPhase;
  private activityDone: Promise<void> = Promise.resolve();
  /** The agent-scoped registration boundary; the lifecycle owner unwinds it. */
  readonly scope: Scope;
  readonly ctx: Context;
  /** Fused dispatcher, built once so hot-path dispatches never allocate. */
  private readonly dispatch: ReturnType<typeof agentEvents>;
  /** Whether this loop instance has appended its initial request anchor. */
  private requestHeaderLogged = false;
  private readonly schedulerConfig: DriverSchedulerConfig;

  constructor(
    loopCtx: Context,
    id: SessionId,
    options: AgentOptions,
    session: Session,
    schedulerConfig: DriverSchedulerConfig,
  ) {
    this.loopCtx = loopCtx;
    this.id = id;
    this.options = options;
    this.session = session;
    this.schedulerConfig = schedulerConfig;
    this.dispatch = agentEvents(loopCtx, this);
    this.inbox = new Inbox(session, {
      inserted: (message) => {
        this.dispatch.emit("agent/inbox/inserted", { message });
      },
      discarded: (message) => {
        this.dispatch.emit("agent/inbox/discarded", { message });
      },
      claimed: (message, turn) => {
        this.dispatch.emit("agent/inbox/claimed", { message, turn });
      },
    });
    this.phase = { kind: "idle", lastTurn: lastTurnNumber(session.events) };
    // The per-agent service isolation boundary (contracts/saolei-plugins.md
    // §1/§2.1): below this context, `saoleiGame` resolves against a fresh
    // per-agent isolation label, so the Service-class game runtime the
    // factory registers on agent.ctx is visible to this agent (and its
    // derived scopes) while the host/root context — resolving against the
    // shared root label — never sees it (cordis Context.isolate contract;
    // plain Service registration without the label would be process-global,
    // data-model.md §2.5 宿主/根上下文不可见).
    this.scope = createScope(loopCtx, this);
    this.ctx = this.scope.ctx.isolate("saoleiGame").extend({ agent: this });
  }

  get status(): "idle" | "running" {
    return this.phase.kind === "running" ? "running" : "idle";
  }

  /** Commit a phase and publish its externally visible status transition. */
  private setPhase(next: DriverPhase): void {
    const previousStatus = this.status;
    this.phase = next;
    const status = this.status;
    if (status !== previousStatus) {
      this.dispatch.emit("agent/status", { status });
    }
  }

  send(message: UserMessage, target: "next-turn" | "next-step", wakeup: boolean): void {
    const wakingAfterAbort =
      wakeup && this.phase.kind !== "idle" && this.phase.abort.signal.aborted;
    const resolvedTarget = wakingAfterAbort ? "next-turn" : target;
    this.inbox.splice(resolvedTarget, Number.POSITIVE_INFINITY, 0, [message]);
    if (wakeup) {
      this.wakeDriver(wakingAfterAbort);
    }
  }

  followup(message: UserMessage): void {
    this.send(message, "next-turn", true);
  }

  steer(message: UserMessage): void {
    this.send(message, "next-step", true);
  }

  inject(message: UserMessage): void {
    this.send(message, "next-step", false);
  }

  cancel(cause: AgentCancelCause, options: { keepInbox?: boolean } = {}): void {
    if (!options.keepInbox) {
      this.inbox.clear();
      if (this.phase.kind !== "idle") {
        this.phase.wakeRequested = false;
      }
    }
    if (this.phase.kind !== "idle") {
      this.phase.abort.abort(cause);
    }
  }

  runMaintenance<T>(task: (signal: AbortSignal) => Promise<T>): Promise<T> {
    if (this.phase.kind !== "idle") {
      throw new Error(`agent "${this.id}" already has active work`);
    }
    const done = withResolvers<void>();
    const maintenance: Extract<DriverPhase, { kind: "maintenance" }> = {
      kind: "maintenance",
      abort: new AbortController(),
      lastTurn: this.phase.lastTurn,
      wakeRequested: false,
    };
    this.setPhase(maintenance);
    this.activityDone = done.promise;
    return (async () => {
      try {
        return await task(maintenance.abort.signal);
      } finally {
        this.setPhase({ kind: "idle", lastTurn: maintenance.lastTurn });
        if (maintenance.wakeRequested && this.inbox.hasPending) {
          this.wakeDriver();
        }
        done.resolve();
      }
    })();
  }

  /**
   * Start one driver, or latch its wake behind maintenance or an aborted
   * activity (official `wakeDriver` — the abort-后唤醒 latch, §4.7 item 4).
   */
  private wakeDriver(wakeAfterAbort = false): void {
    if (this.phase.kind !== "idle") {
      const active = this.phase;
      if (
        active.abort.signal.reason?.kind !== "disposed" &&
        (active.kind === "maintenance" || wakeAfterAbort)
      ) {
        active.wakeRequested = true;
      }
      return;
    }
    const driver = withResolvers<void>();
    this.activityDone = driver.promise;
    this.setPhase({
      kind: "running",
      abort: new AbortController(),
      turn: this.phase.lastTurn,
      step: 0,
      wakeRequested: false,
    });
    this.loopCtx.agents
      .withInitiator(this, () => this.kick())
      .then(driver.resolve, driver.reject);
  }

  async whenIdle(): Promise<void> {
    for (;;) {
      const activity = this.activityDone;
      await activity;
      if (activity === this.activityDone) {
        return;
      }
    }
  }

  /** Report one failure at its live boundary, then preserve it for driver
   * containment (§4.7 item 6: `agent/error` 先发后抛). */
  private throwError(error: unknown): never {
    const turn = this.phase.kind === "running" ? this.phase.turn : this.phase.lastTurn;
    const step = this.phase.kind === "running" ? this.phase.step : 0;
    this.dispatch.emit("agent/error", { turn, step, error });
    throw error;
  }

  /** Driver containment: a single agent's failure never escapes to the
   * registry (official `kick`). */
  private async kick(): Promise<void> {
    try {
      while (await this.turn()) {
        // next turn
      }
    } catch {
      // contained: the turn boundary already classified and emitted the
      // failure (`agent/error` / `turn/end`); the phase machine converges in
      // the finally block below.
    } finally {
      if (this.phase.kind === "running") {
        const { turn, wakeRequested } = this.phase;
        this.setPhase({ kind: "idle", lastTurn: turn });
        if (wakeRequested && this.inbox.hasPending) {
          this.wakeDriver();
        }
      }
    }
  }

  private async preStep(
    target: "next-turn" | "next-step",
    position: { turn: number; step: number },
  ): Promise<StepProposal> {
    if (this.phase.kind !== "running") {
      throw new Error(`agent "${this.id}": pre-step outside running phase`);
    }
    const signal = this.phase.abort.signal;
    const claimed = this.inbox.claim(target, position.turn);
    const assembly = await this.loopCtx.systemPrompt.assemble(
      assembleContextFor(this, signal),
    );
    signal.throwIfAborted();
    const decision = await this.dispatch.waterfall(
      "agent/pre-step",
      { messages: claimed, ...position, signal },
      () =>
        Promise.resolve({
          kind: "enter" as const,
          messages: claimed,
        }),
    );
    signal.throwIfAborted();
    return decision.kind === "reject" ? decision : { ...decision, assembly };
  }  /**
   * Open one turn before claiming its first proposed step; the turn boundary
   * owns the `turn/end` fallback with its TurnEndReason (§4.7 item 8).
   */
  private async turn(): Promise<boolean> {
    if (this.phase.kind !== "running") {
      this.throwError(new Error(`agent "${this.id}": turn without driver reservation`));
    }
    const phase = this.phase;
    const { signal } = phase.abort;
    signal.throwIfAborted();
    const turn = phase.turn + 1;
    try {
      this.session.append("turn/start", { turn });
    } catch (error) {
      this.throwError(error);
    }
    phase.turn = turn;
    let turnEnds: TurnEndReason | null = null;
    let target: "next-turn" | "next-step" = "next-turn";
    try {
      for (;;) {
        // §4.7 item 2: abort checkpoints at every turn boundary.
        signal.throwIfAborted();
        const step = phase.step + 1;
        const decision = await this.preStep(target, { turn, step });
        if (decision.kind === "reject") {
          turnEnds = { kind: "blocked" };
          return false;
        }
        const assembly = decision.assembly;
        if (turnEnds !== null && decision.messages.length === 0) {
          break;
        }
        if (phase.step === 0 && decision.messages.length === 0) {
          turnEnds = { kind: "completed" };
          return false;
        }
        signal.throwIfAborted();
        this.session.append("step/start", { turn, step });
        phase.step = step;
        try {
          for (const message of decision.messages) {
            this.session.append("user/message", message, { surfaceOp: "append" });
          }
          const stepEnd = await this.step(assembly);
          if (turnEnds === null || turnEnds.kind !== "max-tokens") {
            turnEnds = stepEnd;
          }
        } finally {
          this.session.append("step/end", { turn, step });
        }
        signal.throwIfAborted();
        if (turnEnds !== null && this.inbox.nextStep.length === 0) {
          // §4.7 item 5: `agent/turn-stopping` serial — a listener that
          // steers re-opens the step loop; data decides, order cannot.
          await this.dispatch.serial("agent/turn-stopping", { turn, signal });
          signal.throwIfAborted();
        }
        if (turnEnds !== null && this.inbox.nextStep.length === 0) {
          break;
        }
        target = "next-step";
      }
    } catch (error) {
      if (signal.aborted) {
        // The only abort sources are `cancel(cause)` and lifecycle disposal,
        // both carrying an AgentCancelCause-compatible reason.
        turnEnds = { kind: "aborted", reason: signal.reason as TurnEndCancelCause };
        throw error;
      }
      turnEnds = {
        kind: "error",
        error:
          error instanceof LlmError
            ? error.failure
            : { message: errorChain(error), code: "UNKNOWN" },
      };
      this.throwError(error);
    } finally {
      try {
        this.session.append("turn/end", { turn, reason: turnEnds as TurnEndReason });
      } catch (error) {
        this.throwError(error);
      }
    }
    if (!this.inbox.hasPending) {
      return false;
    }
    phase.abort = new AbortController();
    phase.wakeRequested = false;
    phase.step = 0;
    return true;
  }

  private async step(
    assembly: PromptAssembly,
  ): Promise<{ kind: "completed" } | { kind: "max-tokens" } | null> {
    if (this.phase.kind !== "running") {
      throw new Error(`agent "${this.id}": step outside running phase`);
    }
    const { turn, step, abort } = this.phase;
    const { signal } = abort;
    signal.throwIfAborted();
    const system = renderPrompt(assembly);
    for (;;) {
      const { request, preparedCall } = await this.buildRequest(
        turn,
        step,
        assembly.tools,
        system,
        this.session.deriveMessages(),
        signal,
      );
      const assembler = new BlockAssembler();
      const chunkSeqs: number[] = [];
      try {
        const stream =
          preparedCall?.stream(request) ?? this.loopCtx.llm.stream(request);
        signal.throwIfAborted();
        for await (const chunk of stream) {
          // §4.7 item 2: abort checkpoint per chunk.
          signal.throwIfAborted();
          chunkSeqs.push(
            this.session.append("assistant/chunk", { turn, step, chunk }).seq,
          );
          assembler.push(chunk);
        }
        signal.throwIfAborted();
      } catch (error) {
        if (signal.aborted) {
          // §4.7 item 3: interrupted stream prefix lands in the log with
          // `interrupted: true`; undispatched tool calls stay absent.
          const content = assembler.interruptedBlocks();
          if (content.length > 0) {
            this.session.append(
              "assistant/message",
              {
                turn,
                step,
                message: createAssistantMessage({
                  content,
                  source: {
                    provider: request.provider,
                    model: request.model,
                  },
                }),
                interrupted: true,
                ...(assembler.usage === undefined ? {} : { usage: assembler.usage }),
              },
              { surfaceOp: "append", sourceEventSeqs: chunkSeqs },
            );
          }
        }
        throw error;
      }
      const finish = assembler.finish;
      if (finish.kind === "error" || finish.kind === "aborted") {
        // §4.7 item 5: `agent/request-error` waterfall — a listener returning
        // `{kind: "retry"}` owns recovery (llm-retry mounts here).
        const action = await this.dispatch.waterfall(
          "agent/request-error",
          {
            turn,
            step,
            provider: request.provider,
            failure: finish.failure,
            retryPolicy: preparedCall?.retryPolicy,
            signal,
          },
          () => Promise.resolve(undefined),
        );
        signal.throwIfAborted();
        if (action?.kind !== "retry") {
          throw new LlmError(finish.failure.message, finish.failure.code, finish.failure);
        }
        continue;
      }
      const message = createAssistantMessage({
        content: assembler.blocks(),
        source: {
          provider: request.provider,
          model: request.model,
        },
      });
      this.session.append(
        "assistant/message",
        {
          turn,
          step,
          message,
          ...(assembler.usage === undefined ? {} : { usage: assembler.usage }),
        },
        { surfaceOp: "append", sourceEventSeqs: chunkSeqs },
      );
      if (finish.kind === "max-tokens") {
        return { kind: "max-tokens" };
      }
      const toolCalls = message.content
        .filter((block): block is Extract<typeof block, { type: "tool-call" }> => block.type === "tool-call")
        .map((block) => ({ id: block.id, name: block.name, arguments: block.arguments }));
      if (toolCalls.length === 0) {
        return { kind: "completed" };
      }
      const { concluded } = await executeToolCalls(
        this.loopCtx,
        this.schedulerConfig,
        this,
        turn,
        step,
        toolCalls,
        signal,
        (context) =>
          this.inbox.splice("next-step", this.inbox.nextStep.length, 0, [context]),
      );
      return concluded ? { kind: "completed" } : null;
    }
  }

  /**
   * Compose one frozen request and bind it to the adapter registration that
   * resolved its exact-model defaults (official `buildRequest`): the
   * `agent/request` waterfall can replace the config, headers are logged as
   * initial/change, and the route context tracks provider/model changes.
   */
  private async buildRequest(
    turn: number,
    step: number,
    tools: ToolSchema[],
    system: string,
    boundaryMessages: ReturnType<Session["deriveMessages"]>,
    signal: AbortSignal,
  ): Promise<{ request: GenerateOptions; preparedCall: PreparedLlmCall | undefined }> {
    const { session } = this;
    const persistedHeader = session.requestHeader();
    const persistedConfig = persistedHeader?.config;
    const route = {
      provider: this.options.provider ?? "",
      model: this.options.model ?? "",
    };
    const reasoningEffort =
      persistedConfig?.provider === route.provider &&
      persistedConfig.model === route.model &&
      persistedHeader?.adapterDefaults?.reasoningEffort !== true
        ? persistedConfig.reasoningEffort
        : undefined;
    const maxTokens = this.options.maxTokens;
    const seedConfig = deepFreeze(
      this.requestHeaderLogged
        ? requestProposal(persistedHeader)
        : {
            ...route,
            ...(reasoningEffort === undefined ? {} : { reasoningEffort }),
            ...(maxTokens === undefined ? {} : { maxTokens }),
          },
    );
    const proposedConfig = await this.dispatch.waterfall(
      "agent/request",
      { turn, step, signal },
      () => Promise.resolve(seedConfig),
    );
    signal.throwIfAborted();
    if (!proposedConfig.provider || !proposedConfig.model) {
      throw new Error(
        `agent "${this.id}" has no provider/model: set AgentOptions.provider and AgentOptions.model or supply both via the agent/request waterfall`,
      );
    }
    let config = proposedConfig;
    let preparedCall: PreparedLlmCall | undefined;
    try {
      preparedCall = await this.loopCtx.llm.prepareCall(proposedConfig, signal);
      config = preparedCall.config;
    } catch (error) {
      if (!(error instanceof LlmError) || error.code !== "NO_ADAPTER") {
        throw error;
      }
      config = proposedConfig;
    }
    signal.throwIfAborted();
    const header = canonicalHeader({
      config,
      ...(preparedCall === undefined ? {} : { adapterDefaults: preparedCall.adapterDefaults }),
      ...(system ? { system } : {}),
      ...(tools.length > 0 ? { tools } : {}),
    });
    const baseline = session.requestHeader();
    if (!this.requestHeaderLogged) {
      session.append("request/header", {
        header,
        reason: baseline === undefined ? "initial" : "resume",
      });
      this.requestHeaderLogged = true;
    } else if (baseline === undefined || !headerEquals(baseline, header)) {
      session.append("request/header", { header, reason: "change" });
    }
    const contextWindow = preparedCall?.context?.contextWindow;
    const requestContext = {
      provider: config.provider,
      model: config.model,
      ...(contextWindow === undefined ? {} : { contextWindow }),
    };
    const previousContext = session.requestContext();
    if (
      previousContext?.provider !== requestContext.provider ||
      previousContext.model !== requestContext.model ||
      previousContext.contextWindow !== requestContext.contextWindow
    ) {
      session.append("request/context", requestContext);
    }
    signal.throwIfAborted();
    return {
      request: markAgentLoopRequest(
        deepFreeze({
          ...header.config,
          messages: boundaryMessages,
          ...(header.system !== undefined ? { system: header.system } : {}),
          ...(header.tools !== undefined ? { tools: header.tools } : {}),
          sessionId: this.session.id,
          signal,
        }),
      ),
      preparedCall,
    };
  }
}

/** Strip adapter-derived values before plugins propose the next request
 * config (official `requestProposal`). */
function requestProposal(header: EpochHeader | undefined): LlmCallConfig {
  if (header?.adapterDefaults === undefined) {
    return { ...(header?.config ?? {}) } as LlmCallConfig;
  }
  const proposal = { ...header.config };
  if (header.adapterDefaults.reasoningEffort === true) {
    delete proposal.reasoningEffort;
  }
  if (header.adapterDefaults.maxTokens === true) {
    delete proposal.maxTokens;
  }
  return proposal;
}

/** Exported for tests (ES2020-compatible resolution trio). */
export { withResolvers };
