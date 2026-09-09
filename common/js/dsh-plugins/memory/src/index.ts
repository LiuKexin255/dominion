/**
 * cordis plugin entry for the planner-memory plugin's host service face —
 * the minimal composable placeholder: an empty apply with no service
 * requirements. The ctx.plannerMemory contract (fail-loud snapshot load +
 * write path to the memory service) is
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §3; the preset-row
 * tool face is the `./preset-row` export.
 */

import type { Context } from "@deepseek-ai/cordis";

export const name = "memory";

/** Host-row plugin: no service requirements. */
export const inject: string[] = [];

export function apply(_ctx: Context): void {}
