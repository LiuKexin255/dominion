/**
 * scenes.ts — the host-side scene registration of agent_v2: every scene
 * difference between templates (team roster, face member names, preset role
 * vocabulary, pool-template mapping, orchestration wiring, shared team goal)
 * is defined here, keyed by template id, instead of in the generic
 * server/session paths
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1;
 * specs/079-sts2-template-scaffold/research.md D2;
 * specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §2/§4).
 *
 * The scene's orchestration factory binds the entry's own data through a
 * closure; the scene registration modules never import this table
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§7).
 *
 * An unwired scene is data, not a branch: empty `roster`/`presetRoles` plus
 * `orchestratable: false` make its scene-scoped RPCs fail loud at the entry
 * point with `INVALID_ARGUMENT`, without entering orchestration or storage
 * (specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §3;
 * specs/079-sts2-template-scaffold/data-model.md §6).
 */

import type { DshContext } from "./dsh.js";
import type {
  SceneOrchestration,
  SceneRegistrationDeps,
} from "./orchestration.js";
import { createSaoleiOrchestration } from "./scene-saolei.js";

/** The host-side definition of one scene, keyed by template id. */
export interface SceneSpec {
  /** Fixed member roles of the materialized team, in orchestration order. */
  roster: readonly string[];
  /** Preset role vocabulary (empty = the scene has no preset vocabulary). */
  presetRoles: readonly string[];
  /**
   * Role → preset-authoring pool template id: a key of the `preset-authoring`
   * row Config `templates` snapshot
   * (specs/070-dsh-upgrade/contracts/preset-model-mapping.md §3).
   */
  rolePoolTemplate: Readonly<Record<string, string>>;
  /**
   * Whether the scene's orchestration is wired (false = materialize rejects at
   * the entry). This is the only wiring gate; the presence of
   * {@link SceneSpec.createOrchestration} is equivalent to it.
   */
  orchestratable: boolean;
  /**
   * Goal text of the shared team section; consumed by orchestratable scenes
   * only.
   */
  teamGoal: string;
  /**
   * Face-visible member names (including members the server adds to the
   * roster). Used only for member-name pre-checks and roster error text; the
   * runtime members and their kinds come from the orchestration's `members()`
   * projection, so this field declares no capability or behavior
   * (specs/080-host-main-sub-model/contracts/scene-registry.md §1).
   */
  faceMembers: readonly string[];
  /**
   * The scene's orchestration factory. Required exactly when
   * {@link SceneSpec.orchestratable} is true
   * (specs/080-host-main-sub-model/contracts/scene-registry.md §1).
   */
  createOrchestration?(
    ctx: DshContext,
    deps: SceneRegistrationDeps,
  ): SceneOrchestration;
}

/**
 * The saolei scene entry (the team scene). The orchestration factory binds the
 * entry itself, so the saolei registration module receives the scene data
 * without importing this table.
 */
const saoleiScene: SceneSpec = {
  roster: ["player", "planner"],
  presetRoles: ["player", "planner"],
  rolePoolTemplate: { player: "player", planner: "planner" },
  orchestratable: true,
  teamGoal:
    "协作完成多局扫雷游戏：player 执行操作、planner 复盘与制定策略，共同提高胜率。",
  faceMembers: ["player", "planner", "announcer"],
  createOrchestration: (ctx, deps) =>
    createSaoleiOrchestration(ctx, deps, saoleiScene),
};

/**
 * The scene table. `saolei` is the wired team scene; `sts2` is registered as a
 * template entry whose orchestration stays unwired
 * (specs/079-sts2-template-scaffold/spec.md FR-003/FR-004;
 * specs/080-host-main-sub-model/contracts/scene-registry.md §6). The keys are
 * also the TS-side known-template set: `KNOWN_TEMPLATES` in
 * projects/game/agent_v2/src/server.ts derives from them, so template
 * registration and scene wiring cannot drift.
 */
export const SCENES: Readonly<Record<string, SceneSpec>> = {
  saolei: saoleiScene,
  sts2: {
    roster: [],
    presetRoles: [],
    rolePoolTemplate: {},
    orchestratable: false,
    teamGoal: "",
    faceMembers: [],
  },
};

/**
 * The scene's definition when its team orchestration is wired: the single gate
 * predicate shared by the RPC entry point and the session registry, so an
 * unwired scene rejects before any structure/roster validation
 * (specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §3 item 1).
 */
export function wiredScene(template: string): SceneSpec | undefined {
  const scene = SCENES[template];
  return scene !== undefined && scene.orchestratable ? scene : undefined;
}

/**
 * The `INVALID_ARGUMENT` text for a scene whose team orchestration is not
 * wired. Shared by the RPC entry point and the session registry so the two
 * layers cannot drift
 * (specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §3 item 1).
 */
export function sceneNotWiredMessage(template: string): string {
  return `scene "${template}" is not wired for team orchestration yet`;
}
