import { info } from "@dominion/common-js-logs";
import type { EndpointResolver } from "@dominion/common-js-resolver";
import { ChatOpenAI } from "@langchain/openai";

const FAKE_LLM_TARGET = "dominion:///game/fake-llm:8080";

export async function buildResolverAwareChatModel(
	resolver: EndpointResolver,
): Promise<ChatOpenAI> {
	const endpoints = await resolver.resolve(FAKE_LLM_TARGET);
	if (endpoints.length === 0) {
		throw new Error(`resolver returned no endpoints for ${FAKE_LLM_TARGET}`);
	}
	const endpoint = endpoints[0]; // "host:port" e.g. "10.0.0.9:8080"
	const baseURL = `http://${endpoint}/v1`;
	info("resolved fake-llm endpoint", {
		target: FAKE_LLM_TARGET,
		endpoint,
		baseURL,
	});
	return new ChatOpenAI({
		model: "gpt-4", // model name is ignored by fake-llm
		apiKey: "sk-test",
		configuration: { baseURL },
	});
}
