/**
 * The three wiki tools of the functional wiki unit: `wiki_search`, `wiki_get`,
 * `wiki_write`. Each is a thin, stateless model-facing entry point over the
 * agent-scoped wiki face. The invocation never throws: every
 * failure (domain or infrastructure) is a TEXT result
 * (specs/068-wiki-round-pause/spec.md FR-011), and no model-visible text ever
 * carries a wiki resource `name`/`wiki_id` — titles are the only address the
 * model sees (specs/068-wiki-round-pause/spec.md FR-009).
 *
 * Each description describes only the tool itself; the cross-tool workflow
 * lives in the unit's prompt sections, not here
 * (specs/068-wiki-round-pause/spec.md FR-013). The parameter-schema policy
 * mirrors `tool.ts`: the root stays open (a single-XOR-batch model mistake is a
 * recoverable text answer, not a schema failure) and `entries` items reject
 * unknown keys (`additionalProperties: false`).
 *
 * The face is resolved per call through `exec.agent.ctx.wiki` — the key the
 * wiki unit's mount registers — so the wiki tools work with the wiki unit
 * mounted alone, and a missing face fails loud with the memory tool's wording
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 1).
 */

import { defineTool } from "@deepseek-ai/dsh-tools";
import type { ToolDefinition, ToolRunContext } from "@deepseek-ai/dsh-tools";

import type { WikiFace } from "./face.js";

/** The tool names (one per wiki operation; there is no list tool). */
export const WIKI_SEARCH_TOOL_NAME = "wiki_search";
export const WIKI_GET_TOOL_NAME = "wiki_get";
export const WIKI_WRITE_TOOL_NAME = "wiki_write";

/** Model-facing description: search titles only, matching semantics. */
export const WIKI_SEARCH_TOOL_DESCRIPTION =
  "Search the wiki by title and return the matching titles only (no " +
  "content). Matching is case-insensitive and full-width/half-width " +
  "insensitive, at any position within a title, and supports CJK input.";

/** Model-facing description: batch retrieve content by exact title. */
export const WIKI_GET_TOOL_DESCRIPTION =
  "Retrieve the full content of wiki entries by exact title. Accepts " +
  "one or more titles; titles that do not resolve to an entry are reported in " +
  "the result together with the entries that did.";

/** Model-facing description: upsert by title, single XOR batch. */
export const WIKI_WRITE_TOOL_DESCRIPTION =
  "Create or update wiki entries addressed by title: an entry whose " +
  "normalized title already exists has its content overwritten, otherwise it " +
  "is created. Accepts a single title/content pair OR an entries batch.";

/**
 * `wiki_search` parameters: `query` is the only parameter. The root stays open
 * (like `tool.ts`): the empty-query guidance is a text result, not a schema
 * failure.
 */
export const WIKI_SEARCH_TOOL_PARAMETERS = {
  query: {
    type: "string",
    description: "The title substring to search for",
  },
} as const;

/** `wiki_get` parameters: one or more exact titles. */
export const WIKI_GET_TOOL_PARAMETERS = {
  titles: {
    type: "array",
    description: "The exact titles whose full content to retrieve",
    items: { type: "string" },
  },
} as const;

/** `wiki_write` parameters: the single-entry form XOR the `entries` batch. */
export const WIKI_WRITE_TOOL_PARAMETERS = {
  title: {
    type: "string",
    description:
      "single-entry form: the entry title (mutually exclusive with entries)",
  },
  content: {
    type: "string",
    description:
      "single-entry form: the entry body (mutually exclusive with entries)",
  },
  entries: {
    type: "array",
    description:
      "batch form: ordered {title, content} entries (mutually exclusive with title/content)",
    items: {
      type: "object",
      additionalProperties: false,
      properties: {
        title: { type: "string", required: true },
        content: { type: "string", required: true },
      },
    },
  },
} as const;

/** Arguments accepted by the `wiki_search` tool (all root fields optional). */
export interface WikiSearchToolArgs {
  query?: string;
}

/** Arguments accepted by the `wiki_get` tool. */
export interface WikiGetToolArgs {
  titles?: string[];
}

/** Arguments accepted by the `wiki_write` tool. */
export interface WikiWriteToolArgs {
  title?: string;
  content?: string;
  entries?: { title?: string; content?: string }[];
}

/** The canonical `{result: string}` output (the memory tool's declaration shape). */
const WIKI_RESULT_OUTPUT_SCHEMA = {
  type: "object",
  properties: {
    result: {
      type: "string",
      required: true,
      description:
        "The wiki result text: the matching titles, the retrieved content, the write outcome, or `wiki failed: …`",
    },
  },
  additionalProperties: false,
} as const;

function describeErr(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Resolve the calling agent's wiki face. Failure is fail-loud and mirrors
 * {@link resolveMemoryFace} (`./tool.js`): a missing `exec.agent`
 * (non-loop-driven dispatch) or an unregistered `wiki` face (the orchestration
 * did not mount the wiki unit) throws, and the pipeline turns the throw into a
 * model-visible error result — never a fabricated success.
 *
 * Resolution reads the `wiki` context PROPERTY (the proxy's fiber-walking
 * lookup), not `ctx.get`: the orchestration registers the face under an
 * isolation label (`agentCtx.isolate("wiki", face)`), which the property walk
 * resolves from the shared agent fiber while `get`'s label-keyed store lookup
 * would not (saolei `saoleiGame` precedent).
 *
 * @param toolName The calling tool's name, so the missing-agent text names the
 *   tool that actually ran.
 */
export function resolveWikiFace(
  exec: ToolRunContext,
  toolName = WIKI_SEARCH_TOOL_NAME,
): WikiFace {
  if (exec.agent === undefined) {
    throw new Error(
      `${toolName} tool requires an agent caller: no agent on the tool execution`,
    );
  }
  const face = (exec.agent.ctx as { wiki?: WikiFace }).wiki;
  if (face === undefined) {
    throw new Error(
      `wiki tools require the agent-scoped "wiki" service; agent "${exec.agent.id}" has none\n` +
        "(the team-layer materialization setup registers it from the wiki capability mount)",
    );
  }
  return face;
}

/**
 * Execute one `wiki_search`. Any failure — missing agent caller, unregistered
 * face, storage outage — becomes a `wiki failed: …` TEXT result, so the
 * conversation is never interrupted (specs/068-wiki-round-pause/spec.md
 * FR-011).
 */
export async function executeWikiSearchTool(
  args: WikiSearchToolArgs,
  exec: ToolRunContext,
): Promise<{ result: string }> {
  try {
    return {
      result: await resolveWikiFace(exec, WIKI_SEARCH_TOOL_NAME).wikiSearch(
        args.query ?? "",
      ),
    };
  } catch (err) {
    return { result: `wiki failed: ${describeErr(err)}` };
  }
}

/** Execute one `wiki_get` (content for every resolved title, misses listed). */
export async function executeWikiGetTool(
  args: WikiGetToolArgs,
  exec: ToolRunContext,
): Promise<{ result: string }> {
  try {
    return {
      result: await resolveWikiFace(exec, WIKI_GET_TOOL_NAME).wikiGet(
        args.titles ?? [],
      ),
    };
  } catch (err) {
    return { result: `wiki failed: ${describeErr(err)}` };
  }
}

/** Execute one `wiki_write` (upsert; single XOR batch). */
export async function executeWikiWriteTool(
  args: WikiWriteToolArgs,
  exec: ToolRunContext,
): Promise<{ result: string }> {
  try {
    return {
      result: await resolveWikiFace(exec, WIKI_WRITE_TOOL_NAME).wikiWrite({
        title: args.title,
        content: args.content,
        entries: args.entries,
      }),
    };
  } catch (err) {
    return { result: `wiki failed: ${describeErr(err)}` };
  }
}

/** Build the output declaration shared by the three tools. */
function wikiResultOutput() {
  return {
    schema: WIKI_RESULT_OUTPUT_SCHEMA,
    render: (_args: unknown, value: { result: string }) => [
      { type: "text" as const, text: value.result },
    ],
  };
}

/** Build the registry-ready `wiki_search` tool definition. */
export function createWikiSearchToolDefinition(): ToolDefinition {
  return defineTool({
    name: WIKI_SEARCH_TOOL_NAME,
    description: WIKI_SEARCH_TOOL_DESCRIPTION,
    parameters: WIKI_SEARCH_TOOL_PARAMETERS,
    output: wikiResultOutput(),
    execute: (args, exec) => executeWikiSearchTool(args, exec),
  });
}

/** Build the registry-ready `wiki_get` tool definition. */
export function createWikiGetToolDefinition(): ToolDefinition {
  return defineTool({
    name: WIKI_GET_TOOL_NAME,
    description: WIKI_GET_TOOL_DESCRIPTION,
    parameters: WIKI_GET_TOOL_PARAMETERS,
    output: wikiResultOutput(),
    execute: (args, exec) => executeWikiGetTool(args, exec),
  });
}

/** Build the registry-ready `wiki_write` tool definition. */
export function createWikiWriteToolDefinition(): ToolDefinition {
  return defineTool({
    name: WIKI_WRITE_TOOL_NAME,
    description: WIKI_WRITE_TOOL_DESCRIPTION,
    parameters: WIKI_WRITE_TOOL_PARAMETERS,
    output: wikiResultOutput(),
    execute: (args, exec) => executeWikiWriteTool(args, exec),
  });
}
