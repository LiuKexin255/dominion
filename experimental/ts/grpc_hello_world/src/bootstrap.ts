/**
 * Bootstrap entry point for the TypeScript gRPC hello world server.
 *
 * Initializes OpenTelemetry BEFORE @grpc/grpc-js loads, installs an OTel
 * log reporter, dynamically imports the server module, and handles graceful
 * shutdown on SIGTERM/SIGINT.
 *
 * This module is the runtime entrypoint for the container image.
 */

import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { info, installReporter, createOTelReporter } from "@dominion/common-js-logs";

async function main() {
  // 1. Initialize OTel with gRPC instrumentation BEFORE grpc-js loads
  await init({ instrumentations: [createGrpcInstrumentation()] });

  // 2. Install OTel reporter for structured logs
  const uninstallReporter = installReporter(createOTelReporter("grpc-hello-world-ts/service"));

  // 3. Log service startup
  info("service starting", { service: "grpc-hello-world-ts", port: 50051 });

  // 4. Dynamically import server (defers @grpc/grpc-js load after OTel init)
  const { startServer } = await import("./server.js");
  const server = await startServer();

  // 5. Graceful shutdown
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
