import type { EndpointResolver } from "@dominion/common-js-resolver";
import { ChatOpenAI } from "@langchain/openai";
import { describe, expect, it } from "vitest";
import { buildResolverAwareChatModel } from "./resolver-provider.js";

describe("buildResolverAwareChatModel", () => {
	it("builds ChatOpenAI with resolved endpoint", async () => {
		const resolver: EndpointResolver = {
			resolve: async () => ["10.0.0.9:8080"],
		};
		const model = await buildResolverAwareChatModel(resolver);
		expect(model).toBeInstanceOf(ChatOpenAI);
		// ChatOpenAI stores the resolved client options (incl. baseURL) on
		// `clientConfig` (the OpenAI ClientOptions), not on the `configuration`
		// input field — see @langchain/openai BaseChatOpenAI.clientConfig.
		const clientConfig = (model as { clientConfig?: { baseURL?: string } })
			.clientConfig;
		expect(clientConfig?.baseURL).toBe("http://10.0.0.9:8080/v1");
	});

	it("throws when resolver returns empty array", async () => {
		const resolver: EndpointResolver = {
			resolve: async () => [],
		};
		await expect(buildResolverAwareChatModel(resolver)).rejects.toThrow(
			"no endpoints",
		);
	});
});
