# Contract: saolei_remain 语义表述（saolei-plugins 修订）

> 修订 `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2（结果文本契约索引）下 `specs/051-agent-v2-dsh-migration/data-model.md` §2.5 的 remain 条目（工具结果体形态），以及 `specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1（`saolei:game` 玩法规则行）/§2（`saolei:guidance` 工具条目）所有权分界下的两处 prompt 表述。`saolei_init`/`saolei_operate` 的结果体与表述不变。工具名 `saolei_remain` 不变。

## 1. 结果体（remainText 终态）

```text
saolei_remain → computed
game status: <won|lost|playing>

board size <w>*<h>
legend: each value = mines still unmarked around that number cell = cell number − adjacent flags (0 or negative when over-flagged); it is NOT the count of flags. Columns are x and rows are y — the same (x, y) as saolei_operate.

<坐标标尺网格（本体格式不变）>
```

- legend 行位置：`board size` 行之后、网格之前（单行）。
- 前缀两行（outcome/状态）与网格本体格式不变；既有前缀断言零破坏。
- legend 为结果体的一部分：模型与人类读者不依赖外部文档即可解读（自描述）。

## 2. 工具 description（`common/js/dsh-plugins/saolei/src/index.ts`）

主语义前置：对每个已揭示数字格返回其周围**剩余未标记雷数**（= 数字 − 相邻已标旗数，可为 0 或负）；显式声明不是旗子数量；保留公式派生说明与既有边界（`-` 哨兵、`no_active_game` 拒绝、终局棋盘不被阻断）。

## 3. prompt 表述（`common/js/dsh-plugins/saolei-loop/src/index.ts` SAOLEI_GAME_RULES）

- remain 行：同向改写为"剩余未标记雷数"主语义 + 旗数排除（中文，随所在文本语言）。
- 全局剩余雷数计数行（总雷数 − 已标旗数）：保留，明确其为顶部计数器概念，与 per-cell remain 区分表述。

## 4. 一致性要求

三处表述（description / 规则 prompt / 结果体 legend）语义 MUST 一致：主语义 = 剩余未标记雷数；公式 = 数字 − 相邻已标旗数（可为 0/负）；显式排除旗数读法；坐标读法（列 = x、行 = y）在 legend 与既有 Coordinate ruler 条目双锚定。
