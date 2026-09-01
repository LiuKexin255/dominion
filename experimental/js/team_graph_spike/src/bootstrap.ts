/**
 * Bootstrap entry point for the experimental team-graph spike service.
 */

import { init, shutdown } from "@dominion/common-js-otel";
import {
  info,
  installReporter,
  createOTelReporter,
} from "@dominion/common-js-logs";

async function main() {
  console.error("[bootstrap] starting team-graph-spike");

  await init();

  const uninstallReporter = installReporter(
    createOTelReporter("team-graph-spike"),
  );
  console.error("[bootstrap] OTel initialized");

  info("service starting", { service: "team-graph-spike", port: 8080 });

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
