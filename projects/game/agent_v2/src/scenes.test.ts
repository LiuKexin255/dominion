import { describe, expect, it } from "vitest";
import { SCENES } from "./scenes.js";

/**
 * Unit tests for the host-side scene table
 * (specs/079-sts2-template-scaffold/data-model.md §2;
 * specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §2/§3): the
 * pinned values are the saolei zero-change baseline and the sts2 "registered
 * but not wired" data. The readonly shape is a type-level contract — the
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
    });
  });

  it("pins the sts2 scene values (registered but not wired)", () => {
    expect(SCENES["sts2"]).toEqual({
      roster: [],
      presetRoles: [],
      rolePoolTemplate: {},
      orchestratable: false,
      teamGoal: "",
    });
  });
});
