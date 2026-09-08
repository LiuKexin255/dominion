# Directive 2026-09-01: 用户修改意见 1/2/3/4 的补充设计

**Feature**: [spec.md](../spec.md) | **日期**: 2026-09-01（意见 2 处置终态于 2026-09-02 追加，§8） | **性质**: Phase 3 实现返工的补充设计（设计任务，不含代码变更）

**输入**: 用户对 051 Phase 3（分支 `051-agent-v2-dsh-migration`，实现已完成未提交）的修改意见 1/3/4；意见 2（部署窗口 503）最初由他人并行调查，其**处置终态**已由用户上游修复落定并追加于本文 §8。

**状态**: 本文是各意见的落地方案权威描述；与之冲突的既有设计文档章节按 §5 修订清单同步（终态表述，`.specify/memory/constitution.md` 原则 VII）。

---

## 0. 意见原文与问题定位

| # | 意见原文 | 现状问题（已核实） |
|---|---|---|
| 1 | deploy 不能配置两个相同名称的 service；fake-desktop won/drop 的区别只在环境变量，不应改名单部署两个相同服务，应拆成两个 suite 搭配不同 deploy 配置 | `projects/game/testplan/deploy_agent_v2.yaml` 以**两份 manifest**（`projects/game/fake-desktop/service.yaml` name=fake-desktop-won + `service-drop.yaml` name=fake-desktop-drop）部署同一代码两份，且 `projects/game/fake-desktop/BUILD.bazel` 为此维护**两对 pkg/image**（`service_pkg`/`cmd_image_won` + `service_pkg_drop`/`cmd_image_drop`），差异仅 env（`FAKE_DESKTOP_SCENARIO` 等）；引用同一 deploy 的 `agent-v2-conversation` 等套件也被动部署两个不需要的实例 |
| 3 | proxy agent.go 还需要手写 `parseAgentResourceName` 吗？应该用 codegen 生成的解析方法；v2 proto 没有 resource 注解，codegen 无法生成解析代码 | **用户判断完全正确**。`projects/game/agent_v2.proto` 仅有 `google.api.field_behavior`/`google.api.http` 注解，**无任何 `google.api.resource`/`resource_reference` 注解**（对照 v1 `projects/game/game.proto` 22 处）；proxy 手写 `parseAgentResourceName`（`projects/game/proxy/handler/agent.go:441`）与 `parsePresetName`（`:466`，注释自认 "the preset resource has no proto resource annotation, so the pattern is parsed here"） |
| 4 | preset 是无状态代码，可不经 proxy；拆分 preset 和 agent service；preset service 直接连接 gateway；移除 proxy 的 preset 代码 | preset CRUD 五方法与 `ListModels` 都定义在 `AgentService`，经 proxy 的无亲和稳定哈希路径（`agent.go` `affinityFreeConn`）转发；preset 状态在 Mongo（research D2 已论证多实例一致性），模型目录为静态配置——两者均无 session 亲和诉求，proxy 转发是为 049 D4 "agent-v2 整体必须经 proxy" 约束强加的 |
| 2 | （原）proxy→agent-v2 部署窗口 503 需要排查/测试侧重试 | **已由用户上游修复处置**（052 探针 + 053 JS bootstrap + guitar postDeploySettle；测试侧重试已移除）——处置终态与 agent_v2 的前置迁移 T014b 见 **§8** |

---

## 1. 意见 1 — fake-desktop 单服务化 + suite/deploy 重组

### 1.1 设计原则

一个被测服务 = 一份 manifest + 一对 pkg/image；行为变体（env 差异）由**不同 deploy 文件 + 不同 suite** 表达——与 v1 先例同构：`agent-stall` 套件用独立 `deploy_agent_stall.yaml` 选择 `agent_timeouts` config 块（`projects/game/testplan/system_test.yaml:215-222`），而非为 stall 行为复制 agent 服务 manifest。

### 1.2 单服务终态

- `projects/game/fake-desktop/service.yaml`：`name: fake-desktop`、`desc` 去掉 per-instance 语义、单一 artifact `{name: fake-desktop, target: :cmd_image}`；删除文件内"每 deploy 实例一份 manifest"的注释（其描述的双 manifest 形态即本次被否决的形态）。
- `projects/game/fake-desktop/BUILD.bazel`：单一 `service_pkg`（`service = "fake-desktop"`）+ `cmd_image`；删除 `service_pkg_drop`/`cmd_image_drop`。
- 删除 `projects/game/fake-desktop/service-drop.yaml`。
- 行为差异 100% 由 env 驱动（`projects/game/fake-desktop/cmd/main.go:35-55`：`FAKE_DESKTOP_SESSION`/`FAKE_DESKTOP_SCENARIO`/`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS`），服务代码零改动。

### 1.3 deploy 拆分

两份 deploy 文件、服务清单相同、仅 fake-desktop env 不同：

| 文件 | fake-desktop env | 服务的套件 |
|---|---|---|
| `projects/game/testplan/deploy_agent_v2.yaml`（既有，修剪） | `FAKE_DESKTOP_SESSION: desktop-e2e-won`、`FAKE_DESKTOP_SCENARIO: won` | `agent-v2-conversation`、`agent-v2-game`、`desktop-flow`（及 Phase 4 `agent-v2-preset`、Phase 7 迁入的 `session`/`memory`） |
| `projects/game/testplan/deploy_agent_v2_drop.yaml`（新） | `FAKE_DESKTOP_SESSION: desktop-e2e-drop`、`FAKE_DESKTOP_SCENARIO: progressive`、`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS: "3"` | `agent-v2-game-disconnect`（新） |

两 deploy 的 `name: game.{{run}}` 模式不变；guitar 逐 suite 部署→测试→清理、串行执行（`tools/test/guitar/pkg/run/run.go`），两种拓扑不并存。

### 1.4 测试 binary 拆分（必须）

`guitar` 的 suite 以 bazel target 为 case 粒度（`tools/test/guitar/pkg/config/config.go` `Cases []string`；`run.go:183-194` 直接 `bazel test --config=largetest <cases...>`），**无 per-suite 测试函数过滤**。若 disconnect 套件复用 `agent_v2_game_test` binary，won 链等用例会在 drop 拓扑（无 won 实例）上失败。因此：

- `TestAgentV2GameDisconnectMidGameThenRecover` 从 `projects/game/testplan/agent_v2_game_test.go` 移入新文件 `projects/game/testplan/agent_v2_game_disconnect_test.go`（含其依赖的 `gameFlowReconnectWait` 常量与注释；仅关注点=mid-game 断连分支，符合 `style/large_test.md` §测试组织"按被测模块/关注点拆分"）。
- `projects/game/testplan/BUILD.bazel`：新增 `go_largetest` target `agent_v2_game_disconnect_test`（srcs = `agent_v2_game_disconnect_test.go` + 共享 helpers 四件套，deps/embedsrcs 镜像 `agent_v2_game_test`，size=medium——断连用例等待重连周期）；`agent_v2_game_test` target 的 srcs 不变（`agent_v2_game_test.go` 文件本身留下其余四用例）。目录内已有 gazelle 默认名 target（`testplan_test`），多 target 共存满足 `style/large_test.md` 的默认名规则。
- `agent_v2_helpers_test.go` 的 `agentV2DesktopWonSessionID`（"desktop-e2e-won"）与 `agentV2DesktopDropID`（"desktop-e2e-drop"）常量保留——各套件按自己的 deploy env 使用对应常量。

### 1.5 suite 重组映射表（现有 9 个用例 → 目标 suite/deploy）

| 用例（`*_test.go` 函数） | fake-desktop env 依赖 | 目标 suite / deploy |
|---|---|---|
| `TestAgentV2GameWonChainToolStream` | won 实例（session `desktop-e2e-won` 已连接） | `agent-v2-game` / `deploy_agent_v2.yaml` |
| `TestAgentV2GameDesktopAbsent` | 无（无连接分支） | `agent-v2-game` / `deploy_agent_v2.yaml` |
| `TestAgentV2GameDisconnectMidGameThenRecover` | **drop**（progressive + 3-op 断连 + 重连） | `agent-v2-game-disconnect`（新）/ `deploy_agent_v2_drop.yaml` |
| `TestAgentV2GameMultiSessionIsolation` | won 实例 | `agent-v2-game` / `deploy_agent_v2.yaml` |
| `TestAgentV2GameConversationStreamIndependentOfFlow` | 无（测试自驱 flow 连接） | `agent-v2-game` / `deploy_agent_v2.yaml` |
| `TestDesktopFlowProbeEcho` | 无（测试自驱） | `desktop-flow` / `deploy_agent_v2.yaml` |
| `TestDesktopFlowSecondConnectionTakesOver` | 无（测试自驱） | `desktop-flow` / `deploy_agent_v2.yaml` |
| `TestDesktopFlowOperationDeliveryAndReceipt` | 无（测试自驱） | `desktop-flow` / `deploy_agent_v2.yaml` |
| `TestDesktopFlowDisconnectFailsInFlight` | 无（测试自驱） | `desktop-flow` / `deploy_agent_v2.yaml` |

> `desktop-flow` 的 4 个用例全部由测试自身扮演 desktop 客户端（`projects/game/testplan/desktop_flow_test.go` 逐函数核实），对 deploy 中 fake-desktop 实例无依赖——该套件维持引用基础 deploy 即可。

### 1.6 system_test.yaml 处置

- `agent-v2-game` 套件 description 改写：移除 "fake-desktop executors"（复数双实例）与 drop 分支句，指向 won 拓扑的用例清单；case 不变（`agent_v2_game_test`）。
- 新增 `agent-v2-game-disconnect` 套件：deploy=`//projects/game/testplan/deploy_agent_v2_drop.yaml`，cases=`//projects/game/testplan:agent_v2_game_disconnect_test`，description 按 `style/large_test.md` 惯例写明关注点（mid-game 断连三局序列：断连不可见 → 断连后 init FAILED 回合存活 → 重连后完整重播，US1 场景 5/6）与拓扑差异（fake-desktop 以 progressive + disconnect-after-ops env 部署）。
- 顶部 description 块（Suite 12 等）如提及双 fake-desktop 实例处同步修剪。

### 1.7 与 `style/large_test.md` 反模式清单的核对

- **反模式 4（平行测试计划）**：不新建测试计划 YAML——两份 deploy 文件都在**既有** `system_test.yaml` 内以 suite 组织，"每个被测系统只维护一份测试计划"维持；`deploy_agent_v2_drop.yaml` 是同一系统的部署配置变体（v1 `deploy_agent_stall.yaml` 同例），不是新计划。
- **反模式 1（按交付物维度组织）**：新 suite/binary 以被测关注点（disconnect 分支）命名，非 spec 编号/需求步骤。
- **反模式 2（一文件多模块）**：拆分后每 binary 聚焦一组关注点，粒度更细而非更粗。
- 结论：不冲突，且消除了"为 env 变体复制服务身份"这一 deploy 层面的形态重复。

---

## 2. 意见 3 — resource name 解析改为 codegen 生成

### 2.1 工具链查证结论

**仓库工具链已完全具备生成能力，无需引入任何新依赖或新插件；唯一缺口是 `agent_v2.proto` 缺 `google.api.resource` 注解。**

- 仓库已全仓接入 `protoc-contrib/protoc-gen-go-aip` v0.1.3：根 `BUILD.bazel:51-63` 定义 `go_proto_compiler` `//:go_gen_aip`（注释明示 "generating type-safe resource-name parsers (e.g. ParseSessionName) from google.api.resource annotations"），并经 gazelle 指令 `go_grpc_compilers`/`go_proto_compilers`（`BUILD.bazel:6-7`）应用于**全部** `go_proto_library`。
- `projects/game/BUILD.bazel:70-98` 的 `agent_v2_go_proto` compilers **已含** `//:go_gen_aip`（`:76`）。当前它只为 agent_v2.proto 生成 `// +build ignore` stub——因为无 resource 注解可生成（`experimental/golang/aip_codegen/FINDINGS.md` F4："for a proto with no google.api.resource messages the plugin emits a 2-line stub"）。
- 插件行为（源码核实，`/home/liukexin/go/pkg/mod/github.com/protoc-contrib/protoc-gen-go-aip@v0.1.3/internal/generator/resource/generator.go`）：只为被生成文件（`f.Generate`）产出解析器，但 registry 遍历编译单元内**全部**文件——跨文件（agent_v2.proto 引用 game.proto 声明的 Session/Template 资源类型）的 parent 与 `resource_reference` 在 registry 层可解析；`Parent()` 方法（`emitParent`，`:644`）以 GoIdent 输出返回类型、跨包会被正确限定。**但 parent 构造器方法（`emitParentConstructor`，`:596-614`）的 receiver 以裸名 `parent.Variant.GoName` 输出**——跨 Go 包场景产物不可编译，处置裁定见 §2.6。
- 生成 API 形状（FINDINGS F5 + spike 测试实证）：`ParseAgentName(s) (AgentName{TemplateID, SessionID}, error)`、`String()/FullName()/Validate()`、trailing-literal 单例（`templates/{t}/sessions/{s}/agent` 末段字面量 `agent`）已被 v1 `Team`（`.../team`）同形实证；`resource_reference` 字段生成委托 `Parse<Field>()` 方法。
- 运行时 TS 面零影响：`ts_proto_library` 类型为 import-only；proto-loader 运行时打包的 `google/api/resource.proto` 已在 `agent_v2/server_pkg.tar` 内（game.proto 传递闭包带入，tar 清单实证），`includeDirs` 可解析新 import。

### 2.2 proto 注解方案（对齐 v1 `projects/game/game.proto` 先例与 AIP-122/123）

对 `projects/game/agent_v2.proto` 增补（与意见 4 的服务拆分**同一次** proto 变更交付，见 §4.1）：

```proto
import "google/api/resource.proto";   // 新增 import

message Agent {
  option (google.api.resource) = {
    type: "game.liukexin.com/Agent"
    pattern: "templates/{template}/sessions/{session}/agent"
    singular: "agent"
    plural: "agents"
  };
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];  // 对齐 v1 name 字段惯例
  ...
}

message Preset {
  option (google.api.resource) = {
    type: "game.liukexin.com/Preset"
    pattern: "templates/{template}/presets/{preset}"
    singular: "preset"
    plural: "presets"
  };
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  ...
}
```

`resource_reference` 增补（对齐 v1 请求字段惯例，`game.proto:298/796/939` 等；插件行为已按源码核实：仅 `type` 引用生成委托方法，`child_type` 仅作引用语义不生成——`internal/generator/resource/generator.go` `fieldReference` "Ignore fields that ... only carry a child_type reference"）：

| 字段 | 注解 | 生成物 |
|---|---|---|
| `Agent.preset` | `(google.api.resource_reference) = {type: "game.liukexin.com/Preset"}` | `Agent.ParsePreset() (PresetName, error)` |
| `SendRequest.session` | `type: "game.liukexin.com/Session"`（v1 文件声明的资源；Go 侧经 §2.6 同包化后为同包引用） | `SendRequest.ParseSession() (SessionName, error)`（同包裸名；消费方经 `game.` 限定） |
| `GetAgentRequest.name` / `ListAgentMessagesRequest.parent` | `type: "game.liukexin.com/Agent"`（parent 即 Agent 资源本体，用 `type` 而非 `child_type`） | `ParseName()` / `ParseParent()` → `AgentName` |
| `CreatePresetRequest.parent` / `ListPresetsRequest.parent` | `child_type: "game.liukexin.com/Preset"`（AIP-133/132 与 v1 `ListMessagesRequest.parent` 先例形态） | 无方法生成（`child_type` 仅引用语义，v1 同例） |
| `GetPresetRequest.name` / `DeletePresetRequest.name` | `type: "game.liukexin.com/Preset"` | `ParseName() (PresetName, error)` |

另：`Agent`/`Preset` 消息本体随 resource 注解获得 `ParseName()`/`ParseFullName()` 方法（插件对 resource 消息的 name 字段固定生成，`collectReferences` 首分支）。

要点：

- **type 命名沿用 `game.liukexin.com/*`**（AIP-123：`{service_name}/{Type}`，单一 `/`、PascalCase）。v1/v2 是同一 game API service 的两个版本面，资源共享类型域；v1 已注册六类型（Template/Session/Team/TeamProfile/Memory/Message），`Agent`/`Preset` 与之无冲突（插件 registry 按全类型域键控，重名会静默覆盖，必须避开——已核对无碰撞）。
- **`HistoryMessage` 不加注解**：无 `name` 字段、无单条 Get/子资源寻址消费方（web 按 message_id 展示、proxy 只解析 parent），生成解析器无消费点——按原则 II 取最小面。若未来出现按消息名寻址的 RPC 再补。

### 2.3 BUILD 接线（按 §2.6 裁定 D 的生成单元合并）

- `projects/game/BUILD.bazel`：`agent_v2.proto` 并入 `game_proto` srcs 与 `game_go_proto` 生成单元（compilers 已含 grpc-gateway + `//:go_gen_aip`，v2 双服务的 gRPC/gateway/AIP 产物随同 importpath 产出）；**撤销** `agent_v2_go_proto` target 与 `agent_v2` go_library（同一 importpath 不能并存两个 go_proto_library——"multiple copies of package passed to linker"，Phase 2 实测注释 `projects/game/BUILD.bazel:82-96`）。`agent_v2_proto`（proto_library）保留供 TS `ts_proto_library` 与 `runtime_protos` 消费（与 `game_proto` 共享同一 .proto 文件在 Bazel 下合法——protoc 按 target 独立执行），其 deps 增 `@googleapis//google/api:resource_proto`（新 import 的直接声明；`game_proto` 已有该 dep）。
- 产物：`agent_v2_*.pb.go`（含 `agent_v2_aip.pb.resource.go` 的解析器，取代 ignore-stub）生成于 `dominion/projects/game` 包内（`ParseAgentName`/`ParsePresetName` 等）。
- `//:go_gen_aip` 仍由 gazelle 指令保证不被剥离（FINDINGS F3）；TS/运行时打包面零改动。

### 2.4 手写解析的处置（决策：生成替换 + 保留业务校验薄 wrapper）

| 手写代码 | 处置 | 说明 |
|---|---|---|
| `parseAgentResourceName`（`projects/game/proxy/handler/agent.go:441`） | **改为生成解析 + 已知模板校验的薄 wrapper**：`game.ParseAgentName(name)` + `gameconst.IsKnownTemplateID(parsed.TemplateID)`，返回 `game.AgentName`（字段 `TemplateID`/`SessionID` 与现调用点 `assignAgentOwner(ctx, ..., name.TemplateID, name.SessionID)` 源码兼容；生成 API 经 §2.6 并入 `dominion/projects/game` 包） | 形状校验（5 段、字面量 `templates`/`sessions`/`agent`、非空变量）由生成器承载且语义等价；**已知模板集合校验是业务规则**（`gameconst`），codegen 不承载，保留在 wrapper。测试断言仅校验 status code（`agent_test.go` 核实），生成器错误文案不同不破坏测试 |
| `parsePresetName`（`agent.go:466`） | **随意见 4 整体删除**（proxy preset 转发面移除，解析无残留消费点） | 其注释自认缺注解才手写——注解补齐 + 转发面移除后双双失效 |
| `parseTemplateParent`（`agent.go:451`） | **随意见 4 删除**（仅 preset 转发路径使用，已核实无其他调用点） | `parseAgentSession`（`game.ParseSessionName` 生成 + 模板校验）保留，`Send` 继续使用 |
| v1 `handler.go:260 parseMessagesParent` 等手写解析 | **不动**（v1 代码保留仓库，`spec.md` FR-019） | 不在本 feature 范围 |
| TS 侧 `server.ts` 的 `parseSessionResource`/`parsePresetResource` 等正则解析 | **保留**（TS 面无生成路径：ts-proto/proto-loader 不从 `google.api.resource` 注解生成解析器，TS 生态现状；FINDINGS F6 亦明示 "the TS agent is unaffected"） | 与意见 4 的服务拆分正交（`parsePresetResource` 移入 preset handlers 一侧） |

**否决的替代方案**：仅加注解不改 proxy（解析器生成了却不用，双事实源）；引入其他生成器插件（`protoc-gen-go-patch` 等——仓库已接入的 `protoc-gen-go-aip` 完全覆盖需求，引入第二个生成器徒增工具链面积）。

### 2.5 对 T004 已交付契约面的影响：**增量、非破坏**

`google.api.resource`/`resource_reference`/`IDENTIFIER` 均为 non-wire 选项——不改 wire format、REST 路径、方法签名、字段编号。`go_package` 变更（§2.6）同样仅影响生成代码的 Go 包归属，proto package `projects.game.v2`、wire、REST、TS 面全部不变。`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §1 proto 列表同步增补注解并修订 `go_package` 表述（该文件头部 "go_package ... 不变" 的陈述随 T004a 更新，见 §5 修订清单），下游（web/testplan）零感知。

### 2.6 工具链缺陷与裁定 D：v2 Go 包并入 v1 同包（2026-09-02 用户裁定）

**缺陷事实**：`protoc-gen-go-aip` v0.1.3 的 parent 构造器发射器（`internal/generator/resource/generator.go:596-614` `emitParentConstructor`）以**裸名** `parent.Variant.GoName` 输出 receiver——跨 Go 包父资源场景（`Preset` 的父资源 `Template`、`Agent` 的父资源 `Session` 均声明于 game.proto/v1 包，而 v2 生成文件属 `dominion/projects/game/v2`）产出形如 `func (n TemplateName) PresetName(...)` 的未限定类型引用，v2 包内不可解析，**产物不可编译**；上游无修复版本。T004a 注解落地时实测触发。

**裁定（选项 D）**：放弃对插件打模块补丁的路线，**v2 Go 生成面并入 v1 同包**——`projects/game/agent_v2.proto:13` 的 `option go_package` 从 `dominion/projects/game/v2` 改为 `dominion/projects/game`。生成产物与 v1 同包后，parent 构造器/`Parent()`/`resource_reference` 委托（如 `SendRequest.ParseSession()` 返回 `SessionName`）全部为同包裸名引用，天然可解析，缺陷不再触发。

**配套（一次性）**：

1. **BUILD 生成单元合并**（§2.3）：`agent_v2.proto` 并入 `game_proto`/`game_go_proto`，撤销 `agent_v2_go_proto` target 与 `agent_v2` go_library（同 importpath 双 target 触发链接器 "multiple copies of package" 拒绝）；grpc-gateway 双服务 handler 产物并入 `game_go_proto` 的编译目标。`agent_v2_proto`（proto_library）保留供 TS（`ts_proto_library`、`runtime_protos`）消费——同一 .proto 文件被两个 proto_library 引用在 Bazel 下合法（protoc 按 target 独立执行）。
2. **Go 导入面改名（机械性，12 处）**：`gamev2 "dominion/projects/game/v2"` 导入与 `gamev2.` 限定符并入 `dominion/projects/game` 导入（限定符统一 `game.`）——proxy `cmd/main.go`、`handler/agent.go`、`handler/bridge.go`、`handler/agent_test.go`、`handler/bridge_test.go`；gateway `cmd/main.go`、`cmd/main_test.go`；testplan `agent_v2_conversation_test.go`、`agent_v2_game_test.go`、`agent_v2_helpers_test.go`、`desktop_flow_test.go`、`web_test.go`。
3. **名称安全（已验证）**：v1/v2 proto 包独立（`projects.game` vs `projects.game.v2`），服务/消息/枚举名零碰撞（AgentService/PresetService/DesktopBridgeService、Agent/Preset/Model、ChatEvent 事件家族、ToolStatus 等 v2 名与 v1 名无重合），resource type 无重复声明（§2.2 的类型域核对）——同包合并不产生声明冲突。

**不受影响项**：proto package、wire format、REST 路径、TS 生成与运行时打包、`gameconst.AgentV2Target` 服务发现（服务名与 proto 无关）。

---

## 3. 意见 4 — PresetService 拆分与 gateway 直连 agent-v2

### 3.1 拆分原则

按**状态归属/路由类**拆服务，而非按资源 taxonomy：`AgentService` = 会话作用域的有状态面（owner 亲和，内存态 agent/历史/游戏运行时）；`PresetService` = 无状态配置面（preset 状态在 Mongo `game_agent_v2.presets`，模型目录为静态配置——任意 agent-v2 实例可服务，research D2 已论证）。这与 proxy 现有 handler 注释的自述（"Routing is split by where the RPC's state lives"，`agent.go:40-53`）同构，只是把"无状态经 proxy 哈希选实例"改为"不经 proxy"。

**两服务同宿主 agent-v2 进程（50051 单 gRPC server 双注册 + 既有 DesktopBridgeService 共三服务）**，不新拆部署。理由：

1. `ListModels` 的目录源是 agent-v2 组合内 llm-glm 插件的 `config.models`（research D4：经 `ctx.llm.listModels("glm-responses")` 单一来源导出）——独立部署将迫使目录双源或跨服务调用；
2. preset 存储模块（`presets.ts`）、模型目录（`listModelCatalog`）与宿主 dsh 组合同生命周期，同宿主零迁移成本；
3. 用户指令的落点是"移除 proxy 中转"，非"独立部署"——"preset service" 指 gRPC 服务面拆分。

### 3.2 proto 拆分设计

从 `AgentService` **移除**六方法（连同其 Request/Response 消息归属调整，消息定义本身不动，仅服务面重组）：

- `CreatePreset`/`ListPresets`/`GetPreset`/`UpdatePreset`/`DeletePreset`
- `ListModels`（归属决策见 §3.3）

新服务（`projects/game/agent_v2.proto` 内、REST 注解逐字保留——**路径不变**）：

```proto
// PresetService is the stateless configuration surface of agent_v2: the
// preset collection backing agent materialization and the deployment-level
// model catalog. Preset state lives in Mongo and the catalog is static
// plugin configuration, so any agent_v2 instance can serve every RPC — the
// gateway dials agent_v2 directly for this service (no proxy owner
// affinity). Contract: specs/051-agent-v2-dsh-migration/contracts/agent-api.md.
// Prefix Path: /api/v2
service PresetService {
  rpc CreatePreset(CreatePresetRequest) returns (Preset) { ... }   // 注解原样
  rpc ListPresets(ListPresetsRequest) returns (ListPresetsResponse) { ... }
  rpc GetPreset(GetPresetRequest) returns (Preset) { ... }
  rpc UpdatePreset(UpdatePresetRequest) returns (Preset) { ... }
  rpc DeletePreset(DeletePresetRequest) returns (google.protobuf.Empty) { ... }
  rpc ListModels(ListModelsRequest) returns (ListModelsResponse) { ... }
}
```

`AgentService` 终态 = `UpdateAgent`/`GetAgent`/`ListAgentMessages`/`Send` 四方法（纯 owner 亲和面），服务注释与 Prefix Path 注释同步改写（`style/api.md`：Service/Method 注释要求）。

### 3.3 ListModels 归属决策：**移入 PresetService**

理由：

1. **完成 proxy 无状态路径的彻底移除**：`ListModels` 无 session 键、不可能 owner 亲和；留在 AgentService 将迫使 proxy 仅为一个方法保留整套 `affinityFreeConn` + `listModelsPickKey` + 哈希选实例机制及其 4 个测试——违背"移除 proxy 无状态转发代码"的指令目标。
2. **消费方对齐**：web 物化面板同时消费 presets 下拉与 models 下拉（`contracts/web-frontend.md` §3），同服务归属与消费形态一致。
3. **同源校验不受影响**：`UpdateAgent` 的模型校验读 `ctx.llm.listModels`（agent-v2 进程内），与 `ListModels` RPC 同源（research D4）——服务归属不改变同源性。
4. 命名张力（models 非 preset 资源）被接受并在服务注释中言明：PresetService 语义 = agent-v2 的无状态配置面（preset 集合 + 模型目录）。

### 3.4 gateway 直连拓扑

`projects/game/gateway/cmd/main.go` 增量：

```go
// presetConn dials agent_v2 directly for the stateless configuration
// surface (PresetService): preset state lives in Mongo and the model
// catalog is static — any instance serves, so no proxy owner affinity
// (049 D4 revised by specs/051-agent-v2-dsh-migration/revisions/
// directive-2026-09-01.md §3). Unary RPCs → default keepalive.
presetConn, err := grpc.NewClient(solver.URI(gameconst.AgentV2Target), clientOpts)
...
game.RegisterPresetServiceHandler(ctx, gwmux, presetConn)    // 新注册（生成 API 经 §2.6 并入 dominion/projects/game 包）
game.RegisterAgentServiceHandler(ctx, gwmux, teamConn)       // 既有，仅剩 agent 面 4 RPC 的路径
b.Register(bootstrap.GRPCConn("agent-v2", presetConn))
```

可行性依据（已核实）：

- **发现与负载均衡**：`gameconst.AgentV2Target = "game/agent-v2:grpc"`（`projects/game/pkg/gameconst/const.go:25`）已存在；`solver.URI` 产出普通 `dominion:///` scheme，其 resolver 返回服务**全部 ready 端点**、不区分 stateful（`common/gopkg/solver/deploy_resolver.go` `Resolve` 返回全量 `info.Endpoints`）——gRPC 客户端 LB 覆盖多实例；preset 无亲和诉求，任意实例正确服务。
- **路径不冲突**：proto 拆分后 AgentService handler 只注册 agent 面 4 条路径，PresetService handler 注册 preset/models 路径——gwmux 两 handler 路径集不相交。
- **TLS/keepalive**：与 `sessionConn`/`memoryConn` 同款 `clientOpts`（unary，默认 keepalive；无长流）。
- **公网面不变**：直连是 gateway→agent-v2 的服务发现 gRPC，非 Gateway API HTTP 路由——agent-v2 仍无 `http` 块、无公网暴露。

### 3.5 049 D4 约束的修订（哪个文档、改成什么）

049 的 `research.md` D4 是 049 feature 的历史决策记录（同 v1 代码保留仓库的处置），**不回改**；约束的现役表述在 **051 文档链**，修订如下（049 D4 的原始理由——"有状态服务的内存会话跨实例撕裂"——对 preset/models 面本就不成立，本次修订将该理由的适用边界显式化为"会话面"）：

| 文档/位置 | 修订 |
|---|---|
| `specs/051-agent-v2-dsh-migration/plan.md` Constraints | "agent-v2 仅经 proxy owner 亲和可达（无 http 块，049 D4）" → "agent-v2 的**会话面**（AgentService/DesktopBridgeService）仅经 proxy owner 亲和可达（049 D4）；其**无状态配置面**（PresetService：preset CRUD + 模型目录）由 gateway 直连 agent-v2（2026-09-01 用户指令，[revisions/directive-2026-09-01.md](revisions/directive-2026-09-01.md) §3）——preset 状态在 Mongo、目录为静态配置，无 session 亲和诉求" |
| `specs/051-agent-v2-dsh-migration/research.md` D9 | "无亲和 RPC"分支整段改写为 PresetService 拆分 + gateway 直连；Alternatives 增记"preset 经 proxy 无亲和转发"被否决（为无状态 RPC 强造 proxy 中转层） |
| `specs/051-agent-v2-dsh-migration/data-model.md` §1 图、§2.9 表 | §2.9 删除 "preset CRUD / ListModels \| 无亲和（请求派生键稳定哈希）" 行；增 PresetService 行 = "gateway 直连（任意活实例，gRPC 客户端 LB）"；§1 拓扑图 proxy 层与 gateway 层标注同步 |
| `specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §1/§3/§4 | §1 proto 按本文 §2.2/§3.2 更新（双服务 + 注解）；§3 两跳错误表：preset/models 行改一跳语义（gateway→agent-v2 不可达 → UNAVAILABLE/503；Mongo 故障 → INTERNAL/500 不变）；§4 路由拓扑改双面描述 |
| `specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §3 | "teamConn 保留、改承载 v2 面（AgentService HTTP + DesktopBridgeService bidi）" → "teamConn 承载 AgentService（会话面）HTTP + DesktopBridgeService bidi；PresetService 经 gateway 直连 agent-v2（presetConn）" |
| `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §2/§3 | 增一句：API 路径与请求形状不变，仅后端路由改为 PresetService 直连（前端零改动） |
| `projects/game/testplan/deploy_agent_v2.yaml` 注释 | "The proxy is a mandatory hop for /api/v2" → 会话面两跳 + 配置面直连的分工描述 |
| `projects/game/agent_v2/service.yaml` desc | 增双面语义（会话面经 proxy 亲和；配置面 gateway 直连） |

`projects/game/deploy.yaml`（产线）无变更：无新服务、无新端口、无 http 块。

### 3.6 proxy preset 代码移除范围

`projects/game/proxy/handler/agent.go`：删除 `CreatePreset`/`ListPresets`/`GetPreset`/`UpdatePreset`/`DeletePreset`/`ListModels` 六个转发方法、`affinityFreeConn`（`:394-419`）、`listModelsPickKey`（`:32-36`）、`parsePresetName`（`:466-478`）、`parseTemplateParent`（`:451-460`）；`AgentHandler` struct 与文件头注释改为纯 owner 亲和面自述。`parseAgentResourceName` 按本文 §2.4 换生成解析。`lookupAgentOwner`/`assignAgentOwner`/`agentV2Conn`/`parseAgentSession` 保留（owner 亲和与 `Send` 使用）。

`projects/game/proxy/handler/agent_test.go`：删除 4 个 `TestAgentHandler_AffinityFreeRPCs_*` 用例（`:731-802`）；`UpdateAgent`/`GetAgent`/`ListAgentMessages`/`Send` 用例保留。

`projects/game/proxy/cmd/main.go`：装配不变（注册的仍是 Team/Agent/Bridge 三服务面；生成的 `AgentServiceServer` 接口随 proto 自动缩小，`UnimplementedAgentServiceServer` 嵌入兜底）；注释同步。`cmd/main_test.go` `TestRegisterServices` 按接口缩小适配（注册集不变则仅编译面核对）。

### 3.7 agent_v2 宿主双（三）服务注册

`projects/game/agent_v2/src/server.ts`：

- `buildAgentHandlers(deps)` 拆分：`AgentServiceDeps` 收敛为会话面协作者（`sessions` + 物化校验所需 preset 读取/模型目录——`UpdateAgent` 校验 preset 存在与 model ∈ 目录仍需 `presets`/`listModels` 依赖，保留注入）；preset CRUD 五 handler 与 `ListModels` handler 移出新函数 `buildPresetHandlers(deps: {presets, listModels})`。
- `startServer`：`server.addService` 三次（`AgentService` / `PresetService` / `DesktopBridgeService`）；`parsePresetResource`/`parseTemplateParent`（TS 正则版）随 preset handlers 归置。
- `server.test.ts`：preset CRUD/模型目录/同源校验用例迁至 `buildPresetHandlers` 断言面；agent 面用例去 preset CRUD 项、保留 UpdateAgent 的 preset/model 校验断言。
- `presets.ts` 存储模块、`listModelCatalog`、`bootstrap.ts` 零改动。

### 3.8 web 前端影响：**零改动（已验证）**

PresetService 的 REST 注解与现 AgentService 逐字相同（`/api/v2/{parent=templates/*}/presets`、`/api/v2/{name=templates/*/presets/*}`、`/api/v2/models`、PATCH body 形状）——对 `contracts/web-frontend.md` §2/§3 路径表与 Phase 4 `T024`（`api/agent.ts`，未实现）而言只是后端 gwmux handler 换挂直连 conn，前端 API 客户端形状、调用路径、错误语义全部不变。

### 3.9 testplan 影响

- helpers（`createAgentV2Preset`/`updateAgentV2Agent` 等，`agent_v2_helpers_test.go`）走 gateway HTTP 同路径——**零代码变更**。
- `agent-v2-preset` 套件（Phase 4 T028）断言不变（经 gateway 往返）；拓扑不变。
- 断连根因（意见 2）若导致 fake-llm 模板/等待窗调整，与本文正交。

---

## 4. 冲突检查与实施顺序

### 4.1 与在途 Phase 3 实现的冲突

| 冲突点 | 处置 |
|---|---|
| 意见 4 vs 批次 4 刚写的 proxy 无亲和路径（`agent.go` preset 转发 + `agent_test.go` 4 个 AffinityFree 用例） | §3.6 删除/改写——该批次产出的 owner 亲和部分（`assignAgentOwner`/`lookupAgentOwner` 与 Update/Send/Get/List 用例）全部保留 |
| 意见 4 vs `server.ts` 单 `buildAgentHandlers`（preset handlers 内嵌）与 `server.test.ts` | §3.7 拆分重组 |
| 意见 3 改 proto vs T004 已交付契约面 | §2.5：增量注解（non-wire），`contracts/agent-api.md` §1 增补——非破坏 |
| 意见 3 vs `parseAgentResourceName` 手写与 INVALID_ARGUMENT 用例 | §2.4：wrapper 替换；用例仅断言 status code，不破 |
| 裁定 D vs T005 已交付的独立 `agent_v2_go_proto` codegen 面 | §2.6：生成单元并入 `game_go_proto`、撤销独立 target；12 处 Go 导入面随 T004a (c) 机械改名——proxy/gateway/testplan 的后续任务（T018a/T019a 等）消费改名后的 `game.` 限定符，均在 T004a 之后执行，无顺序变化 |
| 意见 1 vs T022 已交付 deploy/system_test/测试文件 | §1.2–1.6 重组；`deploy_agent_v2.yaml` 从"两实例"修剪为单 won 实例 |
| 意见 1+4 交互 | `system_test.yaml` 的 suite 重组与 deploy 注释修订一次编辑完成；`agent-v2-conversation` 套件 deploy 内注释同步 |
| 意见 3+4 交互 | `parsePresetName` 随 preset 转发面删除（不再需要生成替换）；proto 的注解增补与服务拆分**同一次变更**交付，避免两次 codegen 波动 |

### 4.2 实施顺序（tasks.md Phase 3R 落实）

```text
T004a（proto 修订 + 契约文档同步 + codegen）
  ├─ T014a（agent_v2 宿主注册拆分）→ T014b（agent_v2 接入 053 bootstrap，
  │                                     同目录同文件 serial：T014a 先）
  ├─ T018a（proxy 移除 preset 面）     ├─ 三条线不同目录，可并行
  └─ T019a（gateway 直连注册）        ┘
T022a（fake-desktop 单服务化）→ T022b（suite/deploy 重组 + 测试拆分）
（T022a/T022b 与 T004a 线无文件交集，可并行）
T023（修订后）全量重跑验收——前置：T014b 完成后 agent_v2 才能通过 052
      startupProbe（§8.2），否则部署 rollout 不 READY
```

### 4.3 中间状态说明

T004a 落地而 T019a 未落地的时间窗内，gateway 的 gwmux 上 preset 路径暂时无 handler（404）——该窗内不执行大型测试（验收统一在 T023 重跑），proxy/gateway/agent_v2 单测各自闭环，无部署可见的中间态。

---

## 5. 设计文档修订清单（汇总）

| 文档 | 节 | 变更 |
|---|---|---|
| `specs/051-agent-v2-dsh-migration/contracts/agent-api.md` | §1 | proto：AgentService 缩为 4 RPC；新 PresetService（6 RPC）；resource 注解与 `resource_reference`/`IDENTIFIER` 增补；**`go_package` 表述修订**（`dominion/projects/game/v2` → `dominion/projects/game`，§2.6 裁定 D；文件头部 "go_package 不变" 陈述同步更新） |
| 同上 | §2.5/§2.6 | 语义不变；归属面标注 PresetService |
| 同上 | §3 | 错误表：preset/models 行改一跳语义 |
| 同上 | §4 | 路由拓扑双面描述（teamConn 会话面 / presetConn 配置面） |
| `specs/051-agent-v2-dsh-migration/research.md` | D9 | preset/models 路由改直连；Alternatives 增记 |
| `specs/051-agent-v2-dsh-migration/data-model.md` | §1、§2.9 | 拓扑图与路由表 |
| `specs/051-agent-v2-dsh-migration/plan.md` | Constraints | 049 D4 适用边界修订（§3.5 表） |
| `specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` | §3 | teamConn 承载面表述 |
| `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` | §2/§3 | 增"后端路由直连、前端零改动"注 |
| `specs/051-agent-v2-dsh-migration/quickstart.md` | §2 套件表 | 增 `agent-v2-game-disconnect` 行；`agent-v2-game` 行修剪 |
| `specs/051-agent-v2-dsh-migration/tasks.md` | Phase 3R | 见 §6 |
| `projects/game/testplan/deploy_agent_v2.yaml` | 注释 | proxy mandatory hop → 分工描述 |
| `projects/game/agent_v2/service.yaml` | desc | 双面语义 |

---

## 6. tasks.md 修订（Phase 3R：Directive 2026-09-01 返工）

任务文本以 tasks.md 落地版本为准；此处为摘要：

- **T004a** proto 增量修订（意见 3 注解 + 意见 4 服务拆分 + 裁定 D 同包化）+ §5 契约文档同步 + codegen/生成单元合并；门禁 `bazel build //projects/game/...` + 相关单测。
- **T014a** agent_v2 宿主 `PresetService` 注册拆分（`server.ts`/`server.test.ts`）；门禁 `bazel test //projects/game/agent_v2`。
- **T018a** proxy 移除 preset/models 转发面 + `parseAgentResourceName` 换生成解析；门禁 `bazel test //projects/game/proxy/...`。
- **T019a** gateway `PresetService` 直连注册；门禁 `bazel test //projects/game/gateway`。
- **T022a** fake-desktop 单服务化（manifest/BUILD/删 drop 副本）；门禁 `bazel build //projects/game/fake-desktop/...`。
- **T022b** suite/deploy 重组（`deploy_agent_v2_drop.yaml`、`system_test.yaml`、disconnect 测试拆分与 BUILD target）；门禁 `bazel build --config=largetest` 相关 target。
- **T014b** agent_v2 接入 053 JS bootstrap 组件（`bootstrap.ts` 两段式重写 + `server.ts` 拆 `buildServer` + 依赖/BUILD 接线；意见 2 处置的前置迁移，§8.3）；门禁 `bazel test //projects/game/agent_v2`。
- **T023 修订**：验收重跑范围增 `agent-v2-game-disconnect` 套件；全部用例通过为验收（constitution 原则 VI）；**前置依赖 T014b**（否则 agent_v2 pod 无法通过 startupProbe，部署失败）。

---

## 7. 裁定记录

1. **ListModels 移入 PresetService**（§3.3）——✅ 用户裁定（2026-09-02）：移入。proxy 无状态转发面（含稳定哈希/零 owner 交互机制）整体移除。
2. **resource type 沿用 `game.liukexin.com/*` 前缀**（§2.2）——✅ 用户裁定（2026-09-02）：沿用（`game.liukexin.com/Agent`、`game.liukexin.com/Preset`，v1/v2 同一 API service 共享类型域）。
3. **工具链缺陷处置 = 选项 D（v2 Go 包并入 v1 同包）**（§2.6）——✅ 用户裁定（2026-09-02）：`protoc-gen-go-aip` v0.1.3 跨 Go 包父构造器缺陷以 `go_package` 并入 `dominion/projects/game` 规避（放弃模块补丁路线）；配套 BUILD 生成单元合并（撤销 `agent_v2_go_proto`）与 12 处 Go 导入面改名，随 T004a (c) 交付。

---

## 8. 意见 2 处置终态（2026-09-02 追加）

意见 2 的原始问题——proxy→agent-v2 部署窗口的 503（大型测试中新部署实例就绪前的 gRPC 不可达）——**已由用户在上游修复并合入**，本 feature 只承担一项前置迁移（T014b）。本节为该处置的终态记录（原则 VII：只表述终态与"为什么是现在这样"）。

### 8.1 上游修复事实（已核实）

| 修复 | 内容 | 引用 |
|---|---|---|
| 052 deploy health 探针 | deploy 工具为**所有**用户服务容器无条件附加 `/healthz:38080` startupProbe（约 300s 预算）+ livenessProbe；startupProbe 成功前容器不计入 Ready——就绪判定从"进程在跑"变为"全部组件启动完成" | `projects/infra/deploy/runtime/k8s/builder.go:268-294`（`buildHealthProbes` 无条件附加）、`specs/052-deploy-health-probe/contracts/deploy-probe.md` |
| 053 JS bootstrap 组件 | `common/js/bootstrap`（`@dominion/common-js-bootstrap`）交付：`Bootstrap` 编排器（stage 升序启动、失败逆序回滚、**全部组件启动后才启动 38080/healthz**、停止 health 首位+组件严格逆序）+ grpc-server/grpc-conn/http-server 适配器与 daemon 监督器；Go 侧 `common/gopkg/bootstrap.Run()` 同语义（`bootstrap.go:127-137` health 后置） | `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md`（§3 编排序列/§6 适配器/§8 两段式接入） |
| guitar postDeploySettle | deploy apply 成功 → 首个测试用例之间固定等待 60s，覆盖探针延迟的 DNS/endpoint 传播窗（grpc-go DNS resolver 最长 30s 才重解析） | `tools/test/guitar/pkg/run/run.go:50`（注释含依据） |
| 测试侧重试移除 | `agent_v2_helpers_test.go` 的 503 重试代码已移除，就绪语义改为引用 052 探针 + postDeploySettle（`:40-43` 注释） | `projects/game/testplan/agent_v2_helpers_test.go` |

Go 游戏服务（proxy/gateway/web/session/memory/fake-llm/fake-desktop）经 `common/gopkg/bootstrap` 天然获得 `/healthz:38080`；实验服务已按 053 T016/T017 接入（`experimental/js/grpc_hello_world/src/bootstrap.ts` 为迁移样板）。**`@dominion/common-js-bootstrap` 的生产服务消费者此前为零——agent_v2 是第一个。**

### 8.2 agent_v2 现状与必须迁移的原因

`projects/game/agent_v2/src/bootstrap.ts` 仍是 053 之前的手写 bootstrap：手排 OTel → dsh composition → Mongo → gRPC server 顺序 + 自管 SIGTERM/SIGINT 链，**无 /healthz 端点**。由于 052 探针**无条件**附加，agent-v2 pod 的 startupProbe 将持续失败——rollout 不 READY（`specs/052-deploy-health-probe/contracts/deploy-probe.md` 明列 agent_v2 为未适配服务），Phase 3R 的 T023 重跑会**部署失败**。因此 T014b 不是可选的现代化，而是 T023 的硬前置。

### 8.3 T014b 迁移设计要点（任务文本以 tasks.md 为准）

- **两段式外壳**（053 §8 固定序列，样板 `experimental/js/grpc_hello_world/src/bootstrap.ts`）：`bootstrap.ts` 静态导入仅 `@dominion/common-js-{otel,grpc-otel,logs,bootstrap}` → `await init({ instrumentations: [createGrpcInstrumentation()] })` → `installReporter(createOTelReporter("game/agent-v2"))`（`unhandledRejection` 安全网保留，进程级）→ 动态 import 构建组件 → `register` × 3 → `await run()` → `uninstallReporter()` + `await shutdown()` → `process.exit`。OTel 生命周期不组件化（053 research D4：server 实例必须晚于 init 产生，时序与组件模型互斥）。
- **组件映射**（三组件）：
  - **preset Mongo 存储**：start = `connect` + `ensureIndexes`（fail-loud，FR-005 语义不变——启动失败即回滚退出非零）；stop = `client.close()`。
  - **dsh composition**：start = `bootDsh()`（fail-loud）；stop = `sessions.shutdown()`（dispose 全部 agent session（在途回合 abort）→ composition root fiber）。
  - **gRPC server**：`createGrpcServerComponent`（`0.0.0.0:50051`；stop = tryShutdown 与预算竞速 → forceShutdown——与现 `gracefulStop(10s)` 语义等价）。
- **生命周期不变量**（`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §6，现手写链的语义延续）：启动 Mongo → composition → server；停止严格逆序 = server → agents/fiber → mongo；OTel 最先/最后由两段式外壳承载。组件的 Stage/name 选择由实现按 053 API 满足该不变量（`Stage` 常量：Foundation 100 / Client 200 / Daemon 250 / Server 300，同 stage 按 name 升序稳定排序）。
- **health**：由 `Bootstrap` 内置（38080/healthz，全部组件启动成功后才开始 200）——agent_v2 自动满足 052 startupProbe 门控，无需自写 health 端点。
- **server.ts 拆分**：`startServer`（bindAsync+start）拆出 `buildServer`（构造 `AgentSessions`/deps/服务注册，返回**未 bind** 的 `grpc.Server` + sessions），bind 交组件——与 T014a 的三服务注册拆分（§3.7）在同一文件上 serial：**T014a 先、T014b 后**。
- **依赖与 BUILD 接线**（053 T016 样板）：`projects/game/agent_v2/package.json` + `"@dominion/common-js-bootstrap": "workspace:*"`；`BUILD.bazel` `ts_project` deps += `:node_modules/@dominion/common-js-bootstrap`，`artifact_pkg_js.runtime_deps` += `//common/js/bootstrap:runtime_pkg`（workspace 包仅经 runtime_deps 进部署闭包，不进 `npm_deps`——`projects/game/agent_v2/BUILD.bazel` 既有注释惯例）。
- **service.yaml 零变更**：052 契约明定 38080 不进 containerPorts/Service ports（kubelet 经 Pod IP 直探）。
- **测试**：`server.test.ts` 等单测适配 `buildServer` 形状（handlers 断言面不变）；T014 原文中 bootstrap.ts 的 mongo 生命周期部分由本任务以组件形态承载（T014 的 server.ts 注册部分仍在 T014a 交付）。
