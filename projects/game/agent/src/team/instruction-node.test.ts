/**
 * team/instruction-node.test.ts — Unit tests for the init/compact instruction
 * node factory (T025; `specs/039-planner-memory-calibration/contracts/
 * team-graph-contract.md` §2.3 — FR-015/FR-016/FR-019).
 *
 * The node runs a REAL createAgent loop driven by `fakeModel` (same pattern
 * as team/graph.test.ts): the fake model decides whether to call the real
 * `instruct_player` tool, which stages the instruction into the
 * configurable-provided external buffer (R1); the node reads the staged
 * content AFTER the invoke returns and writes `playerMessages` (a
 * HumanMessage — the same channel write-back as the review node; 不触发
 * player invoke, FR-015/FR-016).
 *
 * Mock strategy (`style/javascript.md` §测试): the model/tools/buffer are
 * injected via DI (no `vi.mock` — see
 * [vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 */

import { describe, expect, it, vi } from "vitest";
import {
	AIMessage,
	HumanMessage,
	SystemMessage,
} from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";

import type { MessagePart } from "../../game_types/projects/game/MessagePart";
import { extractToolCalls } from "../llm";
import type { ChatModel } from "../model-provider";
import {
	FrozenMemorySnapshot,
	PLANNER_MEMORY_SNAPSHOT_ID,
} from "./memory-snapshot";
import { DEFAULT_PLANNER_BASE } from "./planner";
import {
	createInstructionNode,
	type InstructionScenario,
} from "./instruction-node";
import type { InstructionBuffer } from "./instruction-tool";
import type { TeamStateValue } from "./state";

/** The emitter test-double's call signature (string text or MessagePart[]). */
type EmitCall = (
	agent: string,
	content: string | MessagePart[],
	frameId?: string,
	role?: string,
) => void;

/** A minimal empty TeamState for a fresh-session instruction node run. */
function freshState(): TeamStateValue {
	return {
		playerMessages: [],
		plannerMessages: [],
		gameEnded: null,
		gameCounter: 0,
	};
}

/** The fake model that decides to send one instruction via instruct_player. */
function instructingModel(content: string) {
	return fakeModel()
		.respondWithTools([{ name: "instruct_player", args: { content } }])
		.respond(new AIMessage("instruction sent"));
}

/** The fake model that produces NO tool call (LLM decides not to instruct). */
function silentModel() {
	return fakeModel().respond(new AIMessage("no instruction needed"));
}

/** A fake model that always throws (planner model unavailable). */
function failingModel() {
	return fakeModel()
		.respond(new Error("llm down"))
		.respond(new Error("llm down"))
		.respond(new Error("llm down"));
}

function buildNode(
	model: ChatModel,
	scenario: InstructionScenario,
	overrides: { frozenSnapshot?: FrozenMemorySnapshot } = {},
) {
	const node = createInstructionNode(
		{
			model,
			frozenSnapshot:
				overrides.frozenSnapshot ?? new FrozenMemorySnapshot(),
			plannerBasePrompt: "",
		},
		scenario,
	);
	return { node, buffer: { content: null } as InstructionBuffer };
}

/** The instruction request text the node injected for the given scenario. */
function requestContent(
	firstCallMessages: BaseMessage[],
	scenario: InstructionScenario,
): string {
	const needle = scenario === "init" ? "团队初始化" : "上下文刚被压缩";
	const request = firstCallMessages.find(
		(m) => m._getType() === "human" && contentType(m).includes(needle),
	);
	if (!request) throw new Error("instruction request not found in input");
	return contentType(request);
}

function contentType(m: BaseMessage): string {
	const c = m.content;
	return typeof c === "string" ? c : JSON.stringify(c);
}

describe("instruction node — init scenario (T025, contract §2.3)", () => {
	it("writes the staged instruction into playerMessages when the LLM calls instruct_player (R4 — no enforcement of the call itself)", async () => {
		const model = instructingModel("开局先点中心，再清理边角");
		const { node, buffer } = buildNode(model, "init");
		const config = {
			configurable: { thread_id: "t-init", instructionBuffer: buffer },
		};

		const result = await node(freshState(), config);

		// The LLM DID call the tool → the instruction is staged in the
		// external buffer and the node writes it into `playerMessages` as a
		// HumanMessage (same channel write-back as the review node —
		// planner.ts; NO pending slot, NO player invoke, FR-015).
		expect(result.playerMessages).toBeDefined();
		const instructionMsg = (result.playerMessages as BaseMessage[])[0];
		expect(instructionMsg).toBeInstanceOf(HumanMessage);
		expect(contentType(instructionMsg)).toBe("开局先点中心，再清理边角");
		// The buffer slot was reset after the read (R1).
		expect(buffer.content).toBeNull();
		// The planner's channel write-back excludes the frozen snapshot
		// SystemMessage (contract §3 — filtered like the review node).
		expect(result.plannerMessages).toBeDefined();
		for (const m of result.plannerMessages as BaseMessage[]) {
			expect(m.id).not.toBe(PLANNER_MEMORY_SNAPSHOT_ID);
		}
		// The model input = snapshot + (empty) planner history + the REQUIRING
		// init request (无 gameLog — prompt 要求给指令, R4).
		const firstCall = model.calls[0]?.messages as BaseMessage[];
		expect(
			firstCall.some((m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID),
		).toBe(true);
		const request = requestContent(firstCall, "init");
		expect(request).toContain("请调用 instruct_player 发送这条指令");
		expect(request).not.toContain("本局游戏过程");
		// The scenario special cases are stated in the request (the shared
		// base is NOT modified for init/compact — 无游戏历史/请勿复盘/请勿
		// 更新记忆): no game history → no review, and the frozen snapshot
		// stays unchanged (no memory update).
		expect(request).toContain("请勿复盘游戏");
		expect(request).toContain("请勿更新长期记忆");
		// The fake model was actually exercised (style/javascript.md §测试).
		expect(model.calls.length).toBeGreaterThan(0);
	});

	it("writes NO playerMessages when the LLM decides not to call the tool (R4 — no enforcement)", async () => {
		const model = silentModel();
		const { node, buffer } = buildNode(model, "init");
		const config = {
			configurable: { thread_id: "t-init-silent", instructionBuffer: buffer },
		};

		const result = await node(freshState(), config);

		expect(result.playerMessages).toBeUndefined();
		expect(result).not.toHaveProperty("playerMessages");
		expect(buffer.content).toBeNull();
	});

	it("degrades on persistent invoke failure: skips the instruction and does not throw (contract §6)", async () => {
		const model = failingModel();
		const { node } = buildNode(model, "init");
		const config = {
			configurable: {
				thread_id: "t-init-fail",
				instructionBuffer: { content: null } as InstructionBuffer,
			},
		};

		const result = await node(freshState(), config);

		// contract §6: planner model unavailable → skip the init instruction,
		// log, do NOT block the team / UpdateTeam materialization.
		expect(result).toEqual({});
		// The retry wrapper really retried (3 attempts) before degrading.
		expect(model.calls.length).toBe(3);
	});

	it("emits the three init frames — request / tool-call response / write-back — each with frameId == message id (041 T006, contract §2.2 / data-model §3, FR-004)", async () => {
		const model = instructingModel("开局先点中心，再清理边角");
		const { node, buffer } = buildNode(model, "init");
		const emitChannelFrame = vi.fn<EmitCall>();

		const result = await node(freshState(), {
			configurable: {
				thread_id: "t-init-frame",
				instructionBuffer: buffer,
				emitChannelFrame,
			},
		});

		// Exactly three frames in production order (request → response →
		// write-back, specs/041-realtime-init-push/data-model.md §3.3), each
		// carrying the producing agent,
		// the message-typed role and frameId == the persisted message id
		// (dedup anchor, specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §4).
		expect(emitChannelFrame).toHaveBeenCalledTimes(3);

		// (a) Planner request frame: agent=planner, role=USER, the scenario
		// prompt text, frameId = the request HumanMessage's id (which is
		// persisted into plannerMessages — specs/041-realtime-init-push/
		// research.md D3 note).
		const [agent1, content1, frameId1, role1] = emitChannelFrame.mock.calls[0];
		expect(agent1).toBe("planner");
		expect(content1).toContain("团队初始化");
		const persistedRequest = (result.plannerMessages as BaseMessage[]).find(
			(m) => m._getType() === "human" && contentType(m).includes("团队初始化"),
		);
		expect(frameId1).toBe(persistedRequest?.id);
		expect(role1).toBe("MESSAGE_ROLE_USER");

		// (b) Planner response frame: agent=planner, role=AGENT, the
		// instruct_player tool call as a toolCall MessagePart (faithful
		// mirroring — specs/041-realtime-init-push/research.md D3, same
		// conversion as ListMessages,
		// handler.ts:700-711), frameId = the response AIMessage's id.
		const [agent2, content2, frameId2, role2] = emitChannelFrame.mock.calls[1];
		expect(agent2).toBe("planner");
		expect(Array.isArray(content2)).toBe(true);
		const parts2 = content2 as MessagePart[];
		expect(parts2.length).toBe(1);
		expect(parts2[0].toolCall?.name).toBe("instruct_player");
		expect(JSON.parse(parts2[0].toolCall?.argsJson ?? "{}")).toEqual({
			content: "开局先点中心，再清理边角",
		});
		const persistedResponse = (result.plannerMessages as BaseMessage[]).find(
			(m) => m._getType() === "ai" && extractToolCalls(m).length > 0,
		);
		expect(frameId2).toBe(persistedResponse?.id);
		expect(role2).toBe("MESSAGE_ROLE_AGENT");

		// (c) Player write-back frame: agent=player, role=USER, the
		// instruction text, frameId = the write-back HumanMessage's id.
		const [agent3, content3, frameId3, role3] = emitChannelFrame.mock.calls[2];
		expect(agent3).toBe("player");
		expect(content3).toBe("开局先点中心，再清理边角");
		const writeBack = (result.playerMessages as BaseMessage[])[0];
		expect(writeBack).toBeInstanceOf(HumanMessage);
		expect(frameId3).toBe(writeBack.id);
		expect(role3).toBe("MESSAGE_ROLE_USER");
	});

	it("emits NO frame when the planner invoke fails (041 T006 — degrade, contract §2.3 / research.md D9)", async () => {
		const model = failingModel();
		const { node } = buildNode(model, "init");
		const emitChannelFrame = vi.fn<EmitCall>();

		const result = await node(freshState(), {
			configurable: {
				thread_id: "t-init-fail-frame",
				instructionBuffer: { content: null } as InstructionBuffer,
				emitChannelFrame,
			},
		});

		// specs/039-planner-memory-calibration/contracts/team-graph-contract.md
		// §6: planner model unavailable → skip the instruction,
		// degrade. The frame emission happens ONLY after the invoke resolves
		// (041 T006), so a failed planner leaves the channel silent — no
		// orphan frame whose frameId matches no persisted message
		// (specs/041-realtime-init-push/contracts/realtime-channel-contract.md
		// §2.3).
		expect(result).toEqual({});
		expect(emitChannelFrame).not.toHaveBeenCalled();
		// The retry wrapper really retried (3 attempts) before degrading.
		expect(model.calls.length).toBe(3);
	});
});

describe("instruction node — compact scenario (T025, contract §2.3)", () => {
	it("writes the staged instruction into playerMessages with the COMPACT prompt (压缩后重建引导, FR-016)", async () => {
		const model = instructingModel("重新建立引导：保持节奏");
		const { node, buffer } = buildNode(model, "compact");
		// A compressed planner channel (one summary AIMessage) — the node
		// must carry it into the model input (post-compact context).
		const state = freshState();
		state.plannerMessages = [new AIMessage("planner 摘要内容")];
		const config = {
			configurable: { thread_id: "t-compact", instructionBuffer: buffer },
		};

		const result = await node(state, config);

		const instructionMsg = (result.playerMessages as BaseMessage[])[0];
		expect(instructionMsg).toBeInstanceOf(HumanMessage);
		expect(contentType(instructionMsg)).toBe("重新建立引导：保持节奏");
		const firstCall = model.calls[0]?.messages as BaseMessage[];
		// The compressed planner summary is part of the input.
		expect(
			firstCall.some((m) => contentType(m).includes("planner 摘要内容")),
		).toBe(true);
		const request = requestContent(firstCall, "compact");
		expect(request).toContain("上下文刚被压缩");
		expect(request).toContain("请调用 instruct_player 发送这条指令");
		expect(request).not.toContain("本局游戏过程");
		// The compact scenario states the same special cases as init (no
		// review, no memory update — the shared base is untouched).
		expect(request).toContain("请勿复盘游戏");
		expect(request).toContain("请勿更新长期记忆");
	});

	it("keeps the frozen snapshot SystemMessage out of the channel write-back (contract §3)", async () => {
		const snapshot = new FrozenMemorySnapshot();
		const model = instructingModel("指令");
		const { node, buffer } = buildNode(model, "compact", { frozenSnapshot: snapshot });
		const config = {
			configurable: { thread_id: "t-compact-filter", instructionBuffer: buffer },
		};

		const result = await node(freshState(), config);

		for (const m of result.plannerMessages as BaseMessage[]) {
			expect(m.id).not.toBe(PLANNER_MEMORY_SNAPSHOT_ID);
		}
		// The snapshot SystemMessage IS in the model input (纯内容呈现 —
		// SystemMessage with the fixed id, FR-011).
		const firstCall = model.calls[0]?.messages as BaseMessage[];
		const snapshotMsg = firstCall.find(
			(m) => m.id === PLANNER_MEMORY_SNAPSHOT_ID,
		);
		expect(snapshotMsg).toBeInstanceOf(SystemMessage);
	});
});

// ===========================================================================
// Shared core base — FR-034 semantics A (the base is NOT modified for the
// init/compact scenarios; their special cases live in the input request,
// buildInstructionRequest).
// ===========================================================================

describe("instruction node — shared core base (FR-034 semantics A)", () => {
	function capturingCreateAgent() {
		let captured: string | undefined;
		const createAgentFn = vi.fn((config: { systemPrompt?: string }) => {
			captured = config.systemPrompt ?? "";
			return { invoke: async () => ({ messages: [] as BaseMessage[] }) };
		});
		return { createAgentFn, systemPrompt: () => captured };
	}

	it("uses DEFAULT_PLANNER_BASE (the SAME base as the review planner) when planner_prompt is empty", () => {
		const { createAgentFn, systemPrompt } = capturingCreateAgent();
		createInstructionNode(
			{
				model: silentModel(),
				frozenSnapshot: new FrozenMemorySnapshot(),
				plannerBasePrompt: "",
				createAgentFn,
			},
			"init",
		);
		// The core prompt is shared verbatim — no scenario caveat is baked
		// into the systemPrompt (they are in the input request).
		expect(systemPrompt()).toBe(DEFAULT_PLANNER_BASE);
	});

	it("uses the profile planner_prompt verbatim when set (FR-034 semantics A)", () => {
		const { createAgentFn, systemPrompt } = capturingCreateAgent();
		const profilePrompt = "你是自定义的 planner。";
		createInstructionNode(
			{
				model: silentModel(),
				frozenSnapshot: new FrozenMemorySnapshot(),
				plannerBasePrompt: profilePrompt,
				createAgentFn,
			},
			"compact",
		);
		expect(systemPrompt()).toBe(profilePrompt);
	});
});
