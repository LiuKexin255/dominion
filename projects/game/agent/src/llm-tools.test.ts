/**
 * llm-tools.test.ts — Verifies AgentAdapterImpl passes the correct tools
 * array to createAgent based on profile.toolNames.
 *
 * Reliable pattern (FR-009): injects a `vi.fn()` createAgent spy through the
 * AgentAdapterImpl ctor DI seam instead of a module-level `vi.mock("langchain")`.
 * The module mock is fragile under Bazel js_test — the pre-compiled :lib's
 * import of langchain bypasses vitest's mock registry, so the spy was never
 * installed and createAgentMock was called 0 times (see research.md §2 and
 * style/javascript.md §测试). The injected spy works identically under the
 * vitest CLI and Bazel.
 */

import { MemorySaver } from "@langchain/langgraph";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { fakeModel } from "@langchain/core/testing";

import { AgentAdapterImpl } from "./llm";
import { OperationBridge } from "./operation-bridge";

describe("AgentAdapterImpl createAgent tools wiring", () => {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let createAgentMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		createAgentMock = vi.fn(() => ({}));
	});

	it("passes mouse_move and mouse_click tools when toolNames includes them", () => {
		new AgentAdapterImpl(
			fakeModel(),
			"prompt",
			["mouse_move", "mouse_click"],
			new OperationBridge(),
			new MemorySaver(),
			createAgentMock,
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
			new OperationBridge(),
			new MemorySaver(),
			createAgentMock,
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
			new OperationBridge(),
			new MemorySaver(),
			createAgentMock,
		);

		const opts = createAgentMock.mock.calls[0][0] as {
			tools?: { name: string }[];
		};
		expect(opts.tools).toHaveLength(1);
		expect(opts.tools![0].name).toBe("mouse_move");
	});
});
