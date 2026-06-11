/**
 * Test bootstrap entry point for the game agent gRPC server.
 *
 * Identical to bootstrap.ts except it creates a FakeLlmAdapter (deterministic,
 * no network) instead of RealLLMAdapter. All handler, runtime, server, and
 * proto handling code is shared with the production artifact -- only the LLM
 * module differs.
 */

import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { info, installReporter, createOTelReporter } from "@dominion/common-js-logs";
import { FakeLlmAdapter } from "./fake-llm";

async function main() {
  await init({ instrumentations: [createGrpcInstrumentation()] });

  const uninstallReporter = installReporter(createOTelReporter("game-agent/service"));

  info("OTel initialized", { service: "game-agent" });

  const { startServer } = await import("./server.js");
  const llmAdapter = new FakeLlmAdapter();
  const server = await startServer(llmAdapter);

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
