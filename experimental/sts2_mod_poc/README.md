# STS2 mod 方案真机 PoC 工具集

> 用途：执行 [survey/sts2-game-control.md](../../survey/sts2-game-control.md) §7 真机 PoC 清单的
> 驱动脚本、结算探针与 mod 自建补丁。PoC **已于 2026-10-22 执行完毕**，结论与版本矩阵见
> [survey/sts2-mod-poc/report.md](../../survey/sts2-mod-poc/report.md)（本目录只保留可复用的工具与构建产物）。

## 0. 关键事实（已核实）

- **mod 监听无法绑定 `0.0:0:0`**：[McpMod.cs](https://github.com/Gennadiyev/STS2MCP/blob/main/McpMod.cs)
  中 `HttpListener` 前缀硬编码为 `http://localhost:{port}/` 与 `http://127.0.0.1:{port}/`；
  `STS2_MCP.conf` 仅支持 `port` 字段（首次启动自动生成）。监听仅本机可达 →
  访问通道二选一（§1）。
- **release 与 main 分支存在版本差**：latest release v0.4.0（2026-05-05，README 标注
  tested against `v0.103.2`）；v0.107.x 适配在 main 分支（2026-07-29 commit）未发
  release；v0.111 兼容 [issue #131](https://github.com/Gennadiyev/STS2MCP/issues/131)
  开放中。装哪个 mod 来源取决于游戏机当前游戏版本（§3 决策树）。
- mod 安装形态：`STS2_MCP.dll` + `STS2_MCP.json`（复制到 `<game>/mods/`，平台无关
  .NET 程序集），游戏内 Settings → Mods 启用（首次有 consent 弹窗）。

## 1. 访问通道（dev 侧 → 游戏机 localhost:15526）

### 方式 A：SSH 隧道（推荐）

Windows 游戏机一次性设置（管理员 PowerShell）：

```powershell
# 安装并启动 OpenSSH Server（Windows 10 1809+ 内置）
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Start-Service sshd
Set-Service -Name sshd -StartupType Automatic
```

dev 侧建立隧道（保持会话）：

```bash
ssh -N -L 15526:localhost:15526 <user>@<windows-host>
curl -s http://localhost:15526/   # 应返回 {"message":"Hello from STS2 MCP v...","status":"ok"}
```

### 方式 B：netsh portproxy（备选，无需 SSH）

Windows 游戏机一次性设置（管理员 PowerShell）：

```powershell
netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=15526 connectaddress=127.0.0.1 connectport=15526
New-NetFirewallRule -DisplayName "STS2MCP PoC" -Direction Inbound -LocalPort 15526 -Protocol TCP -Action Allow
```

dev 侧直连 `http://<windows-host>:15526/`（脚本经 `STS2_POC_BASE` 环境变量适配）。

> 注意：portproxy 将无鉴权 API 暴露到 LAN，防火墙规则应收窄来源 IP；PoC 结束后
> `netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=15526` 清理。

## 2. PoC 步骤对照表

§7 清单项与执行方式的对应（脚本用法详见 `sts2_poc.py --help`）：

| # | PoC 项 | 执行方式 |
|---|---|---|
| 1 | 装游戏并钉版本 | 手工：记录版本号（Steam 游戏属性 → 更新；或主菜单 Settings），游戏属性 → Updates → **Only update this game when I launch it**；确认未参与 beta branch |
| 2 | 装 mod | 手工：按 §3 决策树取 DLL/JSON → `<game>/mods/`，游戏内启用，`sts2_poc.py handshake` 验证 |
| 3 | 状态读取采样 | 游戏内人工走到目标状态（菜单/战斗/地图/商店/事件），逐个 `sts2_poc.py sample --label <场景名>`（json+markdown 双格式留档） |
| 4 | 开局 | `sts2_poc.py start`（不带 seed）/ `sts2_poc.py start --seed <SEED>` |
| 5 | 战斗闭环 | `sts2_poc.py combat`（auto 出牌 → end_turn → 轮询敌回合，直至战斗结束；含 target 卡启发式处理） |
| 6 | 全状态面遍历 | 手工在游戏内推进 + `sample` 留档；动作经 `sts2_poc.py post`（万能动作口）或专用子命令 |
| 7 | 终局闭环 | `sts2_poc.py to-menu`（game_over → `menu_select{main_menu}`） |
| 8 | seed 复现 | `sts2_poc.py seed-check --seed <SEED>`（同 seed 两局：passive 速死 → 回菜单 → 二局，diff 首个稳定状态） |
| 9 | 干净档 | `sts2_poc.py profiles` / `profile-switch --id N` / `profile-delete --id N` |
| 10 | Instant Mode + 速度 | 手工开关游戏内 Instant Mode（mod 注入的设置项），`combat` 输出单场耗时对比 |
| 11 | 版本矩阵记录 | 填写 §4 模板（执行期曾以 `samples/` 留档，结论固化后已删除） |

## 3. mod 来源决策树（PoC 清单 #2 的前置裁定）

先记录游戏机当前游戏版本（PoC 项 #1），再按下表选 mod 来源：

| 游戏版本 | mod 来源 | 操作 |
|---|---|---|
| ≤ v0.103.x | [release v0.4.0](https://github.com/Gennadiyev/STS2MCP/releases/tag/0.4.0) | 下载 `STS2_MCP.dll` + `STS2_MCP.json` 复制入 `mods/` |
| v0.107.x | main 分支源码构建 | 游戏机装 .NET 9 SDK，clone 仓库后 `.\build.ps1 -GameDir "<game>"`，取 `out/STS2_MCP/*` 复制入 `mods/` |
| ≥ v0.111 | 无稳定支持 | 先用 main 分支构建实测并记录异常（[#131](https://github.com/Gennadiyev/STS2MCP/issues/131)：战斗状态读取/build 受 MP lobby API 变更影响）；若不可用，评估 Steam beta branch 是否提供旧版本钉住 |

游戏安装目录定位：Steam 库右键游戏 → Manage → Browse local files（Windows 默认
`C:\Program Files (x86)\Steam\steamapps\common\Slay the Spire 2\`）。

### 当前 mod 产物（权威）

**不维护 fork**（2026-10-22 需求方裁定）：直接使用已打包并经 PoC 验证的
[`mod/STS2_MCP.dll`](mod/STS2_MCP.dll)（sha256 `93de6572…`，= 上游 main@55e0648
+ card_in_play 补丁，即当前游戏机部署的那份）。部署方式：复制该 DLL 到
`<game>/mods/STS2_MCP.dll`（manifest `STS2_MCP.json` 沿用 [release v0.4.0](https://github.com/Gennadiyev/STS2MCP/releases/tag/0.4.0)
资产，内容未变）。

[`mod/0001-card-in-play.patch`](mod/0001-card-in-play.patch) 与
[`mod/build.sh`](mod/build.sh) **仅作记录**（补丁内容与构建方式的可追溯说明），
不作为维护路径——需要重新构建时再按需启用。

## 4. 版本矩阵

权威版本矩阵与结论见 [survey/sts2-mod-poc/report.md](../../survey/sts2-mod-poc/report.md) §1
（2026-10-22 执行：v0.107.1 × main@55e0648 自建 + card_in_play 补丁 = 全清单通过）。## 5. 采样留档

- 所有 GET/POST 请求响应自动存档至 `samples/`，文件名 `<序号>_<label>.<json|md>`
  （脚本维护单调递增序号），另附 `samples/index.jsonl` 逐条索引。
- PoC 结束后筛选有代表性的样本入库（全部状态 json+markdown 各一份 + seed-check
  对比 + 版本握手），作为 SDD `research.md` 的一手素材与 token 估算依据。

## 6. 驱动脚本

`sts2_poc.py`：Python 3 标准库实现，无第三方依赖；dev 侧执行，目标地址经
`STS2_POC_BASE`（默认 `http://localhost:15526`，配合隧道或 portproxy）。

```bash
python3 experimental/sts2_mod_poc/sts2_poc.py --help
python3 experimental/sts2_mod_poc/sts2_poc.py handshake
```

子命令速览：

| 子命令 | 用途 |
|---|---|
| `handshake` | GET `/` 验证握手并留档 mod 版本 |
| `state` / `sample` | 读单人状态（`--format json\|markdown`），`sample` 双格式留档 |
| `select --option X [--seed S]` | 原子 `menu_select` |
| `start [--seed S] [--character C]` | 菜单 → 单人入口 → 角色选择的开局封装 |
| `combat [--max-turns N] [--passive]` | 战斗闭环（auto 出牌/end_turn/轮询；`--passive` 只 end_turn 速死供 seed-check） |
| `to-menu` | game_over → 主菜单 |
| `seed-check --seed S` | 同 seed 两局确定性对比 |
| `profiles` / `profile-switch` / `profile-delete` | profile 档位管理 |
| `wiki --query Q` / `compendium` | 知识查询面 |
| `get PATH` / `post PATH JSON` | 万能探索口（非预设动作的兜底） |

## 7. 已知风险与应对（执行时关注）

- **overlay 状态卡死**：`state_type=overlay` 无动作面，需游戏内人工干预后继续
  （调研 R5）；每次遇到时记录触发路径到版本矩阵备注。
- **v0.111 兼容性**：战斗状态读取可能异常（issue #131）；实测记录，必要时走
  beta branch 钉旧版本。
- **`menu_select` 的 option 字符串**大小写不敏感但需与状态 `options` 对齐；脚本
  每步打印可用 options，失败时按实际 options 用 `select`/`post` 兜底。
- **开局 seed 仅在支持 seed 的入口生效**：标准单人 character select 传 seed 会
  返回错误且不开局（API 文档明确）；`start --seed` 失败时按返回 message 调整入口。

## 8. 引用清单

- 调研与 PoC 清单：[survey/sts2-game-control.md](../../survey/sts2-game-control.md)
- STS2MCP：<https://github.com/Gennadiyev/STS2MCP>（[raw-simplified.md](https://github.com/Gennadiyev/STS2MCP/blob/main/docs/raw-simplified.md)、
  [raw-full.md](https://github.com/Gennadiyev/STS2MCP/blob/main/docs/raw-full.md)、
  [releases](https://github.com/Gennadiyev/STS2MCP/releases)、
  [McpMod.cs 监听绑定](https://github.com/Gennadiyev/STS2MCP/blob/main/McpMod.cs)、
  [issue #131](https://github.com/Gennadiyev/STS2MCP/issues/131)）
