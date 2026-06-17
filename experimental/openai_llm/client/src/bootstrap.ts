/**
 * Bootstrap entry point for the experimental OpenAI LLM client service.
 */

import { init, shutdown } from "@dominion/common-js-otel";
import { info, installReporter, createOTelReporter } from "@dominion/common-js-logs";

async function main() {
  console.error("[bootstrap] starting openai-llm-client");

  await init();

  const uninstallReporter = installReporter(createOTelReporter("openai-llm-client"));
  console.error("[bootstrap] OTel initialized");

  info("service starting", { service: "openai-llm-client", port: 8080 });

  const { startServer } = await import("./server.js");
  const server = await startServer();

  const shutdownHandler = async (signal: string) => {
    info("shutting down", { signal });
    uninstallReporter();
    server.close();
    await shutdown();
    process.exit(0);
  };

  process.on("SIGTERM", () => shutdownHandler("SIGTERM"));
  process.on("SIGINT", () => shutdownHandler("SIGINT"));
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
