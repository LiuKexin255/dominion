import { describe, expect, it, vi } from "vitest";
import { OrchestratorStateError } from "@dominion/dsh-saolei-loop";
import type {
  AnnouncerMember,
  OrchestratorFailure,
  OrchestratorSnapshot,
} from "@dominion/dsh-saolei-loop";

import { SceneStateError } from "./orchestration.js";
import type { SceneMaterializeInput } from "./orchestration.js";
import { adaptSaoleiOrchestration } from "./scene-saolei.js";
import type {
  SaoleiSceneData,
  SaoleiTeamOrchestration,
} from "./scene-saolei.js";
import { SCENES } from "./scenes.js";

/**
 * Unit tests for the saolei scene registration
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§2/§5): the
 * adapter's materialization derivation, the announce-only announcer
 * invariant, the member projection and the seam state-error translation.
 * Orchestration doubles enter through the adapter's injection point
 * (style/javascript.md Mock convention).
 */

const SPEC: SaoleiSceneData = { teamGoal: "协作完成多局扫雷游戏" };

const ANNOUNCER = {} as AnnouncerMember;

const INPUT: SceneMaterializeInput = {
  session: "templates/saolei/sessions/s1",
  template: "saolei",
  members: [
    {
      role: "player",
      preset: "player-1",
      provider: "glm-responses",
      model: "glm-5.3",
    },
    {
      role: "planner",
      preset: "planner-1",
      provider: "opencode-go",
      model: "kimi-k3",
    },
  ],
};

function snapshot(
  overrides: Partial<OrchestratorSnapshot> = {},
): OrchestratorSnapshot {
  return {
    materialized: true,
    phase: "playing",
    active: null,
    activation: "player",
    paused: false,
    queued: 0,
    failed: false,
    lastError: null,
    ...overrides,
  };
}

function teamDouble(
  overrides: Partial<SaoleiTeamOrchestration> = {},
): SaoleiTeamOrchestration {
  return {
    materialize: vi.fn(async () => undefined),
    member: vi.fn(() => undefined),
    teamEvents: vi.fn(() => ({ subscribe: () => () => undefined })),
    snapshot: vi.fn(() => snapshot()),
    submit: vi.fn(() => ({ queued: false, messageId: "1" })),
    cancel: vi.fn(() => ({ dropped: [] })),
    whenQuiescent: vi.fn(async () => undefined),
    dispose: vi.fn(async () => undefined),
    announcer: ANNOUNCER,
    ...overrides,
  };
}

describe("SaoleiSceneOrchestration", () => {
  it("forwards the scene goal and derives the planner memory scope", async () => {
    const orchestrator = teamDouble();

    await adaptSaoleiOrchestration(orchestrator, SPEC).materialize(INPUT);

    expect(orchestrator.materialize).toHaveBeenCalledOnce();
    expect(orchestrator.materialize).toHaveBeenCalledWith({
      session: INPUT.session,
      // The planner preset id, not the session template or the preset resource
      // name (specs/071-memory-preset-scope/contracts/dsh-plugins.md §4).
      memoryScope: { template: "saolei", preset: "planner-1" },
      goal: SPEC.teamGoal,
      player: {
        preset: "player-1",
        provider: "glm-responses",
        model: "glm-5.3",
      },
      planner: {
        preset: "planner-1",
        provider: "opencode-go",
        model: "kimi-k3",
      },
    });
  });

  it("rolls back and rejects when the announcer member is missing", async () => {
    const orchestrator = teamDouble({ announcer: undefined });

    await expect(
      adaptSaoleiOrchestration(orchestrator, SPEC).materialize(INPUT),
    ).rejects.toThrow(
      "team materialization invariant violated: the announcer member is missing",
    );

    expect(orchestrator.materialize).toHaveBeenCalledOnce();
    expect(orchestrator.dispose).toHaveBeenCalledOnce();
  });

  it("projects the three members in presentation order once materialized", () => {
    const orchestration = adaptSaoleiOrchestration(teamDouble(), SPEC);

    expect(orchestration.members()).toEqual([
      { role: "player", kind: "agent" },
      { role: "planner", kind: "agent" },
      { role: "announcer", kind: "announce-only" },
    ]);
  });

  it("reports an empty projection while unmaterialized", () => {
    const orchestrator = teamDouble({
      snapshot: vi.fn(() => snapshot({ materialized: false })),
    });

    expect(adaptSaoleiOrchestration(orchestrator, SPEC).members()).toEqual([]);
  });

  it("drops the saolei phase field from the snapshot", () => {
    const lastError: OrchestratorFailure = {
      message: "boom",
      member: "planner",
      phase: "planning",
      code: "TRANSIENT",
      origin: "member-turn",
      detail: { engineCode: "engine-x", message: "boom" },
    };
    const orchestrator = teamDouble({
      snapshot: vi.fn(() =>
        snapshot({
          active: "planner",
          phase: "planning",
          failed: true,
          lastError,
        }),
      ),
    });

    const view = adaptSaoleiOrchestration(orchestrator, SPEC).snapshot();

    expect(view).toEqual({
      materialized: true,
      active: "planner",
      activation: "player",
      paused: false,
      queued: 0,
      failed: true,
      lastError: {
        message: "boom",
        member: "planner",
        code: "TRANSIENT",
        origin: "member-turn",
        detail: { engineCode: "engine-x", message: "boom" },
      },
    });
    expect("phase" in view).toBe(false);
  });

  it("translates the package state error into the seam state error", async () => {
    const orchestrator = teamDouble({
      materialize: vi.fn(async () => {
        throw new OrchestratorStateError(
          "ALREADY_MATERIALIZED",
          "team is already materialized",
        );
      }),
    });

    const materialize = adaptSaoleiOrchestration(
      orchestrator,
      SPEC,
    ).materialize(INPUT);

    await expect(materialize).rejects.toBeInstanceOf(SceneStateError);
    await expect(materialize).rejects.toMatchObject({
      code: "ALREADY_MATERIALIZED",
      message: "team is already materialized",
    });
  });

  it("pins the face member names to the member projection", () => {
    expect(SCENES["saolei"].faceMembers).toEqual([
      "player",
      "planner",
      "announcer",
    ]);
    expect(
      adaptSaoleiOrchestration(teamDouble(), SPEC)
        .members()
        .map((member) => member.role),
    ).toEqual(SCENES["saolei"].faceMembers);
  });
});
