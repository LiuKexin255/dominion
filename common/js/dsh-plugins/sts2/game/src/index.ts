/**
 * cordis plugin entry for the sts2 domain game foundation
 * (`@dominion/dsh-sts2`): the composition's lazy foundation row
 * (specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md §1/§3;
 * specs/079-sts2-template-scaffold/research.md D3/D6).
 *
 * The row is the sts2 domain foundation package and carries no capability:
 * the plugin exports the row id as its cordis name and an empty `apply`, so it
 * provides no service and registers no tools or prompt sections
 * (specs/080-host-main-sub-model/spec.md FR-010). sts2 orchestration is owned
 * by `@dominion/dsh-sts2-loop`
 * (common/js/dsh-plugins/sts2/loop/,
 * specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md §1); game
 * runtime and tools belong to later sts2 features
 * (survey/sts2-game-control.md §5, survey/sts2-mod-poc.md §5).
 */

import type { Context } from "@deepseek-ai/cordis";

/** Composition row id (projects/game/agent_v2/cordis.yml). */
export const name = "sts2";

/** Lazy row: the scaffold requires no service to be present. */
export const inject: string[] = [];

/** Foundation row: provides no service, tools or prompt sections. */
export function apply(_ctx: Context): void {}
