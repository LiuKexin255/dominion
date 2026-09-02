/**
 * Daemon worker supervisor.
 *
 * Contract: `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §5`
 * (Go reference: common/gopkg/bootstrap/daemon.go — blocking worker run,
 * exponential backoff min(cur*2, max), restart limit with negative disabling,
 * pluggable error classification, fatal reported via the exited signal).
 */

import { error, info, warn } from "@dominion/common-js-logs";
import { Stage, type Component, type ExitWatchable } from "./component.js";
import { raceAbort } from "./signal.js";

export interface Worker {
  /**
   * Blocks until the worker finishes (resolve) or fails (reject). When the
   * signal cancels, it should reject with an AbortError or resolve cleanly.
   */
  run(signal: AbortSignal): Promise<void>;
}

export type DaemonDecision = "restart" | "stop" | "fatal";

export interface DaemonOptions {
  /** First backoff between restarts. */
  initialBackoffMs?: number;
  /** Backoff cap; each restart doubles the current backoff up to this. */
  maxBackoffMs?: number;
  /** Max restart attempts; a negative value disables the limit; exhausted → fatal. */
  maxRestarts?: number;
  classifyError?: (err: unknown) => DaemonDecision;
}

const DEFAULT_INITIAL_BACKOFF_MS = 1000;
const DEFAULT_MAX_BACKOFF_MS = 30000;
const DEFAULT_MAX_RESTARTS = 5;

function isAbortError(err: unknown): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "name" in err &&
    (err as { name?: unknown }).name === "AbortError"
  );
}

/**
 * Daemon-stage component supervising a worker: it builds a fresh worker per
 * start/restart, restarts after exponential backoff per the configured
 * policy, and reports unrecoverable errors through `exited` so the
 * orchestrator triggers a global shutdown.
 */
export function createDaemon(
  name: string,
  buildWorker: (signal: AbortSignal) => Worker | Promise<Worker>,
  options?: DaemonOptions,
): Component & ExitWatchable {
  const initialBackoffMs = options?.initialBackoffMs ?? DEFAULT_INITIAL_BACKOFF_MS;
  const maxBackoffMs = options?.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS;
  const maxRestarts = options?.maxRestarts ?? DEFAULT_MAX_RESTARTS;

  let settleExited: (err: Error | undefined) => void = () => {};
  const exited = new Promise<Error | undefined>((resolve) => {
    settleExited = resolve;
  });
  let fatalReported = false;

  // The daemon owns a cancellable supervision signal (Go: context.WithCancel
  // over the start signal): stop aborts it to cancel the current worker and
  // the backoff sleep, while the start signal alone can also end supervision.
  const stopController = new AbortController();
  let supervisionSignal: AbortSignal | undefined;
  let supervision: Promise<void> | undefined;
  let restarts = 0;
  let backoff = initialBackoffMs;

  // Default classification (Go defaultErrorClassifier): a clean worker
  // completion stops; an AbortError under a cancelled supervision signal
  // stops; everything else restarts.
  const defaultClassify = (err: unknown): DaemonDecision => {
    if (err === undefined || err === null) return "stop";
    if (isAbortError(err) && supervisionSignal?.aborted) return "stop";
    return "restart";
  };
  const classifyError = options?.classifyError ?? defaultClassify;

  function reportFatal(err: unknown): void {
    if (fatalReported) return;
    fatalReported = true;
    const detail = err instanceof Error ? err.message : String(err);
    const fatalErr = new Error(`bootstrap: daemon "${name}" fatal: ${detail}`);
    error("daemon fatal error", { component: name, err: fatalErr });
    settleExited(fatalErr);
  }

  /** Resolves true when the full backoff elapsed, false when cancelled. */
  function sleepBackoff(ms: number, signal: AbortSignal): Promise<boolean> {
    return new Promise<boolean>((resolve) => {
      if (signal.aborted) {
        resolve(false);
        return;
      }
      const cleanup = () => {
        clearTimeout(timer);
        signal.removeEventListener("abort", onAbort);
      };
      const onAbort = () => {
        cleanup();
        resolve(false);
      };
      const timer = setTimeout(() => {
        cleanup();
        resolve(true);
      }, ms);
      signal.addEventListener("abort", onAbort, { once: true });
    });
  }

  async function applyRestartPolicy(err: unknown, signal: AbortSignal): Promise<boolean> {
    const decision = classifyError(err);
    if (decision === "stop") return false;
    if (decision === "fatal") {
      reportFatal(err);
      return false;
    }
    restarts += 1;
    if (maxRestarts >= 0 && restarts > maxRestarts) {
      error("restart policy exhausted", { component: name, attempts: maxRestarts });
      reportFatal(err);
      return false;
    }
    warn("restarting daemon", { component: name, attempt: restarts, backoffMs: backoff });
    if (!(await sleepBackoff(backoff, signal))) return false;
    backoff = Math.min(backoff * 2, maxBackoffMs);
    return true;
  }

  async function supervise(): Promise<void> {
    const signal = supervisionSignal as AbortSignal;
    try {
      for (;;) {
        if (signal.aborted) return;
        let worker: Worker;
        try {
          worker = await buildWorker(signal);
        } catch (err) {
          error("daemon build failed", { component: name, err: err as Error });
          // Shutdown in progress: a build failure during shutdown is stop
          // noise — neither restart nor fatal, aligned with the worker path
          // below.
          if (signal.aborted) return;
          if (!(await applyRestartPolicy(err, signal))) return;
          continue;
        }
        let failed = false;
        let runErr: unknown;
        try {
          await worker.run(signal);
        } catch (err) {
          failed = true;
          runErr = err;
        }
        // Shutdown in progress: the worker exit is not classified for restart.
        if (signal.aborted) return;
        if (failed) {
          error("daemon worker failed", { component: name, err: runErr as Error });
        } else {
          info("daemon worker finished", { component: name });
        }
        if (!(await applyRestartPolicy(failed ? runErr : undefined, signal))) return;
      }
    } catch (err) {
      // Internal supervision error must not end the loop silently and hang
      // stop() or starve the exit signal.
      reportFatal(err);
    }
  }

  return {
    name,
    stage: Stage.Daemon,
    exited,

    async start(signal: AbortSignal): Promise<void> {
      if (supervision) {
        throw new Error(`bootstrap: daemon "${name}" already started`);
      }
      supervisionSignal = AbortSignal.any([signal, stopController.signal]);
      // The loop never rejects; stop() awaits it.
      supervision = supervise();
    },

    async stop(budgetSignal: AbortSignal): Promise<void> {
      if (!supervision) return;
      stopController.abort();
      await raceAbort(
        supervision,
        budgetSignal,
        (reason) => new Error(`bootstrap: daemon "${name}" stop did not finish within the shutdown budget: ${String(reason)}`),
      );
    },
  };
}
