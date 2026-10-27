# Contract: `game-sts2` 大型测试 suite（sts2 最小接线验收）

> Feature: [spec.md](../spec.md)（FR-008/FR-009；SC-001/SC-003）| 决策：[research.md](../research.md) D8 | 规范：`style/large_test.md`、[specs/079-sts2-template-scaffold/contracts/scene-dispatch.md](../../079-sts2-template-scaffold/contracts/scene-dispatch.md) §1
> 本契约定义 sts2 最小接线的验收面：精简部署拓扑、suite 挂载、用例、fake-llm fixture 锚定与 helper 参数化。执行经 testplan skill（`guitar run projects/game/testplan/system_test.yaml`）完成部署→测试→清理闭环，全部用例通过为验收（宪法 VI）。

## §1 部署拓扑（`projects/game/testplan/deploy_sts2.yaml` 新建）

| 服务 | 说明 |
|---|---|
| `mongo`（infra） | preset-authoring 存储（`game_agent_v2.presets`） |
| `session` | `/api/v1` session CRUD（sts2 数据面 079 已开放） |
| `fake-llm` | LLM 替换端点（Responses 协议） |
| `proxy` | 会话面 owner 亲和路由 |
| `agent-v2-test` | 被测服务；`GLM_LLM_TARGET: dominion:///game/fake-llm:8080`（+ opencode 同型 env），零 secret |
| `gateway` | `game.liukexin.com` + `/api/v1/`、`/api/v2/` PathPrefix 路由 |

省略（相对 `deploy_agent_v2.yaml`）：`web`（本 feature web 零改动）、`fake-desktop`（无 desktop flow）、`memory`（无 memory 挂载；无 memory 可启动已由 `deploy_agent_v2_memory_down.yaml` 证明）。`name: game.{{run}}`、`type: test` 顶层形态不变。

## §2 suite 挂载（`projects/game/testplan/system_test.yaml`）

`suites:` 末尾追加（既有 suite 顺序不变——主 suite 先跑、窄分支在后）；同步更新文件顶部 suite 描述（四 suite → 五 suite）、`projects/game/testplan/README.md` §2 的 suite 表/拓扑说明与 §5 的运行说明（四 → 五 suite），以及 §6 第 3 条（常量约定；补"suite 专用期望常量可与该 suite 的用例文件同文件"）：

```yaml
- name: game-sts2
  description: "The sts2 minimal single-main-agent conversation face on the slim fake-llm topology: preset(main, empty persona) → UpdateTeam → Send NDJSON turns → quiescence / refresh / cancel; no desktop, no memory, no announcer."
  deploy: //projects/game/testplan/deploy_sts2.yaml
  endpoint:
    http:
      public: https://game.liukexin.com
  cases:
    - //projects/game/testplan:sts2_conversation_test
```

## §3 用例（`projects/game/testplan/sts2_conversation_test.go`，新 `go_largetest` target）

沿用既有 helper 惯例（`agentV2TeamName`/`startTeamSend`/`drainTeamStream`/`groupTeamMemberTurns`/`assertTeamStreamWellFormed`/`assertTeamStreamMessagesMatchList`，`agent_v2_helpers_test.go`）；资源名 `uniqueSuffix()` 隔离；`traceContext` 贯穿。

| # | 用例 | Given / When / Then |
|---|---|---|
| 1 | `TestSts2MainMaterializeAndRoster` | G：sts2 模板；W：创建 role=`main` preset + `UpdateTeam`（单成员）；T：`GetTeam.members` 恰 `main` 一项（无 announcer、name/role/preset/model 回显）、`active_member = "main"`；物化前 `GetTeam` NOT_FOUND |
| 2 | `TestSts2MainConversationTurn` | G：已物化；W：`Send`（fixture 触发词）；T：NDJSON 流恰 1 个 `main` turn（`turn_start`→`block_*`→`turn_end{COMPLETED}`），文本匹配 fixture；`team_message{member="user"}` 固化；`ListTeamMessages`/`ListMemberMessages("main")` 与流投影一致（`assertTeamStreamMessagesMatchList`） |
| 3 | `TestSts2MainQuiescenceAndRefresh` | G：已物化并完成一回合；W：流静止后再次 `UpdateTeam`；T：静止期无自发帧；刷新后 `create_time` 保留、`update_time` 更新、历史清空（List 面）、`GetTeam` 新名册；在途刷新变体：turn 在途时刷新 → `turn_end{ABORTED}`；取消变体：`:cancel` → `turn_end{CANCELED}` → 冻结（再次 Send 恢复） |
| 4 | `TestSts2NonRosterMemberNotFound` | G：已物化；W：`GetTeamMember("announcer")` 与任一非 `main` 角色、`ListMemberMessages` 同；T：`NOT_FOUND`（FR-007 名册固定） |

校验类负向（并入用例 1）：roster 双成员 / 非 `main` role / 跨模板 preset（结构层 `template` 不匹配拒绝）→ `INVALID_ARGUMENT`。同模板 preset role 不匹配经公共 API 不可达（sts2 池 preset create 仅接受 `main` 角色，`projects/game/agent_v2/src/server.ts` 的场景角色词汇校验），该等值分支由 `projects/game/agent_v2/src/session.test.ts` 单测覆盖（"rejects roster and preset violations with INVALID_ARGUMENT before constructing the orchestration" 的 sts2 单成员 role 等值断言，以及 "rejects a preset role mismatch and an unknown preset before any member creation"）。

## §4 fake-llm fixture（`projects/game/fake-llm/service/testdata/sts2_main.yaml` 新建）

- 每条目：`responses_only: true` + `system_keywords: ["sts2 主 agent"]`（锚 cordis.yml main 池模板 persona 身份行——fake-llm 无 session/模板维度，persona 锚定是唯一作用域手段）+ 触发词 `sts2-ping`（**新增 1 条消息模板，总数 41→42**；已核对不与既有触发词（含 `hello`）构成子串包含）。**用例创建 preset 时 MUST 传空 persona**——空 persona 保留池模板 prefix base；非空 persona 会覆盖 prefix 使锚串消失（`common/js/dsh-plugins/preset-authoring/src/derive.ts`），fixture 将静默失配。匹配语义以 `specs/012-fake-llm-service/spec.md` FR-006/FR-007 为准（keywords 子串命中、多命中取 name 字典序最小）；`system_keywords`/多轮条件语义见 `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2/§3；模板 schema 见 `specs/046-fake-llm-think-chunking/contracts/template-config.md`。
- 期望输出钉为 `projects/game/testplan/sts2_conversation_test.go` 的包级常量（T014 建常量骨架，T016 在同一文件补用例；不分散到 fixture 头注释）。
- **嵌入**：`projects/game/fake-llm/service/BUILD.bazel` 的 `service.embedsrcs` MUST 新增 `testdata/sts2_main.yaml`——`//go:embed testdata/*` 只嵌入已声明文件，漏列会使 fixture 不进入二进制。
- **lockstep**：`projects/game/fake-llm/service/message_store_test.go` 嵌入断言同步（testdata 三方锁惯例，`projects/game/testplan/README.md`）——含总数（41→42）与位置索引断言的重钉。

## §5 helper 参数化（`agent_v2_helpers_test.go` 演进，saolei 零语义变化）

- `agentV2SessionName`/`ensureAgentV2Session`/preset 创建族（`postAgentV2PresetCreate` 等）增加 `templateID` 参数；saolei 既有调用点显式传 `saoleiTemplateID`（编译期全量更新，行为不变）。
- 新增薄包装 `updateSts2MainTeam`（内部复用 `updateAgentV2TeamMembers` 传单成员 `main`）——不改动 saolei 双成员默认路径。

## §6 验收执行

1. saolei 零回归：既有四 suite（game-system/game-disconnect/game-memory-down/game-stall）全部用例通过（SC-001）。
2. 新 suite：`game-sts2` 全部用例通过（SC-003）。
3. 执行方式：testplan skill `guitar run projects/game/testplan/system_test.yaml --timeout=20m`（五 suite 全链路预算；若整体超时则按 suite 单独执行）；每 suite 独立 run id + 部署 + 清理；任一 failed/flaky = 验收未通过，修复后重跑至全绿。
