# Feature Specification: Proto 契约修正 — 资源父级字段合规与帧方向拆分

**Feature Branch**: `035-proto-contract-refine`

**Created**: 2026-08-03

**Status**: Draft

**Input**: User description: "1. 修正 game.proto 和 deploy.proto 里 resource 包含 parent 不合规的问题。Session.template 和 TeamProfile.template 的理由不充分——TeamProfile.template 作为 double check 实际是不信任 url 当中的 template 参数（oneof 一致性是与 template 的一致，如果 url 中的 template 是正确的那就不需要 TeamProfile.template)，这种校验完全是多余且不符合接口设计原则的。2. 将 TeamService.Connect 的入参和出参拆分为服务端发送帧和客户端发送帧，不把两者的请求混在一个对象里，新的两个帧对象都只包含自己所发送消息内容，降低复杂度。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 移除资源体内可从名称派生的冗余字段 (Priority: P1)

`Session.template`（OUTPUT_ONLY，从 parent 派生）和 `TeamProfile.template`（REQUIRED，客户端设置）都是与资源名称 pattern 中 template 段重复的字段。经代码调查确认：`Session.template` 在前端从未被读取（前端路由/显示全部使用本地常量与 `session.name`）；`TeamProfile.template` 的 "double-check" 校验是不信任 URL 路径参数的多余校验——若 URL 中的 template 是权威来源，则资源体中不需要一个与之重复的字段来二次确认。

同类问题还存在于 `Session.session_id`（OUTPUT_ONLY，field 3）：其值始终等于 `Session.name` 的最后一段（resource ID），可从 name 派生。虽与 template（父级冗余，违反 AIP-124）性质略有不同——session_id 是资源自身 ID 冗余——但遵循同一"资源体不携带可从名称派生的字段"原则，应一并移除。

依据 [AIP-124 Resource association](https://google.aip.dev/124)：「A resource **must** have at most one canonical parent」—— 规范父级应由资源名称 pattern 表达，不应在资源体内冗余一个与 name 段重复的字段。同理，[AIP-133 Standard methods: Create](https://google.aip.dev/133) 规定 `parent` 属于请求消息而非资源本身。

> **deploy.proto 调查结论**：deploy.proto 中的资源（Scope、Environment）不包含冗余父级字段，已合规。ServiceEndpoints 的 `resolved_scope` / `resolved_environment` 经确认**不是**冗余字段——它们在 PROD_FALLBACK 场景下表示端点实际解析自的**另一个**环境，值与 name 中的段不同，是有意义的解析诊断输出。因此本需求 Part 1 仅涉及 game.proto 的变更。

**Why this priority**: 接口契约的合规性是协作边界的基础；冗余字段增加了消费者理解成本、维护成本，且 TeamProfile.template 的多余校验逻辑违反接口设计原则，应优先修正。

**Independent Test**: 移除 `Session.template`、`Session.session_id` 和 `TeamProfile.template` 后，所有现有 RPC（Create/Get/List/Update/Delete TeamProfile、Create/Get/List Session、CreateTeam 及其 oneof 一致性校验）行为不变，可通过现有单测与大型测试验证。

**Acceptance Scenarios**:

1. **Given** 一个 CreateTeamProfile 请求（parent="templates/saolei"，oneof spec 为 SaoleiProfile），**When** 服务端处理请求，**Then** 服务端从 parent URL 路径段推导 template（"saolei"），校验 oneof variant 与该 template 一致，不读取资源体中的 template 字段。
2. **Given** 一个 CreateSession 请求（parent="templates/saolei"），**When** 服务端创建 Session 并返回响应，**Then** 响应中的 Session 不包含 template 字段；前端使用 session.name（含 template 段）做路由与显示，功能不受影响。
3. **Given** 一个 UpdateTeamProfile 请求，**When** 客户端提交更新，**Then** 客户端不需要在资源体中设置 template 字段；服务端从资源名称路径段推导 template 并做 oneof 一致性校验。
4. **Given** 一个 GetSession / ListSessions 响应，**When** 客户端收到 Session 资源，**Then** Session 不包含 session_id 和 template 字段；desktop view-model 从 `game.ParseSessionName(name)` 派生 sessionId 并保持 Wails JSON 形状（`sessionId`）不变，前端零改动。

---

### User Story 2 - 拆分 Connect 双向流帧为方向独立的消息 (Priority: P1)

当前 `TeamService.Connect(stream AgentFrame) returns (stream AgentFrame)` 在入参和出参中复用同一个 `AgentFrame` 消息。经调查，两个方向实际使用的字段存在显著不对称：

- `frame_id` 和 `create_time` 在客户端→服务端方向被服务端**完全忽略**，仅在服务端→客户端方向被前端消费（帧去重、时间戳）。
- `sender` 字段是方向性的：入站几乎总是 USER，出站是 AGENT（显示）或 SYSTEM（控制）；服务端依赖 `sender===USER` 做路由门控。
- agent 的 operation-bridge 发送的操作帧甚至**未设置** frame_id/create_time/sender 等信封字段，说明统一信封结构本身就不被遵守。

将入参和出参拆分为两个方向独立的消息（用户帧 / 团队帧），每个消息只包含该方向的发送方实际需要设置、接收方实际需要消费的字段，消除"一个对象适配两个方向"的复杂度。

方向拆分使 `FrameSender` 枚举变得冗余：入站帧天然来自用户；出站帧中 `messageParts` 天然是 agent 产出的显示内容，`flowParts` 天然是系统/控制信号。前端当前仅对 `messageParts` 用 `sender` 做气泡对齐，而实时流中 TeamFrame 的 `messageParts` 恒为 AGENT（`SeedFromHistory` 重放的用户消息除外，其 `role=USER`）——因此 `FrameSender` 枚举可随之移除，前端按 `TeamFrame.role`（user 右 / agent 左）+ payload 类型（flowParts→不渲染为对话条目）即可还原现有渲染行为。

**Why this priority**: 帧消息是实时通信的核心契约；当前混合对象使两端代码都必须面对不相关字段的噪音，降低可读性且容易产生不一致（如 operation-bridge 不设信封字段的缺陷）。拆分后每端只看到自己需要的内容，契约更清晰。

**Independent Test**: 拆分后，端到端的实时通信（用户输入→agent 回复→桌面执行操作→结果回报）全链路行为不变，可通过大型测试验证。

**Acceptance Scenarios**:

1. **Given** 桌面客户端通过 WebSocket 发送用户消息，**When** gateway 将其转换为用户帧并转发给 agent，**Then** 用户帧只包含用户有意义的字段（如目标 agent、消息内容），不包含服务端才使用的信封字段（如 frame_id 去重键）。
2. **Given** agent 处理完一轮对话产出回复，**When** agent 通过流发送团队帧，**Then** 团队帧包含前端消费所需的全部字段（帧标识、来源 agent、消息内容），不包含用户才设置的入站专用字段，也不包含 `sender` 枚举字段。
3. **Given** agent 的 operation-bridge 发送操作执行请求，**When** 桌面客户端收到团队帧，**Then** 该帧包含完整的、与显示/控制帧一致的信封字段，不存在"部分字段缺失"的不一致问题。
4. **Given** 前端渲染对话消息，**When** 收到团队帧，**Then** 前端能正确读取帧标识（去重）、来源 agent（分 tab 路由）、消息内容与时间戳；messageParts 按既定 agent 气泡样式渲染，无需依赖 `sender` 字段。

---

### Edge Cases

- 移除 `TeamProfile.template` 后，CreateTeamProfile 的 `{resource}_id` 与 parent 的 template 段仍由各自承担（parent 提供 template，resource_id 提供 profile ID）；UpdateTeamProfile 的 template 从资源名称路径段推导——需确保 URL 路径中的 template 段始终是权威来源，handler 不再依赖资源体字段。
- `TeamProfile` 的 oneof spec 与 template 的一致性校验逻辑需保留，但其数据源从"资源体 template 字段"改为"资源名称路径段"。
- 移除 `Session.session_id` 后，desktop view-model 需从 `game.ParseSessionName(name)` 派生 sessionId（复用 `Team` 资源已有的 `teamViewFromProto` 模式，`projects/game/desktop/view_model.go:153-173`），保持 Wails JSON 的 `sessionId` 字段不变以实现前端零改动。
- `FrameSender` 枚举被两个 message 使用（`AgentFrame.sender` 和 `Message.sender`）；帧方向拆分使帧中的 sender 冗余，但 `Message.sender`（持久化历史消息的发送方标识）有独立的、不可替代的用途（前端气泡对齐、pending 标记、历史重放）。完全移除 `FrameSender` 后，`Message` 需引入专用 `MessageRole` 枚举（如 `MESSAGE_ROLE_UNSPECIFIED`/`MESSAGE_ROLE_USER`/`MESSAGE_ROLE_AGENT`）替代。现有 history 中 tool 消息被推导为 `FRAME_SENDER_SYSTEM`（`projects/game/agent/src/handler.ts:589`），但 live 流中 tool_result 帧为 `FRAME_SENDER_AGENT`（`projects/game/agent/src/turn-loop.ts:462`）——此预存不一致应在 MessageRole 迁移时统一（tool 消息归为 AGENT，与 live 行为对齐）。
- 帧拆分后，gateway/proxy 作为转发层需适配两种帧类型（入站用客户端帧、出站用服务端帧），不得改变其透明转发的语义。
- 前端（Svelte）当前以 protojson 形式消费帧字段（frameId、sender、agent、createTime、messageParts/flowParts），帧结构变更后需同步适配前端字段读取；移除 `FrameSender` 后，messageParts 一律按 agent 气泡渲染，flowParts 不渲染为对话条目（与现有"控制块不渲染"的语义一致）。
- `AgentFrame` 和 `FrameSender` 被引用的 proto 注释、contracts 文档需同步更新。

## Requirements *(mandatory)*

### Functional Requirements

**Part 1: 资源父级字段合规**

- **FR-001**: `Session` 资源 MUST NOT 包含 `template` 字段；template 信息仅由资源名称（`templates/{template}/sessions/{session}`）的路径段承载。
- **FR-002**: `TeamProfile` 资源 MUST NOT 包含 `template` 字段；template 信息仅由资源名称（`templates/{template}/profiles/{profile}`）的路径段承载。
- **FR-003**: CreateTeamProfile handler MUST 从请求 parent（URL 路径段）推导 template，校验 oneof spec variant 与该 template 的一致性，不读取资源体中的 template 字段。
- **FR-004**: UpdateTeamProfile handler MUST 从资源名称路径段推导 template 做 oneof 一致性校验。
- **FR-005**: 服务端创建 Session 时 MUST 从 parent URL 路径段推导 template 并落库（domain 层的 path segment 存储），与现有行为一致；proto 响应不再输出 template 字段。
- **FR-006**: 所有引用 `Session.template` / `TeamProfile.template` 的 Go 代码（handler、view model、desktop client）和 TypeScript 代码（agent、desktop frontend）MUST 移除对该字段的读写。
- **FR-007**: `Session` 资源 MUST NOT 包含 `session_id` 字段；session ID 仅由资源名称的路径段承载。
- **FR-008**: desktop view-model MUST 从 `game.ParseSessionName(session.name)` 派生 sessionId（复用 `Team` 资源已有的 `teamViewFromProto` 模式），保持 Wails JSON 的 `sessionId` 字段形状不变，实现前端零改动。

**Part 2: 帧方向拆分**

- **FR-009**: `TeamService.Connect` 的入参 MUST 定义为用户帧消息（UserFrame），仅包含用户实际设置、服务端实际消费的字段。
- **FR-010**: `TeamService.Connect` 的出参 MUST 定义为团队帧消息（TeamFrame），仅包含服务端实际设置、用户实际消费的字段。
- **FR-011**: 用户帧 MUST NOT 包含仅服务端→用户方向使用的字段（如帧去重标识 frame_id，因服务端不消费入站帧的该字段）。
- **FR-012**: 团队帧 MUST 包含前端消费所需的全部字段：帧标识（去重）、来源 agent（分 tab 路由）、时间戳、消息内容。团队帧 MUST NOT 包含 `sender` 字段。
- **FR-013**: 团队帧的所有发送点（显示帧、控制信号帧、operation-bridge 操作帧）MUST 设置完整的信封字段，消除 operation-bridge 不设信封字段的现有缺陷。
- **FR-014**: gateway（WebSocket↔gRPC 转换）MUST 适配方向独立的帧类型：WebSocket 入站消息→用户帧，团队帧→WebSocket 出站消息，保持透明转发语义不变。
- **FR-015**: proxy 转发层 MUST 适配方向独立的帧类型（用户帧入站、团队帧出站），保持透明转发语义不变。
- **FR-016**: 桌面客户端（Go）MUST 使用用户帧构造出站消息、团队帧解析入站消息，保持现有功能（用户消息发送、操作结果回报、连接探活）不变。
- **FR-017**: agent 服务端（TypeScript）MUST 使用用户帧解析入站消息、团队帧构造出站消息，保持现有功能（turn loop、operation dispatch、信号响应）不变。
- **FR-018**: 前端（Svelte）MUST 适配团队帧的字段结构（protojson 序列化后的 JSON 字段），保持消息渲染、agent 分 tab 路由、帧去重等功能不变；MUST NOT 依赖 `sender` 字段，气泡对齐改由 `TeamFrame.role` 判定（实时 messageParts 帧恒 AGENT；`SeedFromHistory` 重放帧携带 `Message.role` 副本）——user 右 / agent 左；flowParts 一律不渲染为对话条目。
- **FR-019**: `FrameSender` 枚举 MUST 被完全移除；用户帧与团队帧均 MUST NOT 包含 `sender` 字段。所有引用 `FrameSender` 的 Go 代码（gateway、proxy、desktop client）、TypeScript 代码（agent、desktop frontend）及 proto 注释 MUST 同步移除对该枚举及帧 `sender` 字段的读写与引用。
- **FR-020**: `Message` 资源 MUST 引入专用 `MessageRole` 枚举（如 `MESSAGE_ROLE_UNSPECIFIED` / `MESSAGE_ROLE_USER` / `MESSAGE_ROLE_AGENT`）替代原 `FrameSender` 类型的 `sender` 字段；agent 服务端 ListMessages 推导逻辑（从 LangChain 消息类型推导角色）MUST 使用新枚举，tool 消息归为 AGENT（与 live 流 `projects/game/agent/src/turn-loop.ts` 中 tool_result 帧的 AGENT 标记对齐，消除预存不一致）。前端及 desktop view-model MUST 适配新的 `MessageRole` 字段。

### Key Entities *(include if feature involves data)*

- **Session**: 移除 template 和 session_id 字段后的 per-template 会话容器。template 与 session ID 均仅由名称路径段承载；desktop view-model 从 `ParseSessionName` 派生 sessionId。
- **TeamProfile**: 移除 template 字段后的 per-template 团队配置。template 仅由名称路径段承载；oneof spec variant 与 template 的一致性校验数据源改为资源名称路径段。
- **MessageRole**: `Message` 资源专用的发送方角色枚举（替代被移除的 `FrameSender`），用于持久化历史消息的发送方标识，供前端做气泡对齐与渲染区分。
- **用户帧**（UserFrame）: 用户（桌面客户端）→服务端方向的实时通信传输单元，仅包含用户设置的、服务端消费的字段（路由标识、目标 agent、消息内容）。
- **团队帧**（TeamFrame）: 服务端→用户（桌面客户端）方向的实时通信传输单元，仅包含服务端设置的、用户消费的字段（帧标识、来源 agent、时间戳、消息内容）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `Session`（template + session_id）和 `TeamProfile`（template）资源消息中不再存在任何可从资源名称路径段派生的冗余字段，符合 [AIP-124](https://google.aip.dev/124) 规范。
- **SC-002**: CreateTeamProfile / UpdateTeamProfile / CreateSession / GetSession / ListSessions 的端到端行为（创建、校验、oneof 一致性）在移除 template / session_id 字段后与变更前完全一致，全部现有单测通过。
- **SC-003**: `TeamService.Connect` 的 RPC 签名使用方向独立的两种帧消息，不再共用单一消息类型。
- **SC-004**: 端到端实时通信全链路（用户输入→agent 回复→桌面执行操作→结果回报→前端渲染）在帧拆分后功能完整，大型测试全部通过。
- **SC-005**: 团队帧的所有发送点（含 operation-bridge）设置一致的信封字段，不存在"部分字段缺失"的不一致。

## Assumptions

- URL 路径中的 template 段是权威来源，gateway 从 connect URL 注入 session_id/template_id 的行为不变（帧拆分后仍由 gateway 在用户帧上覆盖路由字段）。
- 现有大型测试（`testplan/` 目录下的 session、team、connect 相关测试计划）覆盖了实时通信与资源管理的端到端流程，可作为帧拆分与字段移除的回归验证基础。
- `AgentFrame` 与 `FrameSender` 在 clean break 下直接删除，其历史字段编号（4/5 invoke_id/sequence、10/20/21/22 旧 payload）不迁移，`UserFrame`/`TeamFrame` 按各自的编号空间（1~6 + 10/11 oneof）重新定义，不需跨消息保留 `reserved` 一致性。
- deploy.proto 不需要变更（经调查确认无冗余父级字段问题）。
- `MessageRole` 枚举替代 `FrameSender` 后，history 中 tool 消息的角色从 SYSTEM 统一为 AGENT（与 live 流行为对齐）；前端 tool 消息渲染不依赖 sender 分支（调查确认 `ChatView.svelte` 的 tool-bubble 渲染不按 sender 区分），因此此变更不影响渲染。
- `SeedFromHistory`（desktop 内部将历史 Message 转成帧形状重放给前端的机制）需适配 MessageRole：重放时 user 消息仍需在 SSE 帧中携带角色标识，供前端做气泡对齐。
