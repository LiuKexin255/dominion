# STS2 mod 方案真机 PoC 执行记录

> 执行日期：2026-10-22 | 执行人：agent（dev 侧驱动）+ 人工（游戏机操作）
> 目标与清单：[survey/sts2-game-control.md](sts2-game-control.md) §7
> 工具：[experimental/sts2_mod_poc/](../experimental/sts2_mod_poc/README.md)（驱动脚本/探针/mod 补丁与构建脚本）。执行期采样留档（`samples/`，500+ 文件）
> 已于结论固化后按需求方要求删除；行为证据以本文表格为准。

## 1. 版本矩阵（清单 #11）

| 日期 | 游戏版本 | mod 来源/版本 | 访问通道 | 结果 | 备注 |
|---|---|---|---|---|---|
| 2026-10-22 | STS2 **v0.107.1**（Steam Windows，中文客户端） | release v0.4.0 | SSH 隧道（`ssh -N -L 15526:localhost:15526`） | 菜单/地图/开局/事件/商店等**非战斗面全部正常**；**战斗状态读取 500** | `MissingMethodException: CombatManager.get_IsPlayPhase()`，v0.107 起游戏 API 变更 |
| 2026-10-22 | v0.107.1 | **main@55e0648 源码自建**（Linux 构建） | 同上 | **全清单通过** | 0 警告 0 错误；构建方式见 §4 |
| 2026-10-22 | v0.107.1 | main@55e0648 **+ card_in_play 补丁**（sha256 `93de6572…`） | 同上 | 通过；出牌动画堆积问题解决 | 补丁内容见 §4.2 |

版本策略结论：**锁 main@55e0648 自建 DLL + 游戏钉 v0.107.1（Steam 仅启动时更新）**；升级游戏前必须先核对 STS2MCP 上游适配状态（v0.111 兼容 [issue #131](https://github.com/Gennadiyev/STS2MCP/issues/131) 开放中）。

## 2. 清单项执行结果

| # | 项 | 结果 | 关键证据（执行期采样，已删除；结论以结果列为准） |
|---|---|---|---|
| 1 | 装游戏钉版本 | ✅ v0.107.1 | — |
| 2 | 装 mod + 握手 | ✅（经 §1 版本矩阵两次迭代） | `001_handshake.txt` |
| 3 | 状态读取采样 | ✅ **16 种状态** json+markdown 双格式：menu（main/singleplayer 子菜单/character_select/tutorial_prompt/profile_select）、map、monster/elite、rewards、card_reward、event×2、shop、card_select(upgrade)、rest_site、treasure、game_over、timeline | `*_md.txt` 全集 |
| 4 | 开局（不带 seed） | ✅ 全流程：main→singleplayer→[子菜单]→角色→confirm→tutorial_prompt(拒)→run | `007/010/013/016_*` |
| 4 | 开局（带 seed） | ⛔ **SKIP**：custom/daily 模式未解锁（新档案）；standard 入口 `charSelect.Lobby == null`，无 Lobby 对象可 `SetSeed`，属**游戏结构限制**，fork 补丁无法放开 | `546_sp_entry.json`（子菜单锁定态） |
| 5 | 战斗闭环 | ✅ auto 出牌→end_turn→敌回合轮询，5 场胜 + 1 场被动死亡；发现并修复出牌动画堆积（§3.2） | `537_combat_state.json` |
| 6 | 全状态面遍历 | ✅ 奖励领取/卡牌奖励选卡与跳过/地图节点/事件选项/篝火休息/商店购买/卡牌升级选择/宝箱领取/**卡包包选择（Neow 卷轴箱）** | `116/237/285/401/499/563_*` |
| 7 | 终局闭环 | ✅ 被动死亡→game_over→`menu_select{main_menu}`→主菜单（顺带覆盖 timeline 自动弹出） | `*_game_over*`、`*_timeline*` |
| 8 | seed 复现 | ⛔ SKIP（同 #4 seed 项） | — |
| 9 | 干净档 | ✅ 只读列表/真实存档路径证实 modded 隔离；**switch 为两段式**（首调开屏、再调才选档）；delete 空档位成功 | `*_profiles*`、`*_profile_*` |
| 10 | Instant Mode | ✅ **操作模型变化**：出牌后零等待首读即完全结算（5/5 次，`card_in_play` 恒 False，8–65ms）；end_turn 敌回合 30ms；轮询循环不再需要。单场战斗 5s（基线 54–58s） | `560_*` 探针数据 |
| 11 | 版本矩阵 | ✅ 见 §1 | — |

## 3. 问题与发现（SDD 关键输入）

### 3.1 阻塞级（已解决）

**release v0.4.0 与 v0.107.1 战斗态不兼容**：与 [issue #131](https://github.com/Gennadiyev/STS2MCP/issues/131) 症状一致但该 issue 报的是 v0.111；实测 v0.107 已移除 `CombatManager.IsPlayPhase`。v0.4.0 DLL 能**加载**（v0.107 尚存 `LobbyPlayer` 类型，v0.111 才移除），表现为"非战斗面正常、战斗态 500"。**修复 = main@55e0648 自建**（`BuildBattleState` 已改用 `combatState.CurrentSide`）。

### 3.2 缺陷级（fork 补丁已修 / 上游待报）

**出牌动画堆积**：`play_card` 经 `RunManager.ActionQueueSynchronizer.RequestEnqueue(new PlayCardAction(...))` 入队（与游戏 UI 同路径），连续出牌时动画队列堆积（画面同卡多次出现）；游戏侧有现成结算信号 `NPlayerHand.InCardPlay`（游戏自身 end_turn 守卫 `NEndTurnButton.cs:114-123` 使用），但 mod 未暴露。**补丁**：`BuildBattleState` 增加 `battle.card_in_play`（5 行 C#，§4.2），客户端出牌后轮询该字段归 False 再出下一张，问题消除。

**event.body 为空**：两个事件（BYRDONIS_NEST/SAPPHIRE_SEED）的 `body` 字段均空串，事件描述只有选项级 `description`——agent 侧需容忍。

### 3.3 行为细节（客户端工具面必须处理）

| 行为 | 说明 |
|---|---|
| `claim_reward` 索引重排 | 领取一项后剩余奖励索引前移（领金币后卡牌奖励从 index 1 → 0） |
| `play_card` 手牌竞态 | 出牌请求飞行期间手牌变化 → `card_index out of range`；需容错重查（已实现）+ `card_in_play` 等待 |
| 战斗结束竞态 | 我方出牌致敌全灭后再发 `end_turn` 报 `Not in combat`，属正常竞态需容忍 |
| `profile-switch` 两段式 | 首调仅打开档位屏（返回提示语），再调才真正切换 |
| 菜单选项随进度增长 | 首局前主菜单无 compendium/timeline；完成首局后出现 |
| singleplayer 子菜单时隐时现 | 首次开局观察直达 character_select；后续出现 standard/daily/custom 子菜单（daily/custom 锁定） |
| 中文客户端 | option 标签 mod 归一化为英文（IRONCLAD 等），`name`/`description` 值为中文 |
| 宝箱两段读取 | treasure 首查为 "Opening chest..." 过渡态，再查才有 relics |
| game_over → main_menu 后 timeline 可能自动弹出 | 返回主菜单后首查可能是 timeline 屏（epoch 解锁触发） |

### 3.4 状态体积实测（token 估算依据）

| 状态 | markdown | JSON |
|---|---|---|
| menu（主菜单） | 125 B | — |
| combat（单敌） | ~1.3 KB | 6.4–7.3 KB |
| map（含全 DAG） | 0.7–2.6 KB | 最大 15.4 KB |
| shop | 2.1 KB | 7.2 KB |
| card_select | 1.5 KB | — |

markdown 格式比 JSON 紧凑 4–6 倍；agent 工具面**状态读取应默认 markdown**（调研 §4.1 的 7–8M tokens/局 基线与此吻合）。

### 3.5 Instant Mode 对操作模型的影响（清单 #10 核心结论）

Instant Mode 开启后，**act→read 合并为一步**（探针实测，[settle_probe.py](../experimental/sts2_mod_poc/settle_probe.py)）：

- 出牌 POST 返回后**零等待首读**即为完全结算态：手牌扣减✓ 能量扣减✓ `card_in_play=False`（8–65ms，5/5 次）
- `end_turn` 敌回合 30ms 内结算，一次读取即达下一玩家阶段或战斗结束
- 客户端轮询循环（`wait_card_settled`）不再触发，仅作为 Instant Mode 关闭时的回退保留

残留限制：动作 POST 响应仅 `{status, message}` 不含状态，严格"一步"（动作响应内嵌结算后状态）需 fork 补丁。**已裁定（2026-10-22 需求方）**：raw API 不直接对 agent 开放，由 dsh tools 封装提供；act→read 合并等优化放在 tools 层实现，mod 保持最小 fork（main@55e0648 + card_in_play），降低上游升级维护成本。

## 4. fork 补丁记录（模式① 全链路验证）

### 4.1 main@55e0648 自建（Linux）

```bash
# 引用 DLL（3 个，从游戏机 data_sts2_windows_x86_64/ 拷贝）
#   sts2.dll / GodotSharp.dll / 0Harmony.dll
dotnet build STS2_MCP.csproj -c Release -o out/STS2_MCP \
  -p:STS2GameDir=<游戏目录> -p:STS2GameDataDir=<游戏目录>/data_sts2_windows_x86_64
```

- .NET 9 SDK 用户目录安装即可（PoC 用 /tmp 沙箱，9.0.319）
- 构建产物平台无关，scp 回游戏机 `mods/` 生效
- csproj 引用仅 3 个 DLL（`Private=false` 纯编译引用）

### 4.2 card_in_play 补丁

`McpMod.StateBuilder.cs` `BuildBattleState` 内新增（暴露游戏自身 end_turn 守卫信号）：

```csharp
var uiHand = NCombatRoom.Instance?.Ui?.Hand;
battle["card_in_play"] = uiHand != null && uiHand.InCardPlay;
```

客户端配套：出牌成功后轮询 `battle.card_in_play` 归 False（+0.3s 静默）再出下一张；字段缺失时退化为旧行为（向后兼容）。

**dev 构思→游戏机生效闭环 ≈ 5 分钟**（改码 1min + 构建 4s + scp + 重启游戏）——调研 §3.3 模式①成本判断获实测支撑。

## 5. SDD 输入摘要

1. **接入架构**：desktop 桥 HTTP 转发 kind 可行性获实证（全部操作经 REST 完成，无键鼠依赖；唯一 overlay 未遇）。
2. **raw API 不对 agent 直开（需求方裁定）**：以 `@dominion/dsh-sts2` 工具面封装；act→read 合并、状态裁剪等优化归 tools 层；mod 保持最小 fork。
3. **wait 原语**：`card_in_play`（动作结算信号）已入 mod；Instant Mode 开启后轮询退化为不触发，tools 层保留其等待逻辑作为关闭时的回退。
4. **fake-sts2 设计输入**：按 raw-full.md 状态机建模 + §3.3 行为细节表（重排/竞态/两段式）作为仿真正确性要求。
5. **工具面默认值**：状态读取 markdown 格式（体积为 JSON 的 1/4–1/6）；Instant Mode 纳入部署前置检查项。
6. **版本策略**：游戏钉 v0.107.1 + mod 使用已打包 DLL（`experimental/sts2_mod_poc/mod/STS2_MCP.dll`，sha256 `93de6572…`，已固化入仓可随时重部署）；**fork 暂不维护**（2026-10-22 需求方裁定），[补丁](../experimental/sts2_mod_poc/mod/0001-card-in-play.patch)与[构建脚本](../experimental/sts2_mod_poc/mod/build.sh)仅作记录；升级 = 先看上游 issue 再动。
7. **seed 复现未验证**：需先解锁 custom 模式（打穿一局或查解锁条件），SDD 阶段如需确定性开局要补此项。
8. **知识面分层（实测修正）**：对局内决策**不依赖 wiki**——card_reward/shop/hand 状态自带所涉卡牌的完整规则文本；wiki/compendium 端点按 profile **已发现**内容过滤（端点设计策略，非数据不可得——mod 在 character_select 已读取 `CardPool.AllCards`/`RelicPool.AllRelics` 全池对象；解锁与发现同义，需求方确认）。若需全目录先验知识（跨局规划），走小 fork 端点暴露全池文本即可（代码路径现成），**无需引入外部数据库**（Spire Codex 的 PolyForm 许可风险因此可完全规避）。

## 6. 引用清单

- 调研：[survey/sts2-game-control.md](sts2-game-control.md)
- STS2MCP：[main@55e0648](https://github.com/Gennadiyev/STS2MCP/tree/main)、[raw-full.md](https://github.com/Gennadiyev/STS2MCP/blob/main/docs/raw-full.md)、[issue #131](https://github.com/Gennadiyev/STS2MCP/issues/131)
- 游戏侧守卫引用：`NEndTurnButton.cs:114-123`（经 [McpMod.Actions.cs](https://github.com/Gennadiyev/STS2MCP/blob/main/McpMod.Actions.cs) `ExecuteEndTurn` 转引）
