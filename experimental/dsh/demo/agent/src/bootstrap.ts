/**
 * Bootstrap entry point for the dsh demo chat agent.
 *
 * Order matters (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §1):
 * OTel + gRPC instrumentation initializes BEFORE @grpc/grpc-js loads; the
 * dsh composition boots fail-loud (any failure exits non-zero,
 * specs/047-dsh-chat-demo/spec.md FR-009); only then does the gRPC server
 * module load and start serving. SIGTERM/SIGINT triggers the graceful chain:
 * stop the server, dispose every agent session, dispose the composition's
 * root fiber, flush OTel, exit 0.
 */

import type { Server } from "@grpc/grpc-js";
import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { error, info, installReporter, createOTelReporter } from "@dominion/common-js-logs";
import { bootDsh } from "./dsh.js";

/**
 * Graceful server stop: tryShutdown waits for in-flight RPCs to finish; the
 * bounded-time fallback to forceShutdown keeps a stuck call from blocking the
 * agent/fiber teardown behind it. `Server` is a type-only import so this
 * module still loads no @grpc/grpc-js runtime code before OTel init.
 */
function gracefulStop(server: Server, timeoutMs = 10_000): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      server.forceShutdown();
      resolve();
    }, timeoutMs);
    server.tryShutdown(() => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function main(): Promise<void> {
  await init({ instrumentations: [createGrpcInstrumentation()] });
  const uninstallReporter = installReporter(createOTelReporter("dsh-demo/agent"));
  info("otel initialized", { service: "dsh-demo-agent" });

  // Fail-loud composition boot: resolves only on a fully settled plugin tree.
  const ctx = await bootDsh();

  // Dynamic import defers @grpc/grpc-js loading until after OTel init.
  const { startServer } = await import("./server.js");
  const started = await startServer({ ctx });
  info("service started", { service: "dsh-demo-agent", port: 50051 });

  let exiting = false;
  const shutdownHandler = async (signal: string): Promise<void> => {
    if (exiting) return;
    exiting = true;
    info("shutting down", { signal, service: "dsh-demo-agent" });
    try {
      await gracefulStop(started.server);
      await started.sessions.shutdown();
    } catch (err) {
      error("shutdown cleanup failed", {
        error: err instanceof Error ? err.message : String(err),
      });
    }
    uninstallReporter();
    await shutdown();
    process.exit(0);
  };

  process.on("SIGTERM", () => void shutdownHandler("SIGTERM"));
  process.on("SIGINT", () => void shutdownHandler("SIGINT"));
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
