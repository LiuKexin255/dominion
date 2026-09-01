/**
 * Bootstrap entry point for the experimental team-graph spike service.
 *
 * Two-phase entry per
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2
 * (no instrumentation passed: the service has no @grpc/grpc-js dependency,
 * init() still registers the OTel ESM loader hook idempotently). The entry
 * sequence (init → installReporter → dynamic import → register → run →
 * uninstall/shutdown → exit) is fixed by
 * specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §8; the
 * 38080/healthz endpoint is provided automatically by the shared bootstrap
 * (specs/053-js-bootstrap-migration/spec.md FR-002).
 */

import {
  Bootstrap,
  createHttpServerComponent,
} from "@dominion/common-js-bootstrap";
import {
  createOTelReporter,
  info,
  installReporter,
} from "@dominion/common-js-logs";
import { init, shutdown } from "@dominion/common-js-otel";

// Business port declared in experimental/js/team_graph_spike/service.yaml,
// overridable through the PORT env; /health and /invoke behavior is
// unchanged by the bootstrap adoption
// (specs/053-js-bootstrap-migration/spec.md FR-013).
const port = process.env.PORT || "8080";

async function main(): Promise<void> {
  await init();

  const uninstallReporter = installReporter(
    createOTelReporter("team-graph-spike"),
  );
  info("service starting", { service: "team-graph-spike", port: Number(port) });

  let exitCode = 0;
  try {
    // Dynamic import keeps the langchain dependency graph out of the
    // bootstrap static import graph.
    const { buildServer } = await import("./server.js");

    const bootstrap = new Bootstrap();
    bootstrap.register(
      createHttpServerComponent("http", buildServer(), { port: Number(port) }),
    );

    // Runs until SIGTERM/SIGINT; resolves only on a clean signal exit, with
    // the health endpoint stopped first (FIFO lifecycle).
    await bootstrap.run();
  } catch (err) {
    console.error("Fatal error:", err);
    exitCode = 1;
  }

  // OTel lifecycle stays entry-level glue, not a component
  // (specs/053-js-bootstrap-migration/research.md D4): shutdown runs after
  // every component has stopped so shutdown-period logs still export.
  uninstallReporter();
  await shutdown();
  process.exit(exitCode);
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
