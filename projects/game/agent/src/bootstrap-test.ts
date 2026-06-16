/**
 * Test bootstrap entry point for the game agent gRPC server.
 *
 * Identical to bootstrap.ts. Uses AdapterManager (which creates
 * AgentAdapterImpl instances on demand) — deterministic, no-network test
 * behavior is configured via adapterManagerOverride in startServer().
 */

import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { info, installReporter, createOTelReporter } from "@dominion/common-js-logs";

async function main() {
  await init({ instrumentations: [createGrpcInstrumentation()] });

  const uninstallReporter = installReporter(createOTelReporter("game-agent/service"));

  info("OTel initialized", { service: "game-agent" });

  const { startServer } = await import("./server.js");
  const server = await startServer();

  info("gRPC server listening on 0.0.0.0:50051", { service: "game-agent" });

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
