# tools/

This directory holds the agent's tool factories — one folder per tool. A tool
factory binds a session-scoped `OperationBridge` to a `StructuredToolInterface`
produced by the `tool()` factory from the `langchain` package.

## File-format contract (lead)

* **One folder per tool.** Folder name === the tool's `langchain` `name` field
  verbatim. Example: tool name `mouse_move` → folder `src/tools/mouse_move/`.
  This deliberate underscore-in-folder-name divergence from kebab-case exists
  so the folder path equals the value users put in a profile's `tool_names`
  array.
* **Files inside `{tool_name}/`:**
  * `<kebab-case-name>.ts` (kebab-case per repo convention) exporting
    `create{ToolName}Tool(bridge: OperationBridge): StructuredToolInterface`.
  * `<kebab-case-name>.test.ts` — side-by-side test (repo convention).
* **`src/tools/shared/`** holds cross-tool helpers referenced by 2+ tool
  folders. Files in `shared/` are kebab-case (e.g. `result-blocks.ts`).
* **`src/tools/types.ts`** holds the `StandaloneExtras` type and the
  `isStandalone` helper (see "standalone attribute" below).

## Adding a new tool

1. Create `src/tools/{tool_name}/` where `{tool_name}` is the exact
   `langchain` `name` field (e.g. `my_tool/` if the tool name is `my_tool`).
2. Author the factory in `<kebab-case-name>.ts` using the `tool()` factory
   from `langchain`:

   ```typescript
   import { tool } from "langchain";
   import { z } from "zod";
   import type { StructuredToolInterface } from "@langchain/core/tools";
   import type { OperationBridge } from "../../operation-bridge";
   import type { StandaloneExtras } from "../types";

   const mySchema = z.object({ /* ... */ });

   export function createMyTool(bridge: OperationBridge): StructuredToolInterface {
     return tool(
       async (input, config) => { /* ... */ },
       {
         name: "my_tool",
         description: "...",
         schema: mySchema,
         extras: { standalone: true } satisfies StandaloneExtras,
       },
     );
   }
   ```
3. Set `extras: { standalone: <bool> } satisfies StandaloneExtras` in the
   options bag — see the standalone contract below.
4. Add `<kebab-case-name>.test.ts` next to the source file (side-by-side; repo
   convention).
5. Wire into `buildTools` at `projects/game/agent/src/llm.ts:57` — extend the
   existing `if/else if` dispatch on string tool names so a profile's
   `tool_names` entry resolves to the new factory. Unknown names are silently
   skipped today; the dispatch stays as `if/else if`, not a registry.
6. Run `bazel run //:gazelle projects/game/agent` to regenerate
   `BUILD.bazel`. The existing `glob(["src/**/*.ts"], exclude = ["**/*.test.ts"])`
   at `projects/game/agent/BUILD.bazel:19-22` and the `vitest_test` data glob
  at `projects/game/agent/BUILD.bazel:136-139` already sweep the whole `src/`
  tree, so no manual glob edit is required.

## The `standalone` attribute

Every tool MUST set `extras.standalone` explicitly. Semantics (authoritative
contract: `specs/020-agent-resources-layout/contracts/tool-definition.md`):

| Value   | Desktop profile-config page | Agent runtime dispatch                                     |
| ------- | --------------------------- | --------------------------------------------------------- |
| `true`  | Listed as individually-selectable. | Tool instantiated + dispatched normally when a profile's `tool_names` references it. |
| `false` | HIDDEN (intended for future toolset / collection use only). | Tool STILL instantiated + dispatched normally when a profile's `tool_names` references it. |

* Default when omitted: `true` (preserves today's desktop-display behavior).
  This default is a safety net — contributors MUST set `extras.standalone`
  explicitly on every tool definition. Use the `satisfies StandaloneExtras`
  pattern shown above so a missing `standalone` key fails to type-check.
* Scope of effect: desktop UI visibility ONLY. `standalone` never affects
  runtime behavior, tool dispatch, profile binding, or agent composition.
* Read sites use the `isStandalone(tool)` helper from `src/tools/types.ts`,
  which casts `tool.extras` to `Partial<StandaloneExtras> | undefined` (the
  upstream `extras` field is typed `Record<string, unknown> | undefined` on
  `StructuredToolInterface`).
