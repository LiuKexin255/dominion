import { describe, expect, it } from "vitest";
import { SCENES, sceneNotWiredMessage, wiredScene } from "./scenes.js";

/**
 * Unit tests for the host-side scene registration
 * (specs/080-host-main-sub-model/contracts/scene-registry.md §1/§6;
 * specs/079-sts2-template-scaffold/data-model.md §2;
 * specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §2/§3): the
 * pinned values are the saolei zero-change baseline and the sts2
 * single-main-agent scene. The readonly shape is a type-level contract — the
 * SceneSpec fields are declared readonly, so mutation is rejected at type
 * check time and needs no runtime assertion.
 */

describe("SCENES", () => {
  it("indexes the scene table by template id", () => {
    expect(Object.keys(SCENES).sort()).toEqual(["saolei", "sts2"]);
  });

  it("pins the saolei scene values (the wired team scene)", () => {
    expect(SCENES["saolei"]).toEqual({
      roster: ["player", "planner"],
      presetRoles: ["player", "planner"],
      rolePoolTemplate: { player: "player", planner: "planner" },
      orchestratable: true,
      // Byte-for-byte the team goal the host rendered before the table.
      teamGoal:
        "协作完成多局扫雷游戏：player 执行操作、planner 复盘与制定策略，共同提高胜率。",
      faceMembers: ["player", "planner", "announcer"],
      createOrchestration: expect.any(Function),
    });
  });

  it("pins the sts2 scene values (the wired single-main-agent scene)", () => {
    expect(SCENES["sts2"]).toEqual({
      roster: ["main"],
      presetRoles: ["main"],
      rolePoolTemplate: { main: "main" },
      orchestratable: true,
      teamGoal: "",
      faceMembers: ["main"],
      createOrchestration: expect.any(Function),
    });
  });

  it("wires a factory exactly when the scene is orchestratable", () => {
    for (const [template, scene] of Object.entries(SCENES)) {
      expect(
        scene.createOrchestration !== undefined,
        `${template} factory presence`,
      ).toBe(scene.orchestratable);
    }
  });

  it("resolves wired scenes and keeps the not-wired mechanism for unknown templates", () => {
    expect(wiredScene("saolei")).toBe(SCENES["saolei"]);
    expect(wiredScene("sts2")).toBe(SCENES["sts2"]);
    // Both registered scenes are wired; the gate and its message remain the
    // single rejection mechanism for the unknown-template key
    // (specs/080-host-main-sub-model/contracts/scene-registry.md §7).
    expect(wiredScene("unknown-scene")).toBeUndefined();
    expect(sceneNotWiredMessage("unknown-scene")).toBe(
      'scene "unknown-scene" is not wired for team orchestration yet',
    );
  });
});
