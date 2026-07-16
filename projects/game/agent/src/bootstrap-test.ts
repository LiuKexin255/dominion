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
import type { SaoleiMcp } from "./mcp/saolei/saolei-mcp";
import { buildResolverAwareChatModel } from "./resolver-provider";

async function main() {
	await init({ instrumentations: [createGrpcInstrumentation()] });

	const uninstallReporter = installReporter(
		createOTelReporter("game-agent/service"),
	);

	info("OTel initialized", { service: "game-agent" });

	const resolver = createResolver();

	const { startServer } = await import("./server.js");

	const adapterFactory: AdapterFactory = async (
		_getProvider: () => Promise<ChatModel>,
		systemPrompt: string,
		toolNames: string[],
		_mcpNames: string[],
		bridge: OperationBridge,
		_saoleiMcp: SaoleiMcp | null,
		checkpointer: MemorySaver,
	) => {
		const chatModel = await buildResolverAwareChatModel(resolver);
		return new AgentAdapterImpl(
			chatModel,
			systemPrompt,
			toolNames,
			_mcpNames,
			bridge,
			_saoleiMcp,
			checkpointer,
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
