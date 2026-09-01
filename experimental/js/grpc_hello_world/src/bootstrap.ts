/**
 * Bootstrap entry point for the gRPC hello world service.
 *
 * Two-phase entry per
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2:
 * the static import graph carries only OTel/bootstrap wiring; @grpc/grpc-js
 * loads through the dynamic import below, after init() has registered the
 * OTel ESM loader hook. The entry sequence (init → installReporter →
 * dynamic import → register → run → uninstall/shutdown → exit) is fixed by
 * specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §8.
 */

import {
  Bootstrap,
  createGrpcServerComponent,
} from "@dominion/common-js-bootstrap";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import {
  createOTelReporter,
  info,
  installReporter,
} from "@dominion/common-js-logs";
import { init, shutdown } from "@dominion/common-js-otel";

// Binds on all interfaces, matching the deployed container port declared in
// experimental/js/grpc_hello_world/service.yaml.
const GRPC_ADDRESS = "0.0.0.0:50051";

// Test-only self-heal hook for the large test
// (specs/052-deploy-health-probe/contracts/verification-testplan.md §2): when
// HEALTH_STOP_AFTER_MS is set, the health endpoint stops responding after the
// given delay to simulate a hung process. The process itself stays alive (the
// gRPC server keeps the event loop busy), so the k8s liveness probe — not any
// exit path — is what fails and restarts the container. The hook rides the
// bootstrap's health handle
// (specs/053-js-bootstrap-migration/research.md D6) and exists only in this
// experimental service, never in shared packages
// (specs/053-js-bootstrap-migration/spec.md FR-014).
// Timing constraint: the hook is armed before run() (the D6-prescribed form,
// a plain setTimeout on the entry), so HEALTH_STOP_AFTER_MS must be larger
// than the service startup time — before the health handle exists the
// `bootstrap.health?.stop()` below is a silent no-op. The self-heal test
// injects 60000ms
// (specs/052-deploy-health-probe/contracts/verification-testplan.md §2), far
// above startup time, so there is no practical race.
function armHealthStopHook(bootstrap: Bootstrap): void {
  const raw = process.env.HEALTH_STOP_AFTER_MS;
  // Empty string counts as unset: Number("") is 0, which would stop the
  // endpoint immediately instead of leaving it disabled.
  if (raw === undefined || raw === "") {
    return;
  }
  const delayMs = Number(raw);
  if (Number.isNaN(delayMs)) {
    console.error("[health] invalid HEALTH_STOP_AFTER_MS=%s, stop timer not armed", raw);
    return;
  }
  setTimeout(() => {
    console.error("[health] HEALTH_STOP_AFTER_MS reached, stopping health endpoint (process stays alive)");
    void bootstrap.health?.stop();
  }, delayMs);
}

async function main(): Promise<void> {
  // 1. OTel init registers the ESM loader hook and registers the gRPC
  // instrumentation, so @grpc/grpc-js is patched when it loads in step 3.
  await init({ instrumentations: [createGrpcInstrumentation()] });

  // 2. Structured logs export through the OTel reporter.
  const uninstallReporter = installReporter(
    createOTelReporter("grpc-hello-world-js/service"),
  );
  info("service starting", { service: "grpc-hello-world-js", address: GRPC_ADDRESS });

  let exitCode = 0;
  try {
    // 3. Dynamic import keeps @grpc/grpc-js out of the static import graph.
    const { buildServer } = await import("./server.js");
    const { server, credentials } = buildServer();

    const bootstrap = new Bootstrap();
    bootstrap.register(
      createGrpcServerComponent("grpc", { server, address: GRPC_ADDRESS, credentials }),
    );
    armHealthStopHook(bootstrap);

    // 4. Runs until SIGTERM/SIGINT; resolves only on a clean signal exit,
    // with the health endpoint stopped first (FIFO lifecycle).
    await bootstrap.run();
  } catch (err) {
    console.error("Fatal error:", err);
    exitCode = 1;
  }

  // 5. OTel lifecycle stays entry-level glue, not a component
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
