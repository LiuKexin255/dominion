/**
 * llm-tools.test.ts — Verifies AgentAdapterImpl passes the correct tools
 * array to createAgent based on profile.toolNames.
 *
 * Uses vi.hoisted + vi.mock to spy on createAgent calls while preserving
 * the real implementation (pass-through).
 */

import { MemorySaver } from "@langchain/langgraph";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { fakeModel } from "@langchain/core/testing";

import { AgentAdapterImpl } from "./llm";
import { OperationBridge } from "./operation-bridge";

const { createAgentMock } = vi.hoisted(() => ({
	createAgentMock: vi.fn(),
}));

vi.mock("langchain", async (importOriginal) => {
	const actual = await importOriginal<typeof import("langchain")>();
	return {
		createMiddleware: actual.createMiddleware,
		// `tool` must be passed through so AgentAdapterImpl's buildTools can
		// construct mouse/saolei tools under this mock. Without it, vi.mock
		// returns an object with no `tool` export and every tool factory call
		// throws, which made createAgent unreachable (called 0 times) — masking
		// the real wiring under a confusing assertion failure.
		tool: actual.tool,
		createAgent: (...args: unknown[]) => {
			createAgentMock(...args);
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			return (actual.createAgent as any)(...args);
		},
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
	} as any;
});

beforeEach(() => {
	createAgentMock.mockClear();
});

describe("AgentAdapterImpl createAgent tools wiring", () => {
	it("passes mouse_move and mouse_click tools when toolNames includes them", () => {
		new AgentAdapterImpl(
			fakeModel(),
			"prompt",
			["mouse_move", "mouse_click"],
			[],
			new OperationBridge(),
			null,
			new MemorySaver(),
		);

		expect(createAgentMock).toHaveBeenCalledTimes(1);
		const opts = createAgentMock.mock.calls[0][0] as {
			tools?: { name: string }[];
		};
		expect(Array.isArray(opts.tools)).toBe(true);
		expect(opts.tools).toHaveLength(2);
		expect(opts.tools![0].name).toBe("mouse_move");
		expect(opts.tools![1].name).toBe("mouse_click");
	});

	it("passes an empty tools array when toolNames=[]", () => {
		new AgentAdapterImpl(
			fakeModel(),
			"prompt",
			[],
			[],
			new OperationBridge(),
			null,
			new MemorySaver(),
		);

		expect(createAgentMock).toHaveBeenCalledTimes(1);
		const opts = createAgentMock.mock.calls[0][0] as {
			tools?: unknown[];
		};
		expect(opts.tools).toEqual([]);
	});

	it("does NOT include unknown tool names in the tools array", () => {
		new AgentAdapterImpl(
			fakeModel(),
			"prompt",
			["mouse_move", "nonexistent"],
			[],
			new OperationBridge(),
			null,
			new MemorySaver(),
		);

		const opts = createAgentMock.mock.calls[0][0] as {
			tools?: { name: string }[];
		};
		expect(opts.tools).toHaveLength(1);
		expect(opts.tools![0].name).toBe("mouse_move");
	});
});
