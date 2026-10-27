/**
 * scene-sts2.ts — the sts2 scene registration of agent_v2.
 *
 * Constructs the sts2 minimal orchestration (`@dominion/dsh-sts2-loop`) over
 * the host-injected scene-neutral seams. The sts2 scene has a single main
 * agent and no team relay, announcer or memory mount, so the factory is a
 * direct construction with no scene-local derivation
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §6;
 * specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md §2/§3).
 *
 * The scene table (`scenes.ts`) binds its own entry through the closure that
 * calls this factory; this module never imports the scene table, so the
 * runtime dependency edge stays one-way
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§7).
 */

import { Sts2Orchestrator } from "@dominion/dsh-sts2-loop";

import type { DshContext } from "./dsh.js";
import type {
  SceneOrchestration,
  SceneRegistrationDeps,
} from "./orchestration.js";

/**
 * Build the sts2 orchestration for the scene table entry: one
 * `Sts2Orchestrator` over the host-injected scene-neutral seams. The
 * orchestration project is independent of the host, so the scene registration
 * performs no adaptation: it only selects which seams the scene consumes and
 * passes through what it needs
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §6).
 */
export function createSts2Orchestration(
  ctx: DshContext,
  deps: SceneRegistrationDeps,
): SceneOrchestration {
  return new Sts2Orchestrator(ctx, {
    compose: deps.compose,
    agents: deps.agents,
    logger: deps.logger,
    provider: deps.provider,
  });
}
