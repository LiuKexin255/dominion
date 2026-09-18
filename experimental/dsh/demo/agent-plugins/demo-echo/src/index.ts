/**
 * cordis plugin entry for the demo echo tool: one model-facing tool and its
 * prompt guidance registered inside the SAME apply() — a preset composition
 * naming this row exposes both to the model, an omitted row exposes neither
 * (line-level selection consistency, FR-006 in
 * specs/058-dsh-preset-roster-demo/spec.md).
 * Contract: specs/058-dsh-preset-roster-demo/contracts/demo-echo-plugin.md.
 *
 * This is a PRESET-row plugin, not a host composition row: the roster sends
 * bare specifiers of preset rows to the host composition base, so
 * `@deepseek-ai/dsh-tools` and `@deepseek-ai/dsh-system-prompt` resolve from
 * the demo agent's node_modules
 * (specs/058-dsh-preset-roster-demo/research.md R8).
 */

import type { Context } from "@deepseek-ai/cordis";
import { defineTool } from "@deepseek-ai/dsh-tools";

export const name = "demo-echo";

export const inject = ["tools", "systemPrompt"];

/** The tool name the demo-tools preset presents to the model. */
export const DEMO_ECHO_TOOL = "demo_echo";

/** Deterministic output format asserted by unit tests and fake-llm scenarios. */
function echoText(text: string): string {
  return `echo: ${text}`;
}

export function apply(ctx: Context): void {
  ctx.tools.register(
    defineTool({
      name: DEMO_ECHO_TOOL,
      description: "Echo the provided text back unchanged.",
      parameters: {
        text: {
          type: "string",
          required: true,
          description: "Text to echo back.",
        },
      },
      output: {
        schema: { type: "string" },
        render: (_args, value) => [{ type: "text", text: value }],
      },
      async execute(args) {
        return echoText(args.text);
      },
    }),
  );

  ctx.systemPrompt.section({
    name: "demo-echo:guidance",
    // Tool-guidance band (demo-echo-plugin.md §3; order convention per
    // survey/deepseek-harness-team-mode.md §4.5). The heading carries the
    // tool name so fake-llm `system_keywords` can assert guidance presence
    // end to end (specs/058-dsh-preset-roster-demo/research.md R6).
    order: 100,
    text:
      `## ${DEMO_ECHO_TOOL}\n\n` +
      `${DEMO_ECHO_TOOL} echoes text back verbatim. ` +
      `Call it with the single required parameter \`text\`; ` +
      `the result is \`echo: {text}\`.`,
  });
}
