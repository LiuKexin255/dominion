import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import {
	createOTelReporter,
	error,
	info,
	installReporter,
} from "@dominion/common-js-logs";
import { init, shutdown } from "@dominion/common-js-otel";
import { createResolver } from "@dominion/common-js-resolver";
import type { ChatModel } from "./model-provider.js";
import { buildResolverAwareChatModel } from "./resolver-provider.js";

async function main() {
	await init({ instrumentations: [createGrpcInstrumentation()] });

	const uninstallReporter = installReporter(
		createOTelReporter("game-agent/service"),
	);

	info("OTel initialized", { service: "game-agent" });

	// Defense-in-depth: log (do NOT exit on) unhandled promise rejections.
	// Node.js >=15 defaults to `--unhandled-rejections=throw`, which
	// terminates the process on any unhandled rejection. For a long-running
	// multi-session gRPC server, a single unexpected rejection must not kill
	// all active sessions — e.g. an in-flight team turn that is aborted when
	// a desktop disconnects mid-turn can surface an AbortError as an
	// unhandled rejection if the abort races with the turn's own catch. This
	// handler is the safety net that keeps the test deployment alive in that
	// case, mirroring the production bootstrap (bootstrap.ts) — the two MUST
	// stay aligned so the test SUT exhibits the same crash-resistance as
	// production
	// (specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §2).
	process.on("unhandledRejection", (reason) => {
		error("unhandled promise rejection", { reason: String(reason) });
	});

	const resolver = createResolver();

	const { startServer } = await import("./server.js");

	// The agent_test artifact differs from production ONLY by the
	// resolver-aware provider (spec 012 FR-016/SC-001): every model lookup —
	// the saolei TeamProfile's player AND planner models — resolves to the
	// fake-llm ChatModel, so the full team graph pipeline (player + planner
	// createAgents, memory tools, instruct_player calibration instructions)
	// runs deterministically against the deployed fake-llm with no real LLM
	// involved.
	const server = await startServer({
		getProvider: async (_modelSpec: string): Promise<ChatModel> => {
			return buildResolverAwareChatModel(resolver);
		},
	});

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
