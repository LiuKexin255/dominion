/**
 * cordis plugin entry for the planner-memory plugin's preset-row face — the
 * minimal composable placeholder: an empty apply. Row naming follows the
 * subpath-plugin-row precedent (the dsh invariant rows in
 * projects/game/agent_v2/cordis.yml). The memory tool + snapshot section
 * contract is specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §3.
 */

import type { Context } from "@deepseek-ai/cordis";

export const name = "memory-row";

/** Scope-only row: no service requirements. */
export const inject: string[] = [];

export function apply(_ctx: Context): void {}
