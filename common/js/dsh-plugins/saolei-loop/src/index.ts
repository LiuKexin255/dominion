/**
 * cordis plugin entry for the saolei agent loop: the concrete AgentFactory
 * replacing the official agent-loop (setFactory on the shared registry).
 * `name` is the plugin identifier the cordis Loader resolves from the
 * composition manifest.
 *
 * Class-form Service plugin (official dsh-agent-loop `AgentLoop` convention —
 * the Loader unwraps `exports.default` and constructs the class with
 * `(ctx, config)`): the constructor claims the agent-creation factory through
 * an effect and registers the loop's prompt template variables. Per-agent
 * game state is NOT a host-level service: the factory's prepare phase
 * registers a `GameRuntimeService` instance as the agent scope's `saoleiGame`
 * service (cordis Service contract — unregistered automatically when the
 * owning agent scope unloads, saolei-plugins.md §2.1 / research.md D6).
 * Contract: specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §2.
 */

import { Service, type Context } from "@deepseek-ai/cordis";
import z from "@deepseek-ai/schemastery";
import { emitAgentEvent } from "@deepseek-ai/dsh-agent";
import type {
  Agent,
  AgentFactory,
  AgentHandle,
  AgentOptions,
  CreateAgentOptions,
  ResumeAgentOptions,
} from "@deepseek-ai/dsh-agent";
import { SessionPreparation } from "@deepseek-ai/dsh-session";
import type { SessionId as SessionIdType } from "@deepseek-ai/dsh-session";

import {
  DEFAULT_MAX_PARALLEL_TOOL_CALLS,
  SaoleiLoopAgent,
  withResolvers,
} from "./driver.js";
import { createAgentGameRuntime } from "./game/runtime.js";
import type { SaoleiGame } from "./game/runtime.js";

export { DEFAULT_MAX_PARALLEL_TOOL_CALLS } from "./driver.js";
export { SaoleiLoopAgent } from "./driver.js";
export type { DriverSchedulerConfig } from "./driver.js";
export {
  createAgentGameRuntime,
  GameRuntimeService,
} from "./game/runtime.js";
export type {
  CellOperation,
  GameEventRecord,
  GameLogEntry,
  GameRuntime,
  GameRuntimeDeps,
  GameStats,
  OperationType,
  OperateInput,
  SaoleiGame,
  ToolOutcome,
} from "./game/runtime.js";

export const name = "saolei-loop";

/**
 * Merge-extensible per-agent options: the persona injection seam
 * (research D3). The persona lands in the system prompt as the agent-scoped
 * `deployment:persona` section (order 0), shadowing the global persona.
 */
declare module "@deepseek-ai/dsh-agent" {
  interface AgentOptions {
    persona?: string;
  }
}

/**
 * The player's default base prompt — the materialization fallback when the
 * preset carries an empty `player_prompt` (agent-api.md §2.1 persona rule;
 * v1 source projects/game/agent/src/team/player.ts DEFAULT_PLAYER_BASE).
 */
export const DEFAULT_PLAYER_BASE =
  "你是扫雷游戏的操作者（player）。你的职责是操作桌面上的扫雷窗口完成一局游戏：" +
  "使用 saolei 工具落子（开新局、点击/标记/双击揭示格子、查询剩余雷数），" +
  "根据返回的文本棋盘持续推理并落子，直到一局以 won/lost 结束或你判断应当停止。" +
  "你独占桌面控制，不要等待其他 agent 的指令；每局结束后你可以自行决定是否开新局。" +
  "复盘规划者（planner）可能不时向你发送策略指令（作为对话中的消息），" +
  "你应将其视为对你后续对局的校准指导。";

declare module "@deepseek-ai/cordis" {
  interface Context {
    /**
     * The calling agent's game runtime — agent-scoped only: visible on
     * `agent.ctx` and its derived scopes (undefined on the host/root
     * context), unregistered when the agent scope unloads.
     */
    saoleiGame?: SaoleiGame;
  }
}

/** Validated configuration owned by the saolei-loop service. */
export interface SaoleiLoopConfig {
  maxParallelToolCalls: number;
}

/** Reject a scheduler cap that cannot drive the bounded rolling pool. */
function resolveMaxParallelToolCalls(value: number | undefined): number {
  const maxParallelToolCalls = value ?? DEFAULT_MAX_PARALLEL_TOOL_CALLS;
  if (!Number.isInteger(maxParallelToolCalls) || maxParallelToolCalls < 1) {
    throw new Error("maxParallelToolCalls must be a positive integer");
  }
  return maxParallelToolCalls;
}

/** Reject an output-token cap that cannot be represented on the request wire
 * (official `assertAgentOptions`). */
function assertAgentOptions(options: AgentOptions): void {
  if (
    options.maxTokens !== undefined &&
    (!Number.isSafeInteger(options.maxTokens) || options.maxTokens <= 0)
  ) {
    throw new TypeError("agent maxTokens must be a positive safe integer");
  }
}

/** Wrap an abort reason as a creation failure. Non-Error reasons (the
 * AgentCancelCause plain objects carried by `cancel`) are inlined into the
 * message — the ES2020 target's Error constructor has no `cause` option. */
function abortReasonError(signal: AbortSignal, id: string): Error {
  const reason = signal.reason;
  if (reason instanceof Error) {
    return reason;
  }
  const detail =
    typeof reason === "object" && reason !== null
      ? JSON.stringify(reason)
      : String(reason ?? "");
  return new Error(`agent "${id}" creation aborted (${detail})`);
}

/** Await `operation`, or throw the signal's reason as soon as it aborts
 * (official `raceAbort`). */
async function raceAbort<T>(
  operation: Promise<T>,
  signal: AbortSignal,
  id: string,
): Promise<T> {
  if (signal.aborted) {
    throw abortReasonError(signal, id);
  }
  const aborted = withResolvers<never>();
  const listener = () => {
    aborted.reject(abortReasonError(signal, id));
  };
  signal.addEventListener("abort", listener, { once: true });
  try {
    return await Promise.race([operation, aborted.promise]);
  } finally {
    signal.removeEventListener("abort", listener);
  }
}

/**
 * Fiber states that cannot own or serve a new lifecycle: FAILED/DISPOSED/
 * UNLOADING (cordis FiberState 3/4/5; the numeric set mirrors the official
 * loop — the enum is a `const enum`, which this repo's ESM pipeline forbids
 * consuming, style/javascript.md 类型再导出 rule).
 */
const INACTIVE_FIBER_STATES = new Set([5, 4, 3]);

/** Factory-level ownership: live agent teardowns plus startup work (official
 * `FactoryOwnership`; survey §4.7 item 7). Plugin卸载时拒绝新工作、全量
 * abort、等待 startup/settlement 静默。 */
class FactoryOwnership {
  private readonly fiber: { state: number };
  private accepting = true;
  private readonly teardown = new AbortController();
  private readonly liveAgents = new Set<() => Promise<void> | void>();
  private readonly startupTasks = new Set<Promise<unknown>>();

  constructor(fiber: { state: number }) {
    this.fiber = fiber;
  }

  /** Aborts (reason: loop-not-active error) when factory teardown begins. */
  get signal(): AbortSignal {
    return this.teardown.signal;
  }

  isActive(): boolean {
    return this.accepting && !INACTIVE_FIBER_STATES.has(this.fiber.state);
  }

  /** Track one live agent's teardown until it has run. */
  track(dispose: () => Promise<void> | void): () => void {
    this.liveAgents.add(dispose);
    return () => {
      this.liveAgents.delete(dispose);
    };
  }

  /** Join startup work that begins before an agent exists. */
  trackStartup(job: Promise<unknown>): void {
    this.startupTasks.add(job);
    const forget = () => {
      this.startupTasks.delete(job);
    };
    job.then(forget, forget);
  }

  /** Join one public create/resume continuation; dispose awaits settlement. */
  trackWrapper(job: Promise<unknown>): void {
    this.trackStartup(
      job.then(
        () => undefined,
        () => undefined,
      ),
    );
  }

  async dispose(): Promise<void> {
    this.accepting = false;
    this.teardown.abort(new Error("saolei loop is not active"));
    await Promise.all([
      ...[...this.liveAgents].map((dispose) => dispose()),
      ...this.startupTasks,
    ]);
  }
}

/** Per-agent collaborators the factory builds alongside the driver. */
export interface SaoleiLoopPluginOptions {
  /**
   * Builds and registers the per-agent GameRuntime as the agent scope's
   * `saoleiGame` service. The default wires the production
   * {@link createAgentGameRuntime} to the composed `desktopBridge`; tests
   * inject a builder over fake dispatch/board doubles
   * (style/javascript.md Mock convention).
   */
  createRuntime?: (agent: Agent) => SaoleiGame;
}

/** Downlevel `Symbol.dispose` invocation (the ES2020 lib lacks the
 * well-known symbol that `SessionPreparation` implements). */
function disposePreparation(preparation: SessionPreparation): void {
  const disposeSymbol = (Symbol as { dispose?: symbol }).dispose;
  if (disposeSymbol !== undefined) {
    const disposable = preparation as unknown as Record<symbol, (() => void) | undefined>;
    disposable[disposeSymbol]?.();
  }
}

/**
 * The concrete agent factory. Constructed by the Loader from the composition
 * row `{ id: saolei-loop, name: '@dominion/dsh-saolei-loop' }`. Registered as
 * the `saoleiLoop` service (official AgentLoop registers `agentLoop`
 * likewise); no consumer injects it — the agent-scoped `saoleiGame` service
 * created per agent is the consumed face.
 */
export class SaoleiLoopPlugin extends Service implements AgentFactory {
  static inject = [
    "agents",
    "sessions",
    "llm",
    "tools",
    "systemPrompt",
    "desktopBridge",
  ];

  /** Runtime schema for the service config. */
  static Config = z.object({
    maxParallelToolCalls: z.number().step(1).min(1).default(10),
  }) as unknown as z<SaoleiLoopConfig>;

  /** Validated configuration (scheduler cap resolution at construction). */
  readonly config: SaoleiLoopConfig;

  /**
   * Plain holder prevents Cordis from re-tracing the factory's dependency
   * context through a caller shadow (official AgentLoop.runtime).
   */
  private readonly runtime: { ctx: Context };

  private readonly ownership: FactoryOwnership;

  private readonly createRuntime: (agent: Agent) => SaoleiGame;

  constructor(ctx: Context, config: SaoleiLoopConfig, options?: SaoleiLoopPluginOptions) {
    super(ctx, "saoleiLoop");
    this.config = {
      maxParallelToolCalls: resolveMaxParallelToolCalls(config.maxParallelToolCalls),
    };
    this.runtime = { ctx };
    this.ownership = new FactoryOwnership(ctx.fiber);
    const desktopBridge = ctx.desktopBridge;
    this.createRuntime =
      options?.createRuntime ?? ((agent: Agent) => createAgentGameRuntime(agent, desktopBridge));
    ctx.effect(() => () => this.ownership.dispose(), "saolei-loop.transactions()");
    ctx.effect(() => ctx.agents.setFactory(this), "saolei-loop.setFactory()");
    // Loop template variables (official loop registers provider/model/cwd;
    // this loop owns model/cwd per the factory contract).
    ctx.systemPrompt.variable("model", (context) => context.agent?.options.model);
    ctx.systemPrompt.variable("cwd", (context) => context.agent?.session.header.cwd);
  }

  /**
   * Construct the driver, scope, and one memoized reverse teardown for a new
   * agent; the teardown registers with the factory and the owner fiber
   * BEFORE publication so a mid-setup unload rolls everything back (official
   * `prepare`). Also wires the agent-scoped persona section and registers
   * the agent-scoped game runtime.
   */
  private prepare(
    ownerCtx: Context,
    id: SessionIdType,
    options: AgentOptions,
    session: SessionPreparation["session"],
    callerSignal: AbortSignal | undefined,
  ): {
    agent: SaoleiLoopAgent;
    signal: AbortSignal;
    publish: (source: "startup" | "resume") => { agent: Agent; dispose: () => Promise<void> };
    dispose: () => Promise<void>;
  } {
    assertAgentOptions(options);
    ownerCtx.fiber.assertActive();
    if (!this.ownership.isActive()) {
      throw new Error("saolei loop is not active");
    }
    if (callerSignal?.aborted) {
      throw abortReasonError(callerSignal, id);
    }
    const loopCtx = this.runtime.ctx;
    const abort = new AbortController();
    const onCallerAbort = () => {
      abort.abort(
        callerSignal !== undefined ? abortReasonError(callerSignal, id) : new Error(`agent "${id}" creation aborted`),
      );
    };
    const onFactoryTeardown = () => {
      abort.abort(this.ownership.signal.reason);
    };
    callerSignal?.addEventListener("abort", onCallerAbort, { once: true });
    this.ownership.signal.addEventListener("abort", onFactoryTeardown, { once: true });
    let machine: SaoleiLoopAgent | undefined;
    let detachSession: (() => void) | undefined;
    let detachAgent: (() => void) | undefined;
    let disposing: Promise<void> | undefined;
    const machineReady = withResolvers<void>();
    const dispose = (ownerTriggered = false): Promise<void> =>
      (disposing ??= (async () => {
        abort.abort(new Error(`agent "${id}" lifecycle disposed`));
        callerSignal?.removeEventListener("abort", onCallerAbort);
        this.ownership.signal.removeEventListener("abort", onFactoryTeardown);
        try {
          if (machine === undefined) {
            await machineReady.promise;
          }
          if (machine !== undefined) {
            machine.cancel({ kind: "disposed" });
            await machine.whenIdle();
            await machine.scope.dispose();
          }
        } finally {
          try {
            detachAgent?.();
            detachSession?.();
          } finally {
            untrack();
            if (!ownerTriggered) {
              await unfollowOwner();
            }
          }
        }
      })());
    const untrack = this.ownership.track(dispose);
    let unfollowOwner: () => Promise<void> | void;
    try {
      unfollowOwner = ownerCtx.effect(() => {
        return () => {
          if (disposing !== undefined) {
            return;
          }
          abort.abort(new Error(`agent "${id}" setup aborted: owner disposed during setup`));
          return dispose(true);
        };
      }, `saolei-loop.lifecycle(${id})`);
    } catch (error) {
      untrack();
      callerSignal?.removeEventListener("abort", onCallerAbort);
      this.ownership.signal.removeEventListener("abort", onFactoryTeardown);
      throw error;
    }
    const assertLive = () => {
      if (!abort.signal.aborted) {
        return;
      }
      throw abort.signal.reason instanceof Error
        ? abort.signal.reason
        : new Error(String(abort.signal.reason));
    };
    try {
      machine = new SaoleiLoopAgent(
        loopCtx,
        id,
        options,
        session,
        this.config,
      );
      const agent = machine;
      machineReady.resolve();
      // Agent-scoped persona (research D3): shadows the global
      // `deployment:persona` for this agent only; registered on agent.ctx so
      // it exists before publication and the first prompt assembly, and
      // unwinds with the agent scope.
      agent.ctx.systemPrompt.section({
        name: "deployment:persona",
        order: 0,
        text: options.persona || DEFAULT_PLAYER_BASE,
      });
      // Game runtime (saolei-plugins.md §2.1): the builder constructs the
      // Service-class runtime on agent.ctx — the registration is an effect
      // on the agent scope's backing fiber and cordis unregisters it when
      // the scope unloads. No host-level registry, no manual cleanup.
      this.createRuntime(agent);
      assertLive();
      return {
        agent,
        signal: abort.signal,
        publish: (source) => {
          assertLive();
          detachSession = agent.ctx.sessions.enter(session);
          detachAgent = loopCtx.agents.enter(agent, ownerCtx.agent);
          agent.ctx.sessions.announce(session);
          assertLive();
          loopCtx.agents.announce(agent);
          assertLive();
          emitAgentEvent(loopCtx, agent, "agent/session-start", { source });
          assertLive();
          return { agent, dispose };
        },
        dispose,
      };
    } catch (error) {
      machineReady.resolve();
      void dispose();
      throw error;
    }
  }

  /** Prepare one Agent around an acquired Session, run setup, publish. */
  private async setupAndPublish(
    ownerCtx: Context,
    id: SessionIdType,
    preparation: SessionPreparation,
    agentOptions: AgentOptions,
    setup: ((agentCtx: Context) => unknown) | undefined,
    signal: AbortSignal | undefined,
    source: "startup" | "resume",
  ): Promise<AgentHandle> {
    const session = preparation.session;
    try {
      const prepared = this.prepare(ownerCtx, id, agentOptions, session, signal);
      try {
        const commit = await raceAbort(
          Promise.resolve(setup?.(prepared.agent.ctx)),
          prepared.signal,
          id,
        );
        if (commit !== undefined && commit !== null && typeof commit === "object" && "commit" in commit) {
          (commit as { commit: () => void }).commit();
        }
        return prepared.publish(source);
      } catch (error) {
        await prepared.dispose();
        throw error;
      }
    } finally {
      // Release provider state once the preparation leaves this scope (a
      // no-op for sessions prepared without a persistence seed).
      disposePreparation(preparation);
    }
  }

  /**
   * Create an owned agent on a caller-supplied session identity (official
   * `createAgent` publication sequence: setup → enter session/agent →
   * announcements → `agent/session-start` → loop start).
   */
  async createAgent(ownerCtx: Context, options: CreateAgentOptions): Promise<AgentHandle> {
    const preparation = SessionPreparation.create(
      this.runtime.ctx.sessions.prepare(options.sessionId, {
        ...(options.seed === undefined ? {} : { seed: options.seed }),
        ...(options.meta === undefined ? {} : { meta: options.meta }),
      }),
    );
    const published = this.setupAndPublish(
      ownerCtx,
      options.sessionId,
      preparation,
      options.agentOptions ?? {},
      options.setup,
      options.signal,
      "startup",
    );
    this.ownership.trackWrapper(published);
    return published;
  }

  /**
   * Unreachable by composition (A2: no dsh-session-persistence row is
   * mounted, so the registry can never route a resume here) — fail loud
   * instead of silently cold-starting.
   */
  async resume(_ownerCtx: Context, _options: ResumeAgentOptions): Promise<AgentHandle> {
    throw new Error(
      "saolei-loop cannot resume: session persistence is not part of the composition (materialize agents with ctx.agents.create instead)",
    );
  }
}

export default SaoleiLoopPlugin;
