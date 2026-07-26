# @dominion/game-saolei-board

扫雷截图识别公共库：将桌面端捕获的扫雷截图（PNG）识别为结构化的游戏棋盘状态，
输出可被 LLM 直接读取的文字棋盘。用于替代 saolei MCP 原先直接返回截图给模型的
方式，把视觉识别从 LLM 卸载到确定性的颜色分析代码，提升操作准确率。

面向经典 Win32 Microsoft Minesweeper（`winmine.exe`，board 几何 origin 24/200、
cell 32×32px）。识别基于固定几何 + 单格颜色分析，不依赖 OCR 或重型 CV 库。

## 组成

```
src/
├── core/   # 核心库（可被其他包引用，即 @dominion/game-saolei-board）
└── cli/    # CLI 外壳（saolei-recognize），依赖核心库
```

两者拆分为独立 Bazel target，便于其他代码只引用核心库而不引入 CLI。

## 核心库用法

```ts
import {
  recognizeBoard,
  SaoleiBoard,
  renderBoardText,
} from "@dominion/game-saolei-board";

// 一次性识别整盘
const { state } = recognizeBoard(pngBytes);
console.log(renderBoardText(state));
// board size 9*9
//
// * * * 1 0 0 1 M *
// * * X 1 0 0 1 2 M
// ...

// 有状态：同局多张截图，带跨步校验
const board = SaoleiBoard.init(firstScreenshot);   // 开局/新游戏必须走 init
board.updateFromScreenshot(nextScreenshot);        // 同局后续；校验尺寸+状态兼容
// 不同游戏的截图 → updateFromScreenshot 会抛 BoardStateIncompatibleError /
// BoardDimensionMismatchError，必须重新 SaoleiBoard.init()
```

### `SaoleiBoard` 的状态校验

`updateFromScreenshot` 采用**单调校验**（允许跨步，不要求逐帧），用于防止误操作
（如重启游戏后未清理状态），而非严格的游戏规则校验：

- 棋盘尺寸必须与 init 时一致；
- 已揭示的格子（数字 0-8 / 雷）是永久的——不能回退为未开/旗帜或变成别的数字；
- 未开/旗帜可变为任意状态（跨多少步都行）；
- `UNKNOWN`（识别不确定）宽容处理，不阻断合法更新。

### 文字棋盘符号

| 符号 | 含义 |
|---|---|
| `*` | 未开（INITIAL） |
| `0`-`8` | 已揭示数字 |
| `F` | 旗帜（FLAG） |
| `X` | 踩雷（HIT_MINE，触发的那颗雷） |
| `M` | 终局揭示的雷（MINE） |
| `?` | 识别不确定（UNKNOWN，校准用） |

## CLI

```bash
# 默认：打印文字棋盘
bazel run //projects/game/pkg/saolei-board:cli -- <screenshot.png>

# JSON GameState
bazel run //projects/game/pkg/saolei-board:cli -- <screenshot.png> --json

# 每格诊断（采样色、bevel、黑/红像素数、胜出参考色）——校准用
bazel run //projects/game/pkg/saolei-board:cli -- <screenshot.png> --debug

# 覆盖自动推算的棋盘尺寸
bazel run //projects/game/pkg/saolei-board:cli -- <screenshot.png> --width 9 --height 9
```

棋盘尺寸默认从截图尺寸自动推算（`cols = floor((W - 24) / 32)`）。

## 识别算法

每格 32×32 区域，按以下顺序判定（阈值均在 `ColorProfile` 中，默认面向经典 Win32）：

1. **bevel 检测**——只扫边框环的白色像素。未开格有白色高光带（凸起按钮外观），
   已开格纯灰无白色。限定边框环可避免雷图标中心的白色高光点被误判为 bevel。
2. **旗帜检测**——未开格中心区域有红色像素 → FLAG。
3. **雷检测（按黑像素计数优先）**——中心近黑像素数 ≥ 阈值即判为雷；再按边框
   红度区分 HIT_MINE（红底，踩雷）与 MINE（灰底，终局揭示）。先于数字投票，
   避免 HIT_MINE 的大片红底把雷"票"成数字 3。
4. **数字识别**——中心 glyph 像素按最近参考色投票（1蓝/2绿/3红/4深蓝/5深红/
   6青/7黑/8灰）。

数字参考色来自经典扫雷公开配色
（[社区来源](https://online.games.narkive.com/FUc9B1QB/colors-in-minesweeper)）；
社区主流做法参考
([BestHub Python bot](https://www.besthub.dev/articles/how-to-build-an-automated-minesweeper-bot-with-python-and-win32-api-d1d7ef54e731))。

## 校准与 Golden 测试

`testdata/` 存放真实桌面截图（`saolei_N.png`）及对应 golden 文本
（`saolei_N.golden.txt`，即经人工校准确认的 `renderBoardText` 输出）。
`src/core/golden.test.ts` 对每张截图识别后与 golden 比对，回归即失败。

调整 `src/core/classify.ts` 的 `DEFAULT_COLOR_PROFILE` 阈值后：

1. 用 `--debug` 对照截图确认每格诊断；
2. 重跑 CLI 覆盖 `.golden.txt`（`bazel run //projects/game/pkg/saolei-board:cli -- testdata/xxx.png > testdata/xxx.golden.txt`，注意保留 header 后的空行）；
3. `bazel test //projects/game/pkg/saolei-board:lib_test` 全绿。

## Bazel targets

| target | 用途 |
|---|---|
| `:core` | 核心库 ts_project（外部包引用） |
| `:pkg` | js_library，赋予包名供 npm 链接 |
| `:cli` | CLI js_binary |
| `:lib_test` | vitest 单测 + golden |

外部包（如 agent）引用：在 `package.json` 加 `"@dominion/game-saolei-board": "workspace:*"`，
BUILD 加 `:node_modules/@dominion/game-saolei-board` 到 deps。

## 坐标空间注意

本库的几何常量（`originYPx = 200`）是**截图空间**（含非客户区 chrome），
用于读像素。agent 的 `projects/game/agent/src/mcp/saolei/geometry.ts` 用的是
**客户端空间**（Y=104，减去 96px chrome 偏移），用于 `WM_*` 点击。两者 X=24、
cell=32 相同，但 Y 不同，勿混用。

## 依赖

- [pngjs](https://github.com/pngjs/pngjs)（纯 JS PNG 解码，无原生依赖）
