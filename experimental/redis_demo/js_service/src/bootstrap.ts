/**
 * Bootstrap entry point for the redis-demo js service.
 *
 * Two-phase entry per
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2:
 * the static import graph carries only OTel wiring; @grpc/grpc-js loads through
 * the dynamic import below, after init() has registered the OTel ESM loader
 * hook. The k8s health probe endpoint is served here on the fixed convention
 * port with a FIFO lifecycle
 * (tools/release/deploy/README.md §健康探针约定;
 * specs/052-deploy-health-probe/contracts/bootstrap-health.md §1) — the same
 * self-contained form as experimental/grpc_chain/mid.
 */
import * as http from "node:http";
import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { createOTelReporter, info, installReporter } from "@dominion/common-js-logs";

const HEALTH_PORT = 38080;
const HEALTH_PATH = "/healthz";

// Starts the health HTTP server. listen() reports bind failures (e.g.
// EADDRINUSE) asynchronously via the 'error' event
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

// Releases the health port. Probe connections are idle, so close() settles
// promptly; a server that never started reports ERR_SERVER_NOT_RUNNING through
// the callback and is short-circuited here.
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
	// 1. OTel init registers the ESM loader hook and the gRPC instrumentation,
	// so @grpc/grpc-js is patched when the server module loads in step 3.
	await init({ instrumentations: [createGrpcInstrumentation()] });

	// 2. Structured logs export through the OTel reporter.
	const uninstallReporter = installReporter(createOTelReporter("redis-demo/js-service"));
	info("service starting", { service: "redis-demo-js-service", port: 50051 });

	// 3. Dynamic import keeps @grpc/grpc-js out of the static import graph.
	const { startServer } = await import("./server.js");
	const service = await startServer();

	// Health starts at the tail of the boot chain: 200 then means "all
	// components started and the process is alive".
	const healthServer = await startHealthServer();
	info("health endpoint serving", { port: HEALTH_PORT, path: HEALTH_PATH });

	// 4. Graceful shutdown — health stops first, before any other component;
	// the service handle then releases redis and the gRPC server.
	const shutdownHandler = async (signal: string) => {
		info("shutting down", { signal });
		await stopHealthServer(healthServer);
		await service.close();
		uninstallReporter();
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
