/**
 * grpc-js Server adapter.
 *
 * Signature and two-phase stop semantics follow
 * `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §6`.
 * No `exited` is provided: grpc-js has no serve-loop exit signal
 * (specs/053-js-bootstrap-migration/research.md D7).
 *
 * Isolation (contracts/bootstrap-js-api.md §7): the module only imports
 * types from @grpc/grpc-js — they are erased at compile time, so the package
 * runtime never loads the library.
 */

import type { Server as GrpcServer, ServerCredentials as GrpcServerCredentials } from "@grpc/grpc-js";
import { error, info } from "@dominion/common-js-logs";
import { Stage, type Component } from "./component.js";

export interface GrpcServerComponentOptions {
  server: GrpcServer;
  address: string;
  credentials: GrpcServerCredentials;
}

/**
 * Server-stage component: start = bindAsync + start (a bind failure, e.g.
 * the address is taken, rejects the start); stop = tryShutdown waits for
 * pending calls, and once the budget signal aborts forceShutdown cancels
 * everything and releases the outstanding tryShutdown callback.
 */
export function createGrpcServerComponent(
  name: string,
  options: GrpcServerComponentOptions,
): Component {
  let started = false;

  return {
    name,
    stage: Stage.Server,

    async start(): Promise<void> {
      if (started) {
        throw new Error(`bootstrap: grpc server "${name}" already started`);
      }
      await new Promise<void>((resolve, reject) => {
        options.server.bindAsync(options.address, options.credentials, (err) => {
          if (err) {
            reject(err);
            return;
          }
          resolve();
        });
      });
      options.server.start();
      started = true;
      info("grpc server started", { component: name, address: options.address });
    },

    async stop(signal: AbortSignal): Promise<void> {
      if (!started) return;
      const onAbort = () => options.server.forceShutdown();
      if (signal.aborted) onAbort();
      else signal.addEventListener("abort", onAbort, { once: true });
      const graceful = new Promise<void>((resolve, reject) => {
        options.server.tryShutdown((err) => {
          signal.removeEventListener("abort", onAbort);
          if (err) {
            reject(err);
            return;
          }
          resolve();
        });
      });
      try {
        await graceful;
      } catch (err) {
        // started stays true so a failed stop can be retried; retrying
        // tryShutdown on an already-shutdown server callbacks immediately.
        error("grpc server stop failed", { component: name, err: err as Error });
        throw err;
      }
      started = false;
      info("grpc server stopped", { component: name, address: options.address });
    },
  };
}
