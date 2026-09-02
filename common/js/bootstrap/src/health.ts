/**
 * Built-in health endpoint served by the Bootstrap orchestrator.
 *
 * Behavior contract: `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §4`
 * (constraints inherited from `specs/052-deploy-health-probe/contracts/bootstrap-health.md §1`:
 * fixed port, FIFO lifecycle, start failure counts as component failure, no
 * config/switch/port validation).
 */

import * as http from "node:http";
import { error, info } from "@dominion/common-js-logs";
import { raceAbort } from "./signal.js";

/** Listens on all interfaces so kubelet can probe via the Pod IP. */
const HEALTH_PORT = 38080;
const HEALTH_PATH = "/healthz";
const HEALTH_BODY = "ok\n";

/**
 * Lifecycle contract the orchestrator needs from the health endpoint.
 * A bind failure (e.g. the port is taken) must surface as a start
 * rejection so it receives the component-start-failure treatment.
 */
export interface HealthService {
  start(): Promise<void>;
  /** Releases the port within the given budget signal. */
  stop(signal: AbortSignal): Promise<void>;
}

/**
 * Controlled stop handle exposed by the running Bootstrap (the only public
 * seam, used for liveness fault injection in experimental services; no
 * start/restart — not an on/off switch).
 */
export interface HealthHandle {
  stop(): Promise<void>;
}

/**
 * Builds the built-in health service: `GET /healthz` → 200 `ok\n`, every
 * other request → 404. No drain requirement, so stop only has to guarantee
 * the port is free when it settles.
 */
export function createHealthServer(): HealthService {
  const server = http.createServer((req, res) => {
    const path = (req.url ?? "").split("?")[0];
    if (req.method === "GET" && path === HEALTH_PATH) {
      res.writeHead(200, { "Content-Type": "text/plain" });
      res.end(HEALTH_BODY);
      return;
    }
    res.statusCode = 404;
    res.end();
  });

  let started = false;

  return {
    async start(): Promise<void> {
      if (started) {
        throw new Error(`bootstrap: health server already listening on :${HEALTH_PORT}`);
      }
      await new Promise<void>((resolve, reject) => {
        const onError = (err: Error) => reject(err);
        server.once("error", onError);
        server.listen(HEALTH_PORT, () => {
          server.removeListener("error", onError);
          resolve();
        });
      });
      started = true;
      info("health server started", { port: HEALTH_PORT });
    },

    async stop(signal: AbortSignal): Promise<void> {
      if (!started) return;
      const closed = new Promise<void>((resolve, reject) => {
        server.close((err) => {
          if (err && (err as NodeJS.ErrnoException).code !== "ERR_SERVER_NOT_RUNNING") {
            reject(err);
            return;
          }
          resolve();
        });
      });
      // Called after close() per the http API guidance, so in-flight probe
      // connections cannot hold the stop past the budget.
      server.closeAllConnections();
      try {
        await raceAbort(closed, signal, (reason) => new Error(`bootstrap: health server stop timed out: ${String(reason)}`));
      } catch (err) {
        // started stays true so a failed/aborted stop can be retried while
        // the port may still be held.
        error("health server stop failed", { port: HEALTH_PORT, err: err as Error });
        throw err;
      }
      started = false;
      info("health server stopped", { port: HEALTH_PORT });
    },
  };
}
