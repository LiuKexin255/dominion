/**
 * Spike T002 — verify research.md D4.
 *
 * Hypothesis (specs/023-saolei-mcp-refine/research.md D4): a `ToolMessage`
 * carrying `additional_kwargs.toolResultStatus` (the real ToolResultStatus
 * enum-name string) plus an `image_url` content block, when produced by a
 * tool and checkpointed via `MemorySaver`, round-trips intact — i.e.
 * `getState` returns the ToolMessage with both `additional_kwargs.toolResultStatus`
 * and the image content block still present.
 *
 * This is the status-carriage assumption: contracts/tool-dispatch-contract.md
 * §3..§4 + data-model.md §6 rely on `additional_kwargs.toolResultStatus`
 * surviving the checkpoint so `ListMessages` reads the real outcome (FR-012..015).
 *
 * Message shape mirrors projects/game/agent/src/tools/shared/result-blocks.ts
 * (`MouseContentBlock`: image block = `{ type: "image_url", image_url: { url } }`,
 * with the url as a `data:image/png;base64,...` string).
 *
 * If either the status or the image block is dropped by MemorySaver serde, D4
 * is falsified — STOP and report (the status-carriage design must be revised).
 */

import { describe, expect, it } from "vitest";

import { AIMessage, HumanMessage, ToolMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";
import { MemorySaver } from "@langchain/langgraph";
import { createAgent, tool } from "langchain";
import { z } from "zod";

const STATUS = "TOOL_RESULT_STATUS_SUCCEEDED";
const IMG_URL = "data:image/png;base64,AAAA";

describe("D4: ToolMessage additional_kwargs + image block survive MemorySaver", () => {
  it("preserves additional_kwargs.toolResultStatus and image_url content after getState", async () => {
    // The tool returns a ToolMessage directly (contracts/tool-dispatch-contract.md
    // §3 — when a tool returns a ToolMessage the ToolNode passes it through, so
    // additional_kwargs set inside the tool reach the checkpoint verbatim).
    // tool_call_id is read from config.toolCall.id when present (D2, verified in
    // spike.toolcall.test.ts) and falls back to a fixed value otherwise so this
    // spike stays independent of D2's outcome.
    const probe = tool(
      async (_args, config): Promise<ToolMessage> => {
        const tcId =
          (config as { toolCall?: { id?: string } } | undefined)?.toolCall?.id ??
          "spike-d4-fallback";
        return new ToolMessage({
          content: [
            { type: "text", text: "ok" },
            { type: "image_url", image_url: { url: IMG_URL } },
          ],
          tool_call_id: tcId,
          name: "probe",
          additional_kwargs: { toolResultStatus: STATUS },
        });
      },
      {
        name: "probe",
        description: "spike probe",
        schema: z.object({}),
      },
    );

    const model = fakeModel()
      .respondWithTools([{ name: "probe", args: {} }])
      .respond(new AIMessage("done"));

    const checkpointer = new MemorySaver();
    const agent = createAgent({
      model,
      tools: [probe],
      checkpointer,
    });

    const threadId = "spike-d4";
    await agent.invoke(
      { messages: [new HumanMessage("call probe")] },
      { configurable: { thread_id: threadId } },
    );

    // Read the checkpointed state back (the history-reconstruction path:
    // handler.ts ListMessages reads exactly this getState().values.messages).
    const state = (await agent.getState({
      configurable: { thread_id: threadId },
    })) as {
      values: {
        messages: Array<{
          _getType?: () => string;
          constructor?: { name: string };
          additional_kwargs?: Record<string, unknown>;
          content: unknown;
        }>;
      };
    };

    const messages = state.values.messages;
    expect(Array.isArray(messages)).toBe(true);

    const toolMsg = messages.find(
      (m) =>
        m._getType?.() === "tool" || m.constructor?.name === "ToolMessage",
    );
    expect(toolMsg).toBeDefined();

    // D4 assertion 1 — additional_kwargs.toolResultStatus survives.
    expect(toolMsg!.additional_kwargs?.toolResultStatus).toBe(STATUS);

    // D4 assertion 2 — image_url content block survives.
    const content = toolMsg!.content;
    expect(Array.isArray(content)).toBe(true);
    const blocks = content as Array<{
      type: string;
      image_url?: { url: string };
    }>;
    const img = blocks.find((b) => b.type === "image_url");
    expect(img).toBeDefined();
    expect(img!.image_url?.url).toBe(IMG_URL);

    // eslint-disable-next-line no-console
    console.warn(
      "[spike.checkpoint D4] toolResultStatus=",
      toolMsg!.additional_kwargs?.toolResultStatus,
      "| image block present=",
      img !== undefined,
      "| content block count=",
      blocks.length,
    );
  });
});
