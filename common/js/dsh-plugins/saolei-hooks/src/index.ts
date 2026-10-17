/**
 * cordis plugin entry for the saolei hook engine
 * (`@dominion/dsh-saolei-hooks`, specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §1): the composition's `saoleiHooks` capability
 * provider, mounted as a saolei-loop `config.plugins` row by bare package name
 * and read back by the orchestration to assemble the planner tool face and the
 * player board-change handler over one shared registry
 * (specs/078-saolei-js-hooks/data-model.md §3.1).
 *
 * The provide face is a team-level factory, not a ready unit:
 * `mount.buildHooksUnit({ announce })` runs once per team materialization and
 * closes over one `HookRegistry`, so the planner's `saolei_hook` writes and the
 * player's board-change handler reads the same process-memory store. The unit
 * carries NO prompt section — the whole guidance is the `saolei_hook` tool
 * description (specs/078-saolei-js-hooks/spec.md FR-006).
 *
 * The announcement seam is injected by the orchestration (which binds it to the
 * announcer it holds,
 * specs/078-saolei-js-hooks/contracts/hook-announcement.md §1), so this package
 * stays a pure provider: it depends only on `@dominion/dsh-saolei` types and
 * the framework faces, never on saolei-tools, the orchestration, the team
 * service or the announcer (specs/078-saolei-js-hooks/spec.md FR-016).
 *
 * This module declares the plugin identity and the mount; the engine-wide
 * resource caps shared by the registry and the sandbox live in `./limits.js`,
 * and the registry / sandbox executor / tool / handler live in their own
 * modules.
 */

import type { Context } from "@deepseek-ai/cordis";
import type { PromptSection } from "@deepseek-ai/dsh-system-prompt";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { GameLogEntry } from "@dominion/dsh-saolei";

import { createOnBoardChange } from "./handler.js";
import { HookRegistry } from "./registry.js";
import { createSaoleiHookTool } from "./tool.js";

/** The plugin identifier the composition manifest references. */
export const name = "saolei-hooks";

/** Pure provider: the mount is exposed at plugin load, nothing is required. */
export const inject: string[] = [];

/** The provide name the saolei-loop orchestration reads the mount from. */
export const SAOLEI_HOOKS_SERVICE = "saoleiHooks";

/**
 * The per-team unit one `buildHooksUnit` call produces
 * (specs/078-saolei-js-hooks/data-model.md §3.1): the planner tool face, the
 * section list and the player board-change handler, all over one shared
 * registry. `sections` is empty by contract — there is no hook prompt section
 * (specs/078-saolei-js-hooks/spec.md FR-006); the field exists so the
 * orchestration registers the unit with the same tools → sections pattern it
 * uses for the saolei static unit.
 */
export interface SaoleiHooksUnit {
  readonly tools: readonly ToolDefinition[];
  readonly sections: readonly PromptSection[];
  readonly onBoardChange: (entry: GameLogEntry) => void;
}

/**
 * The plugin's provide face: the team-level factory the orchestration invokes
 * once per materialization. `announce` is the announcement seam the unit
 * broadcasts each non-empty hook result through; one unit = one team
 * materialization (a refresh builds a fresh unit and so an empty registry).
 */
export interface SaoleiHooksMount {
  buildHooksUnit(deps: { announce: (text: string) => void }): SaoleiHooksUnit;
}

/**
 * Apply the plugin: provide the mount under `saoleiHooks`. Nothing is
 * registered on the composition scope — the unit's tool and section
 * registration happens per member in the orchestration setup
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5).
 */
export function apply(ctx: Context): void {
  ctx.provide(SAOLEI_HOOKS_SERVICE, buildMount());
}

/** Build the stateless mount: every `buildHooksUnit` call creates a fresh
 * registry, and the tool and the handler close over that one instance. */
function buildMount(): SaoleiHooksMount {
  return {
    buildHooksUnit({ announce }): SaoleiHooksUnit {
      const registry = new HookRegistry();
      return {
        tools: [createSaoleiHookTool(registry)],
        sections: [],
        onBoardChange: createOnBoardChange({ registry, announce }),
      };
    },
  };
}
