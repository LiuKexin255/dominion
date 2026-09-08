/**
 * Bootstrap lifecycle orchestrator.
 *
 * Public surface and the run() orchestration sequence follow
 * `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §3`
 * (semantics aligned with Go `common/gopkg/bootstrap/bootstrap.go` RunSignal
 * and `specs/052-deploy-health-probe/contracts/bootstrap-health.md §1`).
 */

import { error, info } from "@dominion/common-js-logs";
import type { Component, ExitWatchable } from "./component.js";
import { createHealthServer, type HealthHandle, type HealthService } from "./health.js";
import { raceAbort, waitForAbort } from "./signal.js";

const DEFAULT_SHUTDOWN_TIMEOUT_MS = 5000;
const DEFAULT_SIGNALS: NodeJS.Signals[] = ["SIGTERM", "SIGINT"];

export interface BootstrapOptions {
  /** Unified shutdown budget (ms) shared by the health endpoint and every component stop. */
  shutdownTimeoutMs?: number;
  /**
   * @internal test seam replacing the built-in health endpoint. Production
   * code MUST NOT pass it (specs/053-js-bootstrap-migration/research.md D5).
   */
  healthServerFactory?: () => HealthService;
}

export interface RunOptions {
  /** External cancellation signal, equivalent to a shutdown process signal. */
  signal?: AbortSignal;
  /** Process signals that trigger the graceful stop. */
  signals?: NodeJS.Signals[];
}

function hasExited(c: Component): c is Component & ExitWatchable {
  return "exited" in c;
}

/** Minimal stoppable surface shared by components and the health service. */
interface StopTarget {
  readonly name: string;
  stop(signal: AbortSignal): Promise<void>;
}

function wrapError(err: unknown, message: string): Error {
  const detail = err instanceof Error ? err.message : String(err);
  return new Error(`${message}: ${detail}`);
}

export class Bootstrap {
  private readonly components: Component[] = [];
  private readonly shutdownTimeoutMs: number;
  private readonly healthServerFactory: () => HealthService;
  private runStarted = false;
  private healthHandle: HealthHandle | undefined;

  constructor(options?: BootstrapOptions) {
    this.shutdownTimeoutMs = options?.shutdownTimeoutMs ?? DEFAULT_SHUTDOWN_TIMEOUT_MS;
    this.healthServerFactory = options?.healthServerFactory ?? createHealthServer;
  }

  /** Health handle while running (fault-injection seam); undefined otherwise. */
  get health(): HealthHandle | undefined {
    return this.healthHandle;
  }

  register(component: Component): void {
    if (this.runStarted) {
      throw new Error(`bootstrap: cannot register component "${component.name}" after run has been called`);
    }
    for (const existing of this.components) {
      if (existing.name === component.name) {
        throw new Error(`bootstrap: duplicate component name "${component.name}"`);
      }
    }
    this.components.push(component);
  }

  async run(options?: RunOptions): Promise<void> {
    if (this.runStarted) {
      throw new Error("bootstrap: run has already been called");
    }
    this.runStarted = true;
    const { signal: externalSignal, signals = DEFAULT_SIGNALS } = options ?? {};

    const exitController = new AbortController();
    const signalListeners: Array<{ name: NodeJS.Signals; fn: () => void }> = [];
    let exitSignal: AbortSignal;
    const sorted = [...this.components].sort((a, b) => {
      if (a.stage !== b.stage) return a.stage - b.stage;
      if (a.name !== b.name) return a.name < b.name ? -1 : 1;
      return 0;
    });

    try {
      // Listeners attach inside try so the finally below removes every one
      // already attached if a later registration throws.
      for (const name of signals) {
        const fn = () => exitController.abort();
        process.on(name, fn);
        signalListeners.push({ name, fn });
      }
      exitSignal = externalSignal
        ? AbortSignal.any([exitController.signal, externalSignal])
        : exitController.signal;

      info("bootstrap starting", { components: sorted.length, signals: signals.join(",") });

      // 1. Sequential start in stage asc, name asc order.
      const started: Component[] = [];
      for (const c of sorted) {
        try {
          await c.start(exitSignal);
        } catch (err) {
          const startErr = wrapError(err, `bootstrap: component "${c.name}" failed to start`);
          error("component start failed, rolling back", { component: c.name, err: startErr });
          const rollbackErrs = await this.stopWithinBudget([...started].reverse());
          throw new AggregateError([startErr, ...rollbackErrs], `bootstrap: startup failed at component "${c.name}"`);
        }
        info("component started", { component: c.name, stage: c.stage });
        started.push(c);
      }

      // 2. Health starts only after every component (endpoint reports "all
      // components up"); any start failure — including one thrown by the
      // factory itself — rolls back exactly like a component start failure
      // (contracts/bootstrap-js-api.md §3.1 step 2).
      let health: HealthService;
      try {
        health = this.healthServerFactory();
        await health.start();
      } catch (err) {
        const healthErr = wrapError(err, "bootstrap: health server failed to start");
        error("health server start failed, rolling back", { err: healthErr });
        const rollbackErrs = await this.stopWithinBudget([...started].reverse());
        throw new AggregateError([healthErr, ...rollbackErrs], "bootstrap: health server failed to start");
      }
      let healthStopped = false;
      this.healthHandle = {
        stop: async () => {
          if (healthStopped) return;
          healthStopped = true;
          try {
            await health.stop(AbortSignal.timeout(this.shutdownTimeoutMs));
          } catch (err) {
            // The handle is fire-and-forget for callers (void-typed usage);
            // a failed stop is logged instead of surfacing an unhandled rejection.
            error("health server stop failed", { err: wrapError(err, "bootstrap: health handle stop failed") });
          }
        },
      };

      // 3. Wait for the exit trigger: process signal / injected signal abort,
      // or any ExitWatchable resolving with an Error.
      const exitWatchers = started.filter(hasExited).map((c) => this.watchExit(c));
      const cause: Error | undefined = await Promise.race([
        waitForAbort(exitSignal).then(() => undefined),
        ...exitWatchers,
      ]);

      // 4. Stop sequence: health first, then components in strict reverse
      // start order, all against one shared budget signal.
      const stopOrder: StopTarget[] = [
        { name: "health", stop: (signal: AbortSignal) => health.stop(signal) },
        ...[...started].reverse(),
      ];
      const stopErrs = await this.stopWithinBudget(stopOrder);

      // 5. Clean signal shutdown resolves; anything else rejects.
      if (cause) {
        throw new AggregateError([cause, ...stopErrs], "bootstrap: run failed");
      }
      if (stopErrs.length > 0) {
        throw new AggregateError(stopErrs, "bootstrap: errors during shutdown");
      }
    } finally {
      for (const { name, fn } of signalListeners) {
        process.removeListener(name, fn);
      }
      this.healthHandle = undefined;
    }
  }

  /** Stops targets in the given order under one shared budget signal. */
  private async stopWithinBudget(stopOrder: StopTarget[]): Promise<Error[]> {
    const budget = new AbortController();
    const timer = setTimeout(
      () => budget.abort(new Error("shutdown budget exceeded")),
      this.shutdownTimeoutMs,
    );
    try {
      const errs: Error[] = [];
      for (const target of stopOrder) {
        try {
          // After the budget aborts, every remaining stop is recorded as
          // failed via the already-aborted signal without awaiting its
          // eventual settlement — "预算内未完成的 stop 记为失败并聚合"
          // (specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §3.1 step 4;
          // specs/053-js-bootstrap-migration/spec.md FR-003).
          await raceAbort(
            Promise.resolve(target.stop(budget.signal)),
            budget.signal,
            (reason) => new Error(`bootstrap: "${target.name}" stop did not finish within the shutdown budget: ${String(reason)}`),
          );
        } catch (err) {
          const stopErr = wrapError(err, `bootstrap: "${target.name}" stop failed`);
          error("stop failed", { target: target.name, err: stopErr });
          errs.push(stopErr);
        }
      }
      return errs;
    } finally {
      clearTimeout(timer);
    }
  }

  /**
   * Maps a component's exited promise into the exit race. A normal exit
   * (resolve without an Error) must not trigger the global shutdown, so it
   * stays pending forever.
   */
  private watchExit(c: Component & ExitWatchable): Promise<Error> {
    return c.exited.then(
      (err) => {
        if (err === undefined) return new Promise<Error>(() => {});
        const detail = err instanceof Error ? err.message : String(err);
        const exitErr = new Error(`bootstrap: component "${c.name}" exited unexpectedly: ${detail}`);
        error("component exited unexpectedly", { component: c.name, err: exitErr });
        return exitErr;
      },
      (err) => {
        const exitErr = wrapError(err, `bootstrap: component "${c.name}" exited unexpectedly`);
        error("component exited unexpectedly", { component: c.name, err: exitErr });
        return exitErr;
      },
    );
  }
}

