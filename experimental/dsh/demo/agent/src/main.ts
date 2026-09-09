/**
 * Process entry point for the dsh demo chat agent.
 *
 * Split from bootstrap.ts so the lifecycle wiring there stays importable
 * without side effects (lifecycle unit tests import it; this file is the
 * only one that starts the process). Two-phase entry per
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2:
 * OTel + gRPC instrumentation initialize BEFORE @grpc/grpc-js loads inside
 * the server component's dynamic import. The entry sequence (init →
 * installReporter → runAgent → uninstall/shutdown → exit) is fixed by
 * specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §8.
 */

import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { error, info, installReporter, createOTelReporter } from "@dominion/common-js-logs";
import { init, shutdown } from "@dominion/common-js-otel";
import { runAgent } from "./bootstrap.js";

async function main(): Promise<void> {
  // 1. OTel init registers the ESM loader hook and the gRPC instrumentation,
  // so @grpc/grpc-js is patched when it loads inside the server component.
  await init({ instrumentations: [createGrpcInstrumentation()] });

  // 2. Structured logs export through the OTel reporter.
  const uninstallReporter = installReporter(createOTelReporter("dsh-demo/agent"));
  info("otel initialized", { service: "dsh-demo-agent" });

  let exitCode = 0;
  try {
    // 3. Runs until SIGTERM/SIGINT; the k8s health endpoint lifecycle and
    // the component stop order are the orchestrator's. A component start
    // failure (dsh composition boot included) rejects — the fail-loud
    // contract: the process exits non-zero and a half-started agent never
    // serves traffic (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §1).
    await runAgent();
  } catch (err) {
    console.error("Fatal error:", err);
    error("agent bootstrap failed, exiting (fail-loud)", {
      service: "dsh-demo-agent",
      error: err instanceof Error ? err.message : String(err),
    });
    exitCode = 1;
  }

  // 4. OTel lifecycle stays entry-level glue, not a component
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
