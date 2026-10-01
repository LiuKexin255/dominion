/**
 * Unit tests for the terminal game-stats message template
 * (specs/065-agent-v2-team-refine/data-model.md §3; the pure text of
 * specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §2):
 * won/lost rendering, the total plus click/flag/chord breakdown taken from
 * the record stats, and determinism (no timestamp or game identity).
 */

import { describe, expect, it } from "vitest";

import type { GameEventRecord } from "./runtime.js";
import { gameStatsText } from "./text.js";

function record(overrides: Partial<GameEventRecord> = {}): GameEventRecord {
  return {
    status: "won",
    stats: {
      operationCount: 12,
      operationsByType: { click: 8, flag: 3, chord: 1 },
      correctFlags: 3,
      avgOpsPerMine: 4,
    },
    endedAt: 1_000,
    ...overrides,
  };
}

describe("gameStatsText", () => {
  it("renders the won/lost result line and the total/breakdown line from the record stats", () => {
    expect(gameStatsText(record())).toBe(
      "本局游戏结束：胜利。\n" +
        "本局共执行 12 个操作：click 8 次、flag 3 次、chord 1 次。",
    );
    expect(gameStatsText(record({ status: "lost" }))).toBe(
      "本局游戏结束：失败。\n" +
        "本局共执行 12 个操作：click 8 次、flag 3 次、chord 1 次。",
    );
  });

  it("renders an instant loss (all parts zero) without fabricating counts", () => {
    expect(
      gameStatsText(
        record({
          status: "lost",
          stats: {
            operationCount: 0,
            operationsByType: { click: 0, flag: 0, chord: 0 },
            correctFlags: null,
            avgOpsPerMine: "N/A",
          },
        }),
      ),
    ).toBe(
      "本局游戏结束：失败。\n" +
        "本局共执行 0 个操作：click 0 次、flag 0 次、chord 0 次。",
    );
  });

  it("is deterministic: the text never depends on the record's endedAt", () => {
    expect(gameStatsText(record({ endedAt: 0 }))).toBe(
      gameStatsText(record({ endedAt: 9_999_999 })),
    );
  });
});
