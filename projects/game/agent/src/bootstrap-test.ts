/**
 * Test bootstrap entry point for the game agent gRPC server.
 *
 * Passes a FakeLlmAdapter factory to startServer — deterministic,
 * no-network test behavior.  The shared checkpointer is created inside
 * startServer so ListMessages and FakeLlmAdapter use the same instance.
 */

import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { info, installReporter, createOTelReporter } from "@dominion/common-js-logs";
import { MemorySaver } from "@langchain/langgraph";

import type { ChatModel } from "./model-provider";
import type { AdapterFactory } from "./llm";
import { FakeLlmAdapter } from "./fake-llm";

async function main() {
  await init({ instrumentations: [createGrpcInstrumentation()] });

  const uninstallReporter = installReporter(createOTelReporter("game-agent/service"));

  info("OTel initialized", { service: "game-agent" });

  const { startServer } = await import("./server.js");

  const adapterFactory: AdapterFactory = async (
    _getProvider: () => Promise<ChatModel>,
    _systemPrompt: string,
    cp: MemorySaver,
  ) => new FakeLlmAdapter(cp);

  const server = await startServer(adapterFactory);

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
