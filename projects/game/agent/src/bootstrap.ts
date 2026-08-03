/**
 * Bootstrap entry point for the game agent gRPC server.
 *
 * Initializes OpenTelemetry BEFORE @grpc/grpc-js loads, installs an OTel
 * log reporter, dynamically imports the server module, and handles graceful
 * shutdown on SIGTERM/SIGINT.
 *
 * This module is the runtime entrypoint for the container image.
 */

import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { info, error, installReporter, createOTelReporter } from "@dominion/common-js-logs";

async function main() {
  // 1. Initialize OTel with gRPC instrumentation BEFORE grpc-js loads
  await init({ instrumentations: [createGrpcInstrumentation()] });

  // 2. Install OTel reporter for structured logs
  const uninstallReporter = installReporter(createOTelReporter("game-agent/service"));

  // 3. Log service startup
  info("OTel initialized", { service: "game-agent" });

  // 4. Defense-in-depth: log (do NOT exit on) unhandled promise rejections.
  // Node.js >=15 defaults to `--unhandled-rejections=throw`, which terminates
  // the process on any unhandled rejection. For a long-running multi-session
  // gRPC server, a single unexpected rejection must not kill all active
  // sessions. The primary fix (safeWrite in handler.ts) closes the known
  // crash vector; this handler is the safety net for any future regression
  // of the same category.
  // Contract: specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §2
  // Behavior: specs/026-agent-abort-crash-fix/data-model.md §2
  // Rationale: specs/026-agent-abort-crash-fix/research.md §E D4
  process.on("unhandledRejection", (reason) => {
    error("unhandled promise rejection", { reason: String(reason) });
  });

  // 5. Dynamically import server (defers @grpc/grpc-js load after OTel init)
  const { startServer } = await import("./server.js");
  const server = await startServer();

  info("gRPC server listening on 0.0.0.0:50051", { service: "game-agent" });

  // 6. Graceful shutdown
  const shutdownHandler = async (signal: string) => {
    info("shutting down", { signal });
    uninstallReporter();
    await shutdown();
    server.forceShutdown();
    process.exit(0);
  };

  process.on("SIGTERM", () => shutdownHandler("SIGTERM"));
  process.on("SIGINT", () => shutdownHandler("SIGINT"));
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
