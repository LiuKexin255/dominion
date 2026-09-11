# Contract: 提示词三层所有权（玩法 section / 工具守则 / persona）

> saolei 场景提示词的所有权分层与内容边界。权威玩法来源：https://en.wikipedia.org/wiki/Microsoft_Minesweeper 、https://en.wikipedia.org/wiki/Minesweeper_(video_game) （旧版经典扫雷 / Win98 时代形态）。
> 决策依据：[research.md](../research.md) R9。

## 1. `saolei:game` section（新；saolei-loop 所有，全员生效）

- **注册**：`@dominion/dsh-saolei-loop` 的 `apply(ctx)` 经 `ctx.systemPrompt.section({ name: 'saolei:game', order: 50, text: SAOLEI_GAME_RULES })`——host 组合行 boot 时全局注册，对全部成员（player/planner）装配可见（静态全员事实，无需按成员注册）。
- **order 50**：persona(0) 与 team section(1-49) 之后、工具守则(100-199) 之前。
- **内容 = 玩法 + 可用操作**（两者对全体成员同源同文）：
  - **玩法**（依据权威来源表述）：棋盘为被隐藏的雷区网格；目标 = 揭示全部非雷格而不踩雷；已揭示格显示数字 1-8（八邻雷数）或空白（0，揭示时级联展开相邻非雷区）；可对未揭示格标旗（推理标记，不改变格内容）；对已揭示数字格，当其相邻旗数满足该数字时可"chord"（同击）展开其余未标记邻格；踩中雷即负（负局展示全部雷位）；全部非雷格揭示即胜；剩余雷数计数 = 总雷数 − 已标旗数（可为负 = 过度标旗）。
  - **可用操作**（与 saolei 插件工具能力**取交集**）：开局/重开（对应 `saolei_init`：新局、重开重播种）、格子操作 click（揭示）/flag（标旗/取消标旗）/chord（同击）（对应 `saolei_operate`，支持单发与有序批量）、只读剩余雷数查询（对应 `saolei_remain`：每数字格的"数字 − 相邻旗数"视图）。**不声明未实现操作**（如 Win98 的 `?` 问号标记、计时器语义）；不重复工具调用形态/结果格式细节（归工具守则）。
- **内容边界（MUST NOT）**：不含工具参数 schema、结果文本三层结构、坐标/符号读法（工具守则域）；不含成员身份/职责（persona 域）；不含团队协作规则（team section 域）。

## 2. `saolei:guidance` 收缩（saolei 插件，仅 player 经模板行挂载）

保留（工具用法域）：
- 符号表（`*`/`0-8`/`F`/`X`/`M`/`?` —— 工具结果棋盘文本的读法）；
- 坐标标尺与 `(x, y)` 参数对应关系（0 基、左上原点）；
- 工具结果三层结构（outcome 行 / game-status 行 / 棋盘文本）与各 outcome 词汇；
- 三工具的调用形态（无参/单发/批量互斥规则、参数完整性要求、空批量 no-op）；
- 校验与拒绝语义分级（no-op 跳过 vs 结构性停批、非法参数组合拒绝文本、识别失效后的 `no_active_game` 恢复路径）；
- 示例流（调用序列形态）与工具使用纪律（不读像素、chord 不以两次 click 代替）。

移除（玩法域，迁至 `saolei:game`）：
- 数字含义/级联展开作为游戏规则的解释；
- flag 的游戏意义（"推理标记"）、chord 的展开条件作为规则的解释；
- 胜负判定语义（won/lost 的棋盘事实定义）作为规则的解释、`X`/`M` 的游戏含义叙述。

切分判据：**"怎么调用、返回什么形状、拒绝时怎么办" 留守则；"规则是什么、这个操作在游戏里意味着什么" 进玩法 section**（符号表条目保留读法定义、剔除游戏含义叙述）。

## 3. preset 模板 persona 去重

- player 模板 persona：保留身份（扫雷 player）、职责（操作桌面扫雷窗口完成对局、独占桌面控制）、风格与纪律（以工具返回的棋盘事实为准、落子前核对、冷静精确、是否开新局自行判断）；**移除操作清单描述**（"调用 saolei 工具开新局、点击/标记/双击揭示格子并查询剩余雷数"——由 `saolei:game` 承载）。
- planner 模板 persona：不变（本无玩法/操作描述；复盘/策略/记忆职责保留）。

## 4. 组装形态（终态示例顺序）

```
[0]   persona（preset 行；身份/职责/风格）
[1-49] team section（team 插件；目标/名册/广播格式约定——单一 XML 标注形态）
[50]  saolei:game（saolei-loop host 行；玩法 + 可用操作 —— 全员）
[100] saolei:guidance（saolei preset 行 —— 仅 player）
[200+] memory 快照（memory preset 行 —— 仅 planner）
```

## 5. 联动

- fake-llm 夹具 `system_keywords`：若现锚定文案被移动（player 夹具锚定 persona/guidance 关键词），随分层同步调整锚点（`projects/game/fake-llm/service/testdata/team_player.yaml`/`team_planner.yaml`）；planner 夹具可新增 `saolei:game` 关键词锚定（玩法 section 到达 planner 的回归断言）。
- 大型测试：断言 planner system prompt 含玩法 section、player 两者兼有且 guidance 无玩法关键词（quickstart.md §V5）。
