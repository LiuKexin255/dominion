# Feature Specification: DesktopBridgeService.Connect 协议拆分——下行服务流 + 上行单发接口（含扫雷统计口径核验）

**Feature Branch**: `066-desktop-bridge-split`

**Created**: 2026-09-18

**Status**: Draft

**Input**: User description: "继续对 agent_v2 进行优化 1.确认 saolei 游戏结束后统计中，关于操作的计数是否统计执行的操作，没有执行的操作不统计。如果不是则修改，如果已经是了则忽略。2. 重构 DesktopBridgeService.Connect 协议，当前 desktop 只用做控制桌面的代理，不再承载用户和智能体消息。将 Connect 拆分成一个 service stream 接口用于接受服务端消息，以及一个 unary 接口用于发送消息。前期调研 @survey/desktop-bridge-connect-split.md 。重构后移除不再使用的代码。"

## Motivation

两项输入的核验/重构动机：

| # | 维度 | 现状 | 目标 |
|---|---|---|---|
| 1 | 扫雷终局统计口径 | `GameRuntimeService` 的 `operationCount`/`operationsByType` 仅在"派发成功且识别成功"后递增（`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:385-388`）；SKIP（无害 no-op）、STOP（结构性拒绝）、桌面派发失败、init/remain 一律不计（`common/js/dsh-plugins/saolei-loop/src/game/runtime.test.ts:594-630` 显式钉死；契约 [specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md](../065-agent-v2-team-refine/contracts/game-stats-broadcast.md) §1.2） | **已核验满足**"仅统计已执行的操作，未执行的操作不统计"——按用户指示忽略，无代码变更；仅作为回归不变量（FR-010，详见 Assumptions） |
| 2 | desktop 桥接协议 | `DesktopBridgeService.Connect` 为 bidi stream（`projects/game/agent_v2.proto:207-215`）。早期结论"grpc-gateway 不支持 bidi"成立，但由此在 gateway 自研了 WS↔gRPC 转换桥（`projects/game/gateway/cmd/main.go` 的路径手工匹配 / `wsStream` 适配器 / `pumpWebSocketConnect` 双向泵 / 关闭码分类，约 230 行 + 配套测试）；proxy 侧配套 bidi relay + 首帧身份重放（`projects/game/proxy/handler/bridge.go`）；desktop/fake-desktop 维护 WS 客户端 | desktop 现在只做控制桌面的代理，不再承载用户/智能体消息。将 `Connect` 拆分为一个 **server-streaming（下行）** 接口 + 一个 **unary（上行）** 接口后，全链路均为 grpc-gateway 原生支持形态（server streaming 已有生产先例：`AgentService.Send`），自研 WS 桥、bidi relay、首帧重放等机制性代码整体删除 |

现状锚点：完整链路 desktop（WSClient，二进制 proto 帧）→ gateway（自研 WS 桥 + URL 身份逐帧注入）→ proxy（首帧身份解析 + bidi relay + owner 亲和）→ agent_v2（grpc-js 单 bidi handler + desktop-bridge 插件 `BidiStream` 结构接口）；详细分析见调研文档 [survey/desktop-bridge-connect-split.md](../../survey/desktop-bridge-connect-split.md)（该文档为本 spec 的决策输入，含 grpc-gateway v2.27.6 支持矩阵、大文件上行编码社区实践、历史 fix 三分类迁移约束）。

## Decisions (2026-09-18, 调研已拍板)

以下决策来自 [survey/desktop-bridge-connect-split.md](../../survey/desktop-bridge-connect-split.md) §7，作为本 spec 的固定输入：

| # | 决策项 | 结论 |
|---|---|---|
| D1 | 下行流端点形状 | `GET /api/v2/{session=templates/*/sessions/*}:stream`（AIP-136 custom method 风格，与 `:send`/`:cancel` 命名族一致，参考 https://google.aip.dev/136 ） |
| D2 | 上行发送端点形状 | `POST /api/v2/{session=templates/*/sessions/*}:sendFrame`（custom method） |
| D3 | 上行编码 | 原始二进制 proto（`application/octet-stream`，内建 `ProtoMarshaller`）；JSON 路由不受影响 |
| D4 | 无活跃下行流时的 SendFrame | 拒绝 `FAILED_PRECONDITION`（单一连接事实源；接管间隙由 desktop 重试） |
| D5 | IDLE 静默期 liveness | 应用层 status 心跳（agent 周期推 status 帧，~10s 量级；顺带解决 ingress 空闲超时） |
| D6 | 迁移策略 | 直接切换（一次变更全链路：desktop/fake-desktop/gateway/proxy/agent_v2/testplan；同仓同发布，testplan 兜底） |
| D7 | SSE 预留 | 不做（Go 客户端 NDJSON 足够；未来需要时补自定义 marshaler 即可，无沉没成本） |

## User Scenarios & Testing *(mandatory)*

### User Story 1 - desktop 建立会话控制流并接收服务端指令 (Priority: P1) 🎯 MVP

desktop（真实桌面客户端或 fake-desktop）为某个游戏会话打开一条长生命周期下行流。连接建立即绑定到该会话（身份取自资源路径，客户端无法指定他人会话）；流打开后服务端**立即**推送一帧 status 作为连接确认（desktop 以首帧超时判定连接是否真正建立）；此后服务端到 desktop 的全部帧（操作请求、status 响应、心跳）都经该流下发。

**Why this priority**: 下行流是新的唯一连接事实源——`desktop_connected` 状态、新连接接管、断开清理全部锚定在这条流上；没有它，上行发送与游戏链路均无从工作。

**Independent Test**: 大型测试（fake-desktop 打开流 → 断言首帧 status 到达 → agent 派发操作请求 → fake-desktop 收到该请求）；单测覆盖流打开即推首帧行为。

**Acceptance Scenarios**:

1. **Given** 会话已物化且 desktop 未连接，**When** desktop 打开该会话的下行流，**Then** 流成功打开、立即收到一帧 status，且会话的桌面连接状态变为已连接。
2. **Given** 下行流已建立，**When** agent 侧发起一次桌面操作派发，**Then** 操作请求帧经下行流到达 desktop，desktop 执行后经上行接口回报结果（US2）。
3. **Given** desktop 打开流后服务端长时间无操作请求，**When** 空闲持续，**Then** 心跳 status 帧周期到达（约 10s 量级），链路不因空闲被中间层断开。

---

### User Story 2 - desktop 以独立请求回报操作结果（含截图） (Priority: P1)

desktop 每收到一个操作请求、执行完成后，通过独立的上行单发接口提交一个结果帧（内含操作截图，≤5MiB 二进制）。上行帧自包含（按 `flow_result.tool_id` 关联，不依赖与其他上行帧的先后顺序）；二进制载荷原样传输、无 base64 膨胀；超过上限的载荷被清晰归因拒绝、绝不静默截断。

**Why this priority**: 上行是桌面控制回路的另一半；截图是扫雷识别的输入，编码效率与守卫语义直接决定游戏链路可用性（延续 [specs/025-desktop-image-state-refine/contracts/image-transport-contract.md](../025-desktop-image-state-refine/contracts/image-transport-contract.md) §2 的既有裁定）。

**Independent Test**: 大型测试完整游戏局（init/operate 多操作 + 胜/负终局）在新接口上通过；超限载荷用例返回明确错误而非截断。

**Acceptance Scenarios**:

1. **Given** 下行流活跃且 desktop 收到操作请求，**When** 操作执行完成，**Then** desktop 经上行接口提交结果帧，agent 侧收到与该 `flow_result.tool_id` 关联的结果（含可识别的截图）。
2. **Given** desktop 依次提交多个结果帧，**When** 帧经复用连接并发传输，**Then** 每帧按自带标识正确关联到对应操作，处理不依赖到达顺序。
3. **Given** 某结果帧携带超过上限的载荷，**When** 提交，**Then** 收到明确的大小超限错误（清晰归因），会话与其他帧不受影响。

---

### User Story 3 - 连接生命周期：接管、断连与无流拒绝 (Priority: P2)

同一会话的新下行流打开时接管连接（旧流被结束）；旧流的晚到断开不得误删新连接的注册（compare-and-delete no-op 语义保持）。下行流关闭（网络故障或客户端退出）将会话的桌面连接置为断开：在途派发以既有 "desktop disconnected" 故障路径收束，新派发立即失败；无活跃下行流时的上行提交被拒绝（`FAILED_PRECONDITION`），desktop 在（重）建立下行流后重试即可恢复。

**Why this priority**: 连接语义的完整性决定重连/故障场景的行为正确性；接管与 no-op 清理是既有契约（[specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md](../051-agent-v2-dsh-migration/contracts/desktop-bridge.md) §2）必须原样保留的部分。

**Independent Test**: 大型测试断连场景（游戏中断开 desktop → 在途操作失败语义 → 重连 → 游戏继续）；单测覆盖接管、晚到断开 no-op、无流拒绝。

**Acceptance Scenarios**:

1. **Given** 会话已有活跃下行流，**When** 同会话新流打开，**Then** 新流接管（立即收到 status 首帧），旧流被结束且其后续断开事件不影响新连接的已连接状态。
2. **Given** 下行流中途断开，**When** agent 侧存在在途派发与新派发，**Then** 在途派发按既有故障路径失败（含 20min backstop 与 abort→"aborted" 语义不变），新派发返回 "desktop disconnected" 类错误。
3. **Given** 会话无活跃下行流，**When** desktop 提交上行帧，**Then** 被拒绝 `FAILED_PRECONDITION`；建立下行流后重试同一提交成功。

---

### User Story 4 - 自研 WS 桥与不再使用的代码移除 (Priority: P2)

协议拆分后，gateway 回归纯生成路由（desktop-bridge 服务经标准注册一行接入，WS 分支路由、路径手工匹配、身份注入、双向泵、关闭码分类全部删除）；proxy 的 bidi relay 与首帧身份重放删除；共享包中仅服务 bidi 转发的工具（双向泵、首帧重放）连同测试删除；desktop 与 fake-desktop 的 WS 客户端替换为 HTTP 流读取 + 单发提交客户端后，原 WS 客户端代码删除。仓库中不存在任何残留的旧协议路径。

**Why this priority**: 用户明确要求"重构后移除不再使用的代码"；这也是原则 II（重构式变更——架构随需求收缩）的直接体现，避免双协议并存的形式腐化。

**Independent Test**: 全仓库检索无旧端点/旧客户端/已删工具的引用；相关构建与测试全绿。

**Acceptance Scenarios**:

1. **Given** 拆分完成，**When** 检索 gateway 源码，**Then** 不存在 desktop-bridge 的 WebSocket 升级分支、路径手工匹配与双向泵代码；`/api/v2` 子树全部经生成路由分发。
2. **Given** 拆分完成，**When** 检索共享转发工具包，**Then** 仅保留仍在使用的 server-stream 转发工具；bidi 泵与首帧重放及其测试已删除。
3. **Given** 拆分完成，**When** 检索 desktop 与 fake-desktop，**Then** 不存在 WS 连接客户端；两者均以新接口工作。

---

### User Story 5 - 既有游戏与团队链路行为保持 (Priority: P2)

拆分是传输层重构，业务语义零变更：扫雷游戏全链路（init/operate/remain、截图识别、批量操作 SKIP/STOP 语义）、终局统计播报（含操作计数口径——仅统计已执行的操作）、team 物化与消息流、web 前端（不消费桥接接口，零改动）均保持现状行为。

**Why this priority**: 重构的验收基线是"行为不变"；输入 1 的统计口径核验结论（已满足）作为回归不变量并入本故事。

**Independent Test**: 既有大型测试套件（game/disconnect/stall/conversation 等）在切换后全部通过，无断言弱化。

**Acceptance Scenarios**:

1. **Given** 新接口全链路就绪，**When** 执行既有游戏大型测试（多局 won/lost、批量操作、终局统计播报），**Then** 全部通过，统计口径（仅计已执行操作）与播报内容不变。
2. **Given** 桥接接口变更，**When** web 前端与 team 对话链路运行，**Then** 行为零变化（前端不消费桥接接口；对话流与控制流本就是独立故障域，两条 HTTP 连接天然保持独立）。

### Edge Cases

- **上行与接管竞态**：接管间隙（旧流已断、新流未开）上行提交 → `FAILED_PRECONDITION` → desktop 重试恢复（D4）。
- **上行乱序到达**：帧自包含、按 `flow_result.tool_id` 关联；plan 阶段需确认全部上行帧类型（含 hold/release 类信号）无顺序依赖（调研 §5.3 遗留检查项）。
- **超限载荷**：HTTP 层显式上限守卫（清晰归因报错）+ 既有 gRPC hop 8MB 兜底；不静默截断、不破坏会话。
- **首帧前无响应头**：长生命周期流打开即推 status 首帧，避免中间层因无响应头而挂起/超时（调研 §3.1.3）。
- **识别失败路径**：结果帧到达但截图不可识别 → 既有"state 失效 + unable to recognize board"语义保持。
- **旧流晚到断开**：compare-and-delete no-op，不误删新连接注册。
- **agent 重启期间 desktop 在连**：流随服务端终止而结束，desktop 侧重连（既有重试）后恢复。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**（下行流建立与身份绑定）：系统 MUST 提供按会话寻址的长生命周期下行流接口（D1 端点形状）；打开即绑定到该会话，身份 MUST 取自资源路径（客户端提交的身份值不作为绑定依据）。
- **FR-002**（连接确认首帧）：下行流打开后服务端 MUST 立即推送一帧 status（连接确认；同时作为 desktop 首帧超时判定的依据与中间层的首响应）。
- **FR-003**（新连接接管）：同会话新下行流打开 MUST 接管连接并结束旧流；旧流的晚到断开 MUST NOT 影响新连接的注册状态（no-op 清理）。
- **FR-004**（上行单发）：系统 MUST 提供按会话寻址的上行单发接口（D2 端点形状）；每个结果/信号帧为一次独立调用，帧自包含（按 `flow_result.tool_id` 关联），MUST NOT 依赖调用间顺序。
- **FR-005**（无流拒绝）：会话无活跃下行流时，上行提交 MUST 被拒绝并返回 `FAILED_PRECONDITION`；建立下行流后重试 MUST 成功。
- **FR-006**（断连感知与故障路径）：下行流关闭将会话桌面连接置为断开；在途派发与新派发的失败语义 MUST 与现状一致（"desktop disconnected" 错误、20min backstop、abort→"aborted"）。
- **FR-007**（二进制上行与守卫）：上行载荷 MUST 以原始二进制 proto 传输（无 base64 膨胀，D3）；超过上限的载荷 MUST 被清晰归因拒绝（HTTP 腿硬上限 8MiB，正常载荷 ≤5MiB），MUST NOT 静默截断或破坏会话；帧的前向兼容性（unknown fields 保留）MUST 保持。
- **FR-008**（应用层心跳）：服务端 MUST 在下行流上周期推送 status 心跳帧（~10s 量级，D5），空闲链路不因中间层空闲超时被断开。
- **FR-009**（一次性切换与移除）：全部消费方（desktop、fake-desktop、gateway、proxy、agent_v2、testplan）MUST 在同一变更内切换到新接口（D6）；旧 bidi 端点、gateway 自研 WS 桥及其测试、proxy bidi relay 与首帧重放、共享包 bidi 泵/首帧重放工具及其测试、desktop/fake-desktop 的 WS 客户端 MUST 全部删除，仓库内 MUST NOT 残留对已删代码的引用。
- **FR-010**（业务语义回归不变量）：扫雷游戏链路、终局统计播报与操作计数口径（仅统计已执行的操作：SKIP/STOP/派发失败/init/remain 不计；分项之和恒等于总数）MUST 保持现状（核验结论：现状已满足，本 feature 不改动该口径，仅作为回归不变量验证）；team 物化/消息流与 web 前端行为 MUST 零变化。

### Key Entities *(include if feature involves data)*

- **会话下行流（Session Control Stream）**：一个会话至多一条活跃；属性：所属会话、生命周期状态（opening / active / superseded / closed）。是桌面连接状态（desktop_connected）、接管与断开清理的唯一事实源。
- **上行帧提交（Frame Submission）**：一次独立的请求-响应提交；属性：目标会话、单个结果/信号帧（含可选截图二进制）、错误语义（含无活跃流时的 `FAILED_PRECONDITION`）。
- **会话连接注册表（Session Connection Registry）**：会话 → 活跃下行流的映射；支持接管（put 覆盖）与 no-op 清理（晚到断开不误删）。
- **下行信封帧（Server Frame Envelope）**：下行流上每一帧的传输信封（含结果载荷与其错误终止标记）；desktop 客户端解包消费，异常终止时可读错误信息。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 拆分后的接口上，完整扫雷对局端到端成功——大型测试（fake-llm + fake-desktop）覆盖多局 won/lost、批量操作、终局统计播报，全部用例通过且统计内容与实际派发一致。
- **SC-002**: 断连与接管场景大型测试全部通过：接管后操作正常下发；游戏中断开后在途操作按既有故障路径收束、重连后恢复。
- **SC-003**: ≤5MiB 截图上行成功且无编码膨胀（发送的 HTTP body 字节数等于原始二进制 proto 载荷字节数，无 base64/JSON 膨胀）；>8MiB 载荷返回明确的大小错误（零静默截断用例）。
- **SC-004**: 空闲保持：下行流在无操作请求的空闲期内持续收到心跳帧，链路存活（空闲存活用例通过）。
- **SC-005**: 移除完成度：gateway 源码中 desktop-bridge 自定义 WS 路由代码为零；共享转发包中 bidi 工具为零；desktop/fake-desktop 中 WS 客户端为零（全仓库检索无引用）。
- **SC-006**: 既有服务行为回归：除桥接接口外，既有 API（team/session/memory/preset 等）大型测试全部通过，无断言弱化。

## Assumptions

- 调研决策 D1–D7（[survey/desktop-bridge-connect-split.md](../../survey/desktop-bridge-connect-split.md) §7）为用户已拍板的固定输入，本 spec 不重新开放。
- 输入 1 核验结论：扫雷终局统计的操作计数**已经**只统计已执行的操作（递增点在派发成功且识别成功之后，`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:385-388`；SKIP/STOP/派发失败/init/remain 不计，`common/js/dsh-plugins/saolei-loop/src/game/runtime.test.ts:594-630`；契约 [specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md](../065-agent-v2-team-refine/contracts/game-stats-broadcast.md) §1.2）——按用户指示忽略，无代码变更；唯一边缘情况（派发成功但识别失败的帧）会使游戏状态失效、该局无法产生终局统计，故不影响任何实际播报口径。
- 桥接接口的消费方仅有真实 desktop、fake-desktop 与大型测试（web 前端零引用，已核验）；desktop 为内部 Go 客户端，无需浏览器兼容形态（SSE 不做，D7）。
- 同仓同发布，直接切换无灰度期（D6）；testplan 大型测试作为切换后的端到端兜底。
- gRPC hop 的 8MB 消息上限、keepalive 配对、无 WriteTimeout 等既有在位配置原样保留（调研 §8 C 类）。
- 历史 fix 的迁移分类（A 类保留意图 / B 类不迁移 / C 类原样）遵循调研 §8；B 类中"URL 身份注入非防伪造"的历史意图澄清同样作为输入。
- 范围外：web 前端、SSE、对话面（`AgentService.Send`）协议、LLM 侧 chunk-idle watchdog、planner/记忆链路。
