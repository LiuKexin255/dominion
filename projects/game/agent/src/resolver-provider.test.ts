import type { EndpointResolver } from "@dominion/common-js-resolver";
import { ChatOpenAI } from "@langchain/openai";
import { describe, expect, it } from "vitest";
import { buildResolverAwareChatModel } from "./resolver-provider";

describe("buildResolverAwareChatModel", () => {
	it("builds ChatOpenAI with resolved endpoint", async () => {
		const resolver: EndpointResolver = {
			resolve: async () => ["10.0.0.9:8080"],
		};
		const model = await buildResolverAwareChatModel(resolver);
		expect(model).toBeInstanceOf(ChatOpenAI);
		const config =
			(model as { configuration?: { baseURL?: string } }).configuration ??
			(model as { config?: { baseURL?: string } }).config;
		expect(config?.baseURL).toBe("http://10.0.0.9:8080/v1");
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
