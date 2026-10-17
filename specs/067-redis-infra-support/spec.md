# Feature Specification: Redis 基础设施支持与实例凭证注入改造

**Feature Branch**: `067-redis-infra-support`

**Created**: 2026-09-19

**Status**: Draft

**Input**: User description: "参考mongo，为 deploy 工具增加 redis（standalone 部署）infra 支持。1. deploy 配置增加 infra 支持 redis 2. 为 js 和 golang 公共库增加 redis 公共库用于连接 3. js 补上 mongo 公共库，并为当前库中的 js 服务替换 mongo 连接代码。4. 在 experimental/ 增加一个 demo，包含 js 和 golang 的 grpc 服务连接 redis，并通过 testplan 验证。5. mongo 实例对于密码注入改为使用环境变量，不再部署 secret，密码生成方式不变。reids 相同。"

## Clarifications

### Session 2026-09-19

- Q: 凭证注入改造后，服务侧公共库获取 redis/mongo 凭证的方式是保持客户端自行派生，还是改为平台向服务容器注入凭证环境变量？ → A: 仅实例侧改为环境变量直注；服务侧公共库保持客户端自行派生凭证（与实例侧同源算法），平台不向服务容器注入凭证。
- Q: "为当前库中的 js 服务替换 mongo 连接代码"的范围是否仅限 agent_v2 宿主侧的连接装配，而 preset-authoring 插件内的 mongo store 保持不动？ → A: 仅替换 agent_v2 宿主侧连接装配；preset-authoring 插件不动，其连接由宿主注入、随宿主替换自然收敛。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 在部署配置中声明 Redis 基础设施实例 (Priority: P1)

部署者在 `deploy.yaml` 中像声明 mongodb 一样声明 `resource: redis` 的 infra 服务（standalone 单实例），由 deploy 工具直接生成并管理对应的集群资源，无需手写任何基础设施清单。

**Why this priority**: redis infra 是本特性的入口能力；没有部署面支持，后续的公共库连接与 demo 验证都无从谈起。

**Independent Test**: 编写一个仅包含 redis infra 声明的 `deploy.yaml` 并执行 `deploy apply`；实例部署成功并进入就绪状态后，该能力即可独立交付。

**Acceptance Scenarios**:

1. **Given** `deploy.yaml` 中声明 `infra: { resource: redis, profile: <已定义 profile>, name: redis, app: <app> }`，**When** 部署者执行 apply，**Then** 环境内创建一个就绪的 redis standalone 实例，且环境详情（describe/list）中展示该 infra 服务及其状态。
2. **Given** `deploy.yaml` 中声明 `resource: redis` 且附带 `persistence.enabled: true`，**When** 部署者执行 apply，**Then** 实例按 profile 的存储配置持久化数据，重新部署后数据保留。
3. **Given** `deploy.yaml` 中声明了未支持的 resource 类型（如 `mysql`），**When** 部署者执行 apply，**Then** 配置校验拒绝该部署并明确报错（既有校验语义扩展到 redis 后保持不变）。
4. **Given** 环境中已部署 redis infra，**When** 部署者从 `deploy.yaml` 中移除该服务后再次 apply（全量替换语义），**Then** 该实例及其配套资源被自动清理。

---

### User Story 2 - 服务开发者通过 Go/JS 公共库连接 Redis (Priority: P1)

服务开发者（Go 或 JS）在服务代码中通过统一的 redis 公共库按 dominion target（`app/name`）建立连接：公共库自动完成实例地址解析与凭证派生，开发者不需要在代码或配置中保存任何连接串或密码。

**Why this priority**: 公共库让"部署的 redis 实例"变成"服务可直接消费的能力"，是部署面价值兑现的关键一步；Go 与 JS 双语言对齐仓库既有服务形态。

**Independent Test**: 在任一已有服务中引入公共库，使用 target 建立连接并完成一次读写；对公共库自身的单测（地址解析、凭证派生、连接构造）即可独立验证。

**Acceptance Scenarios**:

1. **Given** 环境中存在 app `A` 的名为 `cache` 的 redis 实例，**When** 同环境服务以 target `A/cache` 调用公共库建立连接，**Then** 连接建立成功且可完成读写。
2. **Given** 使用公共库的服务，**When** 开发者检查其代码与部署配置，**Then** 不存在硬编码的 redis 地址、密码或连接串。
3. **Given** redis 实例启用了密码认证（见 User Story 4），**When** 公共库建立连接，**Then** 连接携带正确派生的凭证并通过认证。

---

### User Story 3 - JS 服务通过公共库连接 Mongo，存量代码收敛 (Priority: P2)

JS 侧补齐 mongo 公共库（与 Go 公共库行为一致），并将当前仓库中自带 mongo 连接代码的 JS 服务（agent_v2 的 preset 存储连接装配）替换为使用该公共库；对外行为保持不变。

**Why this priority**: agent_v2 目前在业务代码内复制了 Go 的凭证派生与地址解析算法（`projects/game/agent_v2/src/presets.ts`），存在双语言算法漂移风险；公共库化是消除该风险的收敛动作，优先级低于新增 redis 能力。

**Independent Test**: JS mongo 公共库自身的单测独立验证；替换后运行 agent_v2 既有单测与构建，行为不回退。

**Acceptance Scenarios**:

1. **Given** JS mongo 公共库，**When** 以 dominion target 建立连接，**Then** 地址解析与凭证派生行为与 Go 公共库一致，可连接平台部署的 mongo 实例。
2. **Given** agent_v2 服务，**When** 完成替换，**Then** 其 mongo 连接（preset 存储）改由公共库提供，直接注入连接串的既有优先级行为保留，业务功能不变。
3. **Given** 替换后的仓库，**When** 检索 JS 业务代码中的 mongo 凭证派生实现，**Then** 该算法仅存在于公共库中，业务代码无副本。

---

### User Story 4 - 实例凭证改为环境变量注入，不再部署 Secret (Priority: P2)

平台为部署的 mongo/redis 实例生成的认证凭证（确定性生成方式不变），从"部署独立 Secret 对象并以 SecretKeyRef 引用"改为"直接以环境变量值注入实例容器"；redis 实例同样采用该方式。

**Why this priority**: 凭证注入方式简化减少一类被管理资源；由于密码生成方式不变、客户端凭证派生不变，属于内部实现收敛，优先级低于两个 P1。

**Independent Test**: 部署一个 mongo 或 redis 实例，检查集群中不再创建实例专属 Secret 对象、实例容器环境变量中包含正确凭证、实例认证与连接行为正常。

**Acceptance Scenarios**:

1. **Given** 部署一个 mongodb infra 实例，**When** 部署完成，**Then** 集群中不存在该实例的 Secret 对象，实例容器以环境变量直接获得用户名与密码，实例按既有确定性密码启用认证。
2. **Given** 部署一个 redis infra 实例，**When** 部署完成，**Then** 同样不存在实例 Secret 对象，凭证以环境变量注入，实例启用密码认证。
3. **Given** 凭证注入方式变更前已部署的 mongo 实例，**When** 使用新版工具对其重新 apply，**Then** 实例继续使用相同密码（生成方式不变），存量 Secret 被清理，服务连接不中断。
4. **Given** 服务侧公共库，**When** 派生实例凭证，**Then** 派生结果与实例侧注入的凭证一致（连接认证成功即为证明）。

---

### User Story 5 - 通过 demo 与 testplan 端到端验证 Redis 全链路 (Priority: P3)

使用者在 `experimental/` 获得一个包含 Go 与 JS 两个 gRPC 服务连接同一 redis 实例的 demo，并通过 testplan（guitar）完成部署→测试→清理的闭环验证；demo 同时作为两种语言服务接入 redis 公共库的参考实现。

**Why this priority**: demo 是前述能力的集成验收与文档化样例，依赖 User Story 1/2 完成，作为验收收尾。

**Independent Test**: 执行 `guitar run <plan.yaml>`，测试计划完成自动部署与清理，所有用例通过。

**Acceptance Scenarios**:

1. **Given** demo 的测试计划，**When** 执行 `guitar run`，**Then** 自动部署 Go/JS 服务与 redis 实例，执行全部用例后清理环境，全部用例通过。
2. **Given** demo 中的 Go 服务与 JS 服务，**When** 各自通过公共库连接同一 redis 实例，**Then** 一方写入的数据可被另一方读取（跨语言数据一致性）。
3. **Given** demo 部署拓扑，**When** 测试通过外部入口访问服务接口，**Then** 服务对 redis 的读写行为经接口可观测（如写入后读取返回一致）。

### Edge Cases

- redis infra 声明了不存在的 profile 时，部署必须明确报错（对齐 mongodb 既有语义）。
- `persistence.enabled: true` 但容量非法（超上限/无法解析）时，配置校验拒绝（对齐既有 infra 持久化校验）。
- 实例名/app/环境名组合超过资源名长度限制时，校验拒绝（对齐既有命名约束）。
- 公共库连接时目标实例不存在或未就绪：返回明确错误，不静默失败。
- 直接注入连接串（如 `MONGO_URI`）覆盖公共库派生行为的既有优先级必须保留，避免破坏本地开发与测试路径。
- 凭证注入方式切换时（存量环境滚动到新方式），密码不变，不能出现认证失效导致的实例不可用。
- 同一环境同时声明 mongodb 与 redis infra 时互不干扰，均能正常部署与清理。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: deploy 配置（`deploy.yaml`）MUST 支持声明 `resource: redis` 的 infra 服务，部署 standalone 单实例 redis，配置字段（resource/profile/name/app/persistence）与 mongodb infra 保持一致。
- **FR-002**: redis infra MUST 复用既有 infra 校验语义：未知 resource 拒绝、profile 必须存在、持久化容量校验、命名与长度约束。
- **FR-003**: redis 实例 MUST 支持 persistence 配置：启用时按 profile 存储配置持久化（StorageClass/容量/访问模式），未启用时使用实例本地存储。
- **FR-004**: redis 实例 MUST 以与 mongodb 实例一致的方式被同环境服务发现（服务名解析机制）与访问。
- **FR-005**: redis 实例 MUST 启用密码认证；密码按既有确定性算法生成（算法与材料约定与 mongo 区分），并以环境变量直接值注入实例容器。
- **FR-006**: mongodb 实例的凭证（用户名取 profile 配置，密码同输入同密码）MUST 从 SecretKeyRef 引用改为环境变量直接值注入；密码生成方式与当前保持不变。
- **FR-007**: deploy 工具 MUST 不再为 mongo/redis 实例创建、管理实例 Secret 资源；对存量实例 Secret MUST 在后续调和中清理。
- **FR-008**: Go 公共库 MUST 提供 redis 连接包：输入 dominion target（`app/name`），完成地址解析、凭证派生与客户端连接建立，并提供可测试性注入点（对齐 `common/gopkg/mongo` 模式）。公共库 MUST 在客户端侧自行派生凭证（与实例侧同源算法），平台不向服务容器注入 redis/mongo 凭证。
- **FR-009**: JS 公共库 MUST 提供 redis 连接包，能力与 Go 公共库对齐（target 解析、凭证派生、连接建立）。
- **FR-010**: JS 公共库 MUST 提供 mongo 连接包，地址解析与凭证派生行为与 Go 公共库（`common/gopkg/mongo`）一致。
- **FR-011**: 仓库内现有自带 mongo 连接代码的 JS 服务（agent_v2 preset 存储的宿主侧连接装配）MUST 替换为使用 JS mongo 公共库；既有对外行为（连接串直连优先、数据隔离）MUST 保持不变。preset-authoring 插件的 mongo store 不在替换范围内（连接由宿主注入，随宿主替换自然收敛）。
- **FR-012**: `experimental/` MUST 新增一个 demo：包含 golang 与 js 的 gRPC 服务，均连接同一 redis 实例并暴露可验证接口，并附 testplan 测试计划。
- **FR-013**: demo 的 testplan MUST 通过 testplan skill 实际执行（`guitar run <plan.yaml>`）完成部署→测试→清理闭环，且全部用例通过后方可验收。
- **FR-014**: redis infra 的 describe/list 观测（服务状态展示、per-service rollout 状态）MUST 与 mongodb infra 语义一致。

### Key Entities *(include if feature involves data)*

- **Infra 服务声明**：`deploy.yaml` 中 `services[].infra`，字段 resource/profile/name/app/persistence；resource 取值集合从 {mongodb} 扩展为 {mongodb, redis}。
- **Redis 实例（standalone）**：单实例基础设施服务，含工作负载、访问入口（Service）、可选持久化卷；实例档案（profile）含镜像/版本/端口/认证用户/存储配置。
- **实例凭证**：由确定性算法从（app、环境名、服务名、资源种类）派生的密码；仅以环境变量形式注入实例容器。
- **公共库连接入口**：以 dominion target（`app/name`）为输入，组合"地址解析 + 凭证派生 + 客户端连接"三步的统一入口（Go/JS 双语言）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 包含 redis infra 的部署在单次 apply 内完成，实例进入就绪状态；环境 describe/list 正确展示实例与状态。
- **SC-002**: redis demo 的 testplan 实际执行一轮完整闭环（部署→测试→清理），全部测试用例通过，覆盖 Go 与 JS 两个服务的 redis 读写及跨语言数据一致性。
- **SC-003**: 凭证注入方式切换后，新建与存量的 mongo/redis 实例均保持原密码（生成方式不变）：存量 mongo 环境重新 apply 后旧实例 Secret 被删除且连接不中断；demo 用例 1–4 全通过证明 redis 客户端派生凭证与实例注入一致；集群中不再存在实例专属 Secret 对象。（判定依据见 FR-005–FR-007 及 `specs/067-redis-infra-support/quickstart.md` 场景 5。）
- **SC-004**: JS mongo 公共库替换 agent_v2 连接代码后，agent_v2 全部既有单测与构建通过，对外行为零变化。
- **SC-005**: 仓库整体构建与测试（`bazel build` / `bazel test` 相关目标，目标集合见 `specs/067-redis-infra-support/quickstart.md` 场景 1）全部通过；deploy 工具既有配置校验用例（含 mongodb 用例）及既有 `experimental/golang/mongo_demo` 相关目标无回归。

## Assumptions

- redis 仅提供 standalone 单实例形态（无主从/哨兵/cluster），与 mongodb 单实例形态对齐。
- redis 采用密码认证（默认用户，无自定义用户名），凭证为按既有确定性算法派生的密码；算法与 mongo 相同、密钥材料按资源种类区分（同输入同密码，跨语言一致）。
- 凭证流向：平台仅在实例容器内以环境变量注入凭证；服务侧公共库在客户端按同源算法自行派生，平台不向服务容器注入 redis/mongo 凭证、不感知服务与基础设施的依赖关系。
- redis 的 profile 定义进入平台静态配置（对齐 `mongodb.dev-single` 模式），首个 profile 面向开发/测试场景。
- 存量已部署 mongo Secret 的清理由 deploy 的调和机制（prune 按 `dominion.io/environment` + `app.kubernetes.io/managed-by` 标签删除非期望资源）承担，无需人工逐环境处理。
- redis 客户端使用各语言官方维护的标准客户端库（Go/JS 各一），服务侧连接行为由公共库封装、不暴露给业务代码；依赖版本统一纳入仓库依赖管理（catalog / go.mod）。
- "当前库中的 js 服务"指 `projects/game/agent_v2`（preset 存储连接装配）；`common/js/dsh-plugins/preset-authoring` 的连接由宿主注入，随宿主替换自然收敛，不单独改造。
- demo 的 SUT 遵循大型测试规范：默认不启用持久化，使用既有测试域名/网关约定。
- 本特性不改变现有服务 secret（用户业务 Secret 绑定）机制，仅涉及平台自管的基础设施实例凭证。
