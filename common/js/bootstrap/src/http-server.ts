/**
 * node:http Server adapter.
 *
 * Signature and two-phase stop semantics follow
 * `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §6`
 * (Go reference: common/gopkg/bootstrap/http.go — the Serve-loop error there
 * is carried by the exited promise here).
 */

import type * as http from "node:http";
import { error, info } from "@dominion/common-js-logs";
import { Stage, type Component, type ExitWatchable } from "./component.js";

export interface HttpServerComponentOptions {
  port: number;
  host?: string;
}

/**
 * Server-stage component wrapping an existing http.Server: start = listen
 * (an error event, e.g. EADDRINUSE, rejects the start), stop = two-phase
 * close() drain then closeAllConnections() once the budget signal aborts.
 * The exited promise carries unexpected close/error so the orchestrator can
 * trigger a global shutdown.
 */
export function createHttpServerComponent(
  name: string,
  server: http.Server,
  options: HttpServerComponentOptions,
): Component & ExitWatchable {
  let started = false;
  // Set as soon as a stop is requested: the close event settles `exited`
  // with undefined (an intended shutdown) only in this state.
  let stopping = false;
  // The close/error listeners stay attached for the server's lifetime (the
  // component does not own the server); this guard keeps the settlement
  // single-shot regardless of how many of them fire.
  let exitedSettled = false;

  let settleExited: (err: Error | undefined) => void = () => {};
  const exited = new Promise<Error | undefined>((resolve) => {
    settleExited = resolve;
  });
  const settleExitedOnce = (err: Error | undefined) => {
    if (exitedSettled) return;
    exitedSettled = true;
    settleExited(err);
  };
  server.once("close", () => {
    settleExitedOnce(
      stopping
        ? undefined
        : new Error(`bootstrap: http server "${name}" closed unexpectedly`),
    );
  });

  return {
    name,
    stage: Stage.Server,
    exited,

    async start(): Promise<void> {
      if (started) {
        throw new Error(`bootstrap: http server "${name}" already started`);
      }
      await new Promise<void>((resolve, reject) => {
        const onError = (err: Error) => reject(err);
        server.once("error", onError);
        const onListening = () => {
          server.removeListener("error", onError);
          // A runtime error while serving counts as an unexpected exit.
          server.on("error", (err) => settleExitedOnce(err));
          resolve();
        };
        if (options.host) {
          server.listen(options.port, options.host, onListening);
        } else {
          server.listen(options.port, onListening);
        }
      });
      started = true;
      info("http server started", { component: name, port: options.port });
    },

    async stop(signal: AbortSignal): Promise<void> {
      if (!started) return;
      stopping = true;
      // Phase 1: stop accepting connections and drain; phase 2: once the
      // budget signal aborts, force every remaining connection closed so
      // the close can finish; stop settles when the close settles. close is
      // initiated first in both branches and closeAllConnections runs
      // afterwards, per the Node-recommended ordering.
      const closed = new Promise<void>((resolve, reject) => {
        server.close((err) => {
          signal.removeEventListener("abort", onAbort);
          if (err && (err as NodeJS.ErrnoException).code !== "ERR_SERVER_NOT_RUNNING") {
            reject(err);
            return;
          }
          resolve();
        });
      });
      const onAbort = () => server.closeAllConnections();
      if (signal.aborted) onAbort();
      else signal.addEventListener("abort", onAbort, { once: true });
      try {
        await closed;
      } catch (err) {
        // started stays true so a failed stop can be retried while the
        // listener may still be held (same retry semantics as health stop).
        error("http server stop failed", { component: name, err: err as Error });
        throw err;
      }
      started = false;
      info("http server stopped", { component: name, port: options.port });
    },
  };
}
