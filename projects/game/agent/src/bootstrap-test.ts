import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import {
	createOTelReporter,
	info,
	installReporter,
} from "@dominion/common-js-logs";
import { init, shutdown } from "@dominion/common-js-otel";
import { createResolver } from "@dominion/common-js-resolver";
import type { MemorySaver } from "@langchain/langgraph";
import type { AdapterFactory } from "./llm";
import { AgentAdapterImpl } from "./llm";
import type { ChatModel } from "./model-provider";
import type { OperationBridge } from "./operation-bridge";
import { buildResolverAwareChatModel } from "./resolver-provider";

async function main() {
	await init({ instrumentations: [createGrpcInstrumentation()] });

	const uninstallReporter = installReporter(
		createOTelReporter("game-agent/service"),
	);

	info("OTel initialized", { service: "game-agent" });

	const resolver = createResolver();

	const { startServer } = await import("./server.js");

	// spec 012 FR-016/SC-001: the agent_test artifact differs from production
	// ONLY by the resolver-aware provider — it MUST still run the full
	// AgentAdapterImpl.create() pipeline (saolei MCP-client tools +
	// skill auto-injection when mcpNames includes "saolei"; mouse tools +
	// sync constructor otherwise). The earlier version bypassed create()
	// entirely, dropping mcpNames/sessionId and so never binding the saolei
	// MCP tools. Threading all params through create() preserves the real
	// pipeline with only the chat-model source swapped.
	const adapterFactory: AdapterFactory = async (
		_getProvider: () => Promise<ChatModel>,
		systemPrompt: string,
		toolNames: string[],
		bridge: OperationBridge,
		checkpointer: MemorySaver,
		mcpNames: string[],
		sessionId: string,
	) => {
		const chatModel = await buildResolverAwareChatModel(resolver);
		return AgentAdapterImpl.create(
			chatModel,
			systemPrompt,
			toolNames,
			bridge,
			checkpointer,
			mcpNames,
			sessionId,
		);
	};

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
