/**
 * cordis plugin entry for the sts2 scene scaffold (`@dominion/dsh-sts2`):
 * the composition's lazy placeholder row
 * (specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md §3;
 * specs/079-sts2-template-scaffold/research.md D3/D6).
 *
 * The plugin exports the row id as its cordis name and an empty `apply`: it
 * provides no service and registers no tools or prompt sections, so mounting
 * the row exercises the packaging/deploy/bare-specifier resolution channel
 * without any runtime behaviour. The sts2 scene feature fills the
 * implementation in (survey/sts2-game-control.md §5,
 * survey/sts2-mod-poc.md §5).
 */

import type { Context } from "@deepseek-ai/cordis";

/** Composition row id (projects/game/agent_v2/cordis.yml). */
export const name = "sts2";

/** Lazy row: the scaffold requires no service to be present. */
export const inject: string[] = [];

/** Scaffold placeholder: intentionally empty until the scene is wired. */
export function apply(_ctx: Context): void {}
