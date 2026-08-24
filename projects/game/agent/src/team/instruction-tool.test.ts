/**
 * team/instruction-tool.test.ts — Unit tests for the `instruct_player` tool
 * (T024; `specs/039-planner-memory-calibration/contracts/team-graph-contract.md`
 * §4).
 *
 * The tool stages the instruction content into the configurable-provided
 * external {@link InstructionBuffer} (R1 — the same `configurable` staging
 * pattern as 037 `emitChannelFrame`) and returns `{ok: true}`; it never
 * writes the outer `playerMessages` channel directly — the enclosing node
 * performs the channel write after the agent invoke returns.
 *
 * Mock strategy (`style/javascript.md` §测试): the tool reads the buffer from
 * the injected `RunnableConfig` — no `vi.mock` module interception (see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it } from "vitest";

import { buildInstructPlayerTool } from "./instruction-tool.js";

describe("instruct_player tool (T024, contract §4)", () => {
	it("stages the content into the configurable instructionBuffer and returns {ok:true}", async () => {
		const tool_ = buildInstructPlayerTool();
		const buffer = { content: null };
		const result = await tool_.invoke(
			{ content: "优先清理边角雷区" },
			{ configurable: { thread_id: "t", instructionBuffer: buffer } },
		);
		expect(result).toEqual({ ok: true });
		// The instruction landed in the external buffer (R1 — the node reads
		// it after the agent invoke returns).
		expect(buffer.content).toBe("优先清理边角雷区");
	});

	it("is a no-op (still {ok:true}) when no instructionBuffer is provided", async () => {
		const tool_ = buildInstructPlayerTool();
		const result = await tool_.invoke(
			{ content: "保持节奏" },
			{ configurable: { thread_id: "t" } },
		);
		expect(result).toEqual({ ok: true });
	});

	it("is a no-op when invoked without a config at all", async () => {
		const tool_ = buildInstructPlayerTool();
		const result = await tool_.invoke({ content: "保持节奏" });
		expect(result).toEqual({ ok: true });
	});

	it("exposes the fixed name/schema contract (content: string)", () => {
		const tool_ = buildInstructPlayerTool();
		expect(tool_.name).toBe("instruct_player");
		const schema = (tool_.schema as { shape?: Record<string, unknown> }).shape;
		expect(schema).toBeDefined();
		expect(schema?.content).toBeDefined();
	});
});
