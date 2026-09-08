/**
 * Bootstrap entry point for the grpc-chain mid service.
 *
 * Initializes OpenTelemetry BEFORE @grpc/grpc-js loads, installs an OTel
 * log reporter, dynamically imports the server module, serves the k8s health
 * probe endpoint on :38080, and handles graceful shutdown on SIGTERM/SIGINT.
 */

import * as http from "node:http";
import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { info, installReporter, createOTelReporter } from "@dominion/common-js-logs";

// Fixed probe endpoint convention shared with the Go bootstrap: listens on
// all interfaces (kubelet probes the Pod IP, not loopback) and keeps a FIFO
// lifecycle — started after every component, stopped before any of them
// (specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
const HEALTH_PORT = 38080;
const HEALTH_PATH = "/healthz";

// Starts the health HTTP server. listen() reports bind failures
// (e.g. EADDRINUSE) asynchronously via the 'error' event
// (https://nodejs.org/api/http.html#serverlisten), so the promise rejects and
// the failure lands in main().catch — the process exits instead of running
// without a health endpoint (specs/052-deploy-health-probe/spec.md FR-010).
function startHealthServer(): Promise<http.Server> {
  const server = http.createServer((req, res) => {
    if (req.method === "GET" && req.url === HEALTH_PATH) {
      res.writeHead(200, { "Content-Type": "text/plain" });
      res.end("ok\n");
    } else {
      res.writeHead(404);
      res.end();
    }
  });
  return new Promise((resolve, reject) => {
    server.once("error", (err: Error) => {
      reject(new Error(`health server failed to listen on :${HEALTH_PORT}: ${err.message}`));
    });
    server.listen(HEALTH_PORT, () => {
      // An 'error' event with no listener escapes as an uncaught exception;
      // once serving, runtime errors are only logged.
      server.removeAllListeners("error");
      server.on("error", (err: Error) => {
        console.error("[health] server error: %s", err.message);
      });
      resolve(server);
    });
  });
}

// Releases the health port. close() stops accepting new connections and
// closes idle ones (https://nodejs.org/api/http.html#serverclosecallback);
// probe connections are idle, so the callback fires promptly. A server that
// is not listening (test-only early health stop) reports ERR_SERVER_NOT_RUNNING
// through the callback, so it is short-circuited here.
function stopHealthServer(server: http.Server): Promise<void> {
  return new Promise((resolve) => {
    if (!server.listening) {
      resolve();
      return;
    }
    server.close(() => resolve());
  });
}

async function main() {
  console.error("[bootstrap] starting, CHAIN_MODE=%s", process.env.CHAIN_MODE || "standalone");

  // 1. Initialize OTel with gRPC instrumentation BEFORE grpc-js loads
  await init({ instrumentations: [createGrpcInstrumentation()] });

  // 2. Install OTel reporter for structured logs
  const uninstallReporter = installReporter(createOTelReporter("grpc-chain/mid"));
  console.error("[bootstrap] OTel initialized");

  // 3. Log service startup
  info("service starting", { service: "grpc-chain-mid", port: 50051 });

  // 4. Dynamically import server (defers @grpc/grpc-js load after OTel init)
  const { startServer } = await import("./server.js");
  const server = await startServer();

  // Health starts at the tail of the boot chain: 200 then means "all
  // components started and the process is alive"
  // (specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
  const healthServer = await startHealthServer();
  info("health endpoint serving", { port: HEALTH_PORT, path: HEALTH_PATH });

  // 5. Graceful shutdown — health stops first, before any other component
  // (FIFO lifecycle, specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
  const shutdownHandler = async (signal: string) => {
    info("shutting down", { signal });
    await stopHealthServer(healthServer);
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
