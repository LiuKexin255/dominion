# Feature Specification: Deploy Health 探针支持

**Feature Branch**: `052-deploy-health-probe`

**Created**: 2026-09-01

**Status**: Draft

**Input**: User description: "为 deploy 工具增加 health 探针支持
1. deploy 工具部署时，增加 startupProbe 和 livenessProbe 探针，端口固定为 38080, path: /healthz。是否启动依据改为探针，k8s 应该可以自动适配，应该不需要修改 deploy 服务检查逻辑。
2. bootstrap 工具（go 和 js 语言都有）负责 health探针实现，在所有 Component启动后，启动 health 服务。bootstrap 停止/退出时，先停止 health。即 health 服务遵循先进先出原则。
3. health 端口作为约定而非强制校验，不增加任何端口相关的校验代码。"

## Clarifications

### Session 2026-09-01

- Q: 当 health 服务启动失败（例如本地开发时端口 38080 被其他服务占用）时，bootstrap 应该如何处理？ → A: 视同组件启动失败：bootstrap 回滚已启动组件并退出，报错原因清晰（与其他 Component 失败语义一致）。
- Q: 本需求的适配范围是否包含修改现存 service 代码？ → A: 不包含；仅 `experimental/` 目录下的服务可用于测试/验证（允许为其适配），其余 service 代码一律不在本需求内修改。
- Q: JS 服务 agent/agent_v2 是否接入共享 health helper 以保持 system_test 全绿？ → A: 不接入（严格不修改 service 代码）；接受依赖其部署的现有大型测试套件（system_test.yaml 全部 suite）部署失败，作为已知范围外后果由后续适配工作解决。
- Q: JS 侧是否交付共享 health 公共库？ → A: 不交付；移除 JS 相关公共库内容，统一的 JS bootstrap 公共库为后续独立工作。本需求中用于测试的 JS 服务（`experimental/`）自行实现 health 探针。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 部署的服务具备真实就绪与自动自愈能力 (Priority: P1)

平台开发者通过 deploy 工具部署服务后，服务的"是否启动/是否存活"判定依据从"进程存在"升级为"探针通过"：只有 health 端点可正常响应的服务副本才被计入就绪；服务进程挂死或无响应时容器被自动重启；初始化耗时的服务在启动预算内不会被误杀。

**Why this priority**: 这是本特性的核心价值——让部署状态真实反映服务健康，并提供无人工干预的自愈能力。deploy 工具侧只需附加探针，判定由 k8s 原生机制自动适配。

**Independent Test**: 部署一个实现了 health 端点的服务，确认 rollout 就绪依赖探针通过；人为使服务停止响应健康检查，确认容器被自动重启。

**Acceptance Scenarios**:

1. **Given** 一个已实现 health 端点（约定端口 38080、路径 /healthz）的服务被 deploy 工具部署，**When** 服务完成启动并正常响应健康检查，**Then** 该副本被计入就绪，环境状态转为 READY。
2. **Given** 服务已正常运行，**When** 服务进程挂死（不再响应健康检查），**Then** 容器因 livenessProbe 失败被自动重启，无需人工干预。
3. **Given** 一个初始化耗时的服务，**When** 启动耗时超过 liveness 探针常规容忍时间但在 startupProbe 预算内，**Then** 容器不被误杀，启动完成后正常就绪。

---

### User Story 2 - bootstrap 生态自动提供 health 端点 (Priority: P2)

使用 Go 共享 bootstrap 的服务开发者无需自行实现健康检查：bootstrap 在所有 Component 启动成功后自动启动 health 服务；bootstrap 停止/退出时先停止 health 服务，再停止其他 Component（health 服务的存活窗口嵌套在所有组件的存活窗口之内，遵循先进先出原则）。JS 侧统一的 bootstrap 公共库为后续独立工作；本特性中用于验证的 JS 服务（`experimental/`）在自身代码中按同一约定自行实现 health 端点。

**Why this priority**: 消除各服务重复实现健康检查的成本，并保证生命周期顺序正确——health 只在"全部组件已启动"后可见，并在任何组件停止前先下线。

**Independent Test**: 任一使用 Go 共享 bootstrap 的服务本地启动完成后，访问约定端口/path 得到成功响应；触发退出信号后确认 health 先于其他组件停止。

**Acceptance Scenarios**:

1. **Given** 一个使用 bootstrap 的服务已完成所有 Component 启动，**When** 探测约定端口 38080、路径 /healthz 的 health 端点，**Then** 返回成功响应（表明所有组件均已启动、进程存活）。
2. **Given** 一个使用 bootstrap 的服务收到停止/退出信号，**When** 执行退出流程，**Then** health 服务先于其他任何 Component 停止。
3. **Given** 某个 Component 启动失败导致 bootstrap 回滚，**When** 回滚执行，**Then** health 服务尚未启动（或已先行停止），退出后不残留任何监听。

---

### User Story 3 - deploy 状态检查行为保持不变 (Priority: P3)

deploy 工具现有的服务状态检查与 rollout 判定行为保持不变：判定依据由 k8s 探针机制自然接管（探针结果影响就绪副本数），deploy 侧无需也不应修改检查逻辑。

**Why this priority**: 用户明确要求不修改 deploy 服务检查逻辑——通过 k8s 原生适配将改动面收敛到"附加探针"一处，降低风险与维护成本。

**Independent Test**: 在 deploy 状态检查逻辑零修改的前提下，部署未实现 health 端点的服务，确认其 rollout 不被判定 READY（证明判定依据已切换为探针）。

**Acceptance Scenarios**:

1. **Given** deploy 工具为服务生成的容器定义，**When** 检查生成的部署清单，**Then** 其中包含指向约定端口 38080、路径 /healthz 的 startupProbe 与 livenessProbe。
2. **Given** 一个未实现 health 端点的服务被部署，**When** 探针持续失败，**Then** 该服务不被计入就绪，环境状态保持 WAITING（或按既有超时语义转为 FAILED），且 deploy 工具状态检查逻辑无任何修改。

---

### Edge Cases

- **未适配服务被部署（含仓库内现存服务）**：`experimental/` 之外的现存服务代码不在本特性内修改，其中未满足约定的服务（JS 生产服务 agent/agent_v2 等）部署后探针持续失败、无法就绪——已知范围外后果，依赖其部署的测试套件将失败，适配由后续工作承担；未来新增的服务同样遵循约定、自行负责。
- **health 端点响应缓慢**：探针按自身的超时与失败阈值判定，达到阈值即触发既定的重启/不就绪行为。
- **Component 启动失败回滚**：health 在全部 Component 启动成功后才会启动，因此回滚场景下 health 尚未存在，不会产生残留监听。
- **端口 38080 被占用（如本地同主机运行多个服务）**：端口是约定而非强制校验，本特性不增加任何端口冲突/声明校验代码；health 服务绑定失败按 FR-010 视同组件启动失败处理，冲突由服务开发者自行规避。
- **优雅退出超时或异常退出**：health 已在停止序列首位先行停止；探针失败触发的容器重启与正常退出流程互不干扰。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: deploy 工具为部署生成的服务容器 MUST 附加 startupProbe 与 livenessProbe，两者均指向固定约定：端口 38080、路径 /healthz。
- **FR-002**: 探针 MUST 以固定约定的方式附加，不引入服务侧声明、开关或配置项；stateless（Deployment）与 stateful（StatefulSet）工作负载 MUST 同等附加。
- **FR-003**: deploy 工具现有的服务状态检查/rollout 判定逻辑 MUST 保持不变；就绪判定依据由 k8s 探针机制自然接管（"是否启动"的依据改为探针）。
- **FR-004**: bootstrap（Go）MUST 在所有 Component 启动成功后自动启动 health 服务（提供约定端点）；使用 bootstrap 的既有服务 MUST 无需逐个改造即可获得该能力。
- **FR-005**: bootstrap（Go）停止/退出时（含优雅退出与信号触发）MUST 先停止 health 服务，再停止其他 Component。
- **FR-006**: JS 服务 MUST 遵循与 Go 相同的 health 约定：约定端点、先进先出生命周期顺序、启动失败即退出语义。统一的 JS bootstrap 公共库（提供 health 能力）为后续独立工作，不在本特性范围；本特性中用于验证的 JS 服务（`experimental/`）在自身代码中自行实现 health 端点。
- **FR-007**: health 端点在 health 服务运行期间 MUST 返回成功响应；其语义为"所有组件已启动且进程存活"，不要求各组件的深度健康检查。
- **FR-008**: 端口 38080 与路径 /healthz 作为约定而非强制校验，MUST NOT 增加任何端口相关的校验代码（如端口冲突检查、服务声明检查、编译期校验）。
- **FR-009**: 本特性 MUST NOT 修改 `experimental/` 目录之外的服务代码以适配 health 约定；`experimental/` 目录下的服务 MAY 在本特性内适配，并作为本特性的测试与验证载体。未适配约定的现存服务（如 JS 生产服务 agent/agent_v2）被部署后将因探针失败而无法就绪——这是已知的范围外后果，其适配由后续工作承担。
- **FR-010**: health 服务启动失败（如端口 38080 被占用）MUST 视同组件启动失败：bootstrap 按既有失败语义回滚已启动组件并退出，不得以无 health 端点的状态继续运行。

### Key Entities

- **Health 端点（约定）**：固定端口 38080、固定路径 /healthz；成功响应表示"所有组件已启动、进程存活"。属于约定属性，不是配置项，不做校验。
- **探针（startupProbe / livenessProbe）**：deploy 附加到服务容器的健康检查定义，指向 Health 端点。startupProbe 为慢启动服务提供启动预算，livenessProbe 驱动运行期自愈。
- **Health 服务生命周期顺序**：启动时机 = 所有 Component 启动成功之后；停止时机 = 任何 Component 停止之前。即 health 服务的存活窗口嵌套于所有组件的存活窗口之内（先进先出）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 所有经 deploy 工具部署的服务工作负载（Deployment 与 StatefulSet）均携带指向端口 38080、路径 /healthz 的 startupProbe 与 livenessProbe（对生成清单的检查 100% 命中）。
- **SC-002**: 使用 bootstrap 的服务部署后，仅当 health 端点可正常响应时 rollout 才达到 READY；未实现约定端点的服务不会被判定 READY。
- **SC-003**: 使已部署且已就绪的服务停止响应健康检查后，其容器在分钟级时间内被自动重启，全程无需人工干预。
- **SC-004**: bootstrap 服务退出时，health 服务先于其他任何组件停止（顺序可观测、可通过测试验证）。
- **SC-005**: deploy 工具既有的状态查询与 rollout 判定行为（describe/poll 的语义与时序）保持不变。
- **SC-006**: 仓库内不新增任何 health 端口相关的校验代码。
- **SC-007**: 本特性的正向验收（就绪与自愈路径）使用 `experimental/` 目录下完成适配的服务完成且 rollout 达到 READY，不依赖 `experimental/` 之外的服务；负向验证（未适配服务不 READY）除外，可引用未适配的非 `experimental/` 服务工件。

## Assumptions

- 探针参数（周期、超时、失败阈值、启动预算）采用 k8s 社区常见默认值并给予充足的慢启动预算，具体数值在方案阶段确定；不作为服务可配置项。
- 本特性不引入 readinessProbe（用户明确仅要求 startupProbe 与 livenessProbe）；启动后的持续就绪由 liveness 重启机制间接保障。
- deploy 工具管理的基础设施组件（如 Mongo，已有自身探针）不在本特性范围内。
- deploy 服务自身的部署清单不在本特性范围内，可作为后续跟进项。
- health 端点返回成功状态码即可，响应体无结构化要求（极小的纯文本如 `ok\n`）。
- deploy 无条件附加探针后，`specs/050-vite-react-bazel/contracts/static-server-deploy.md` 记录的"artifact 服务 Deployment 无探针（进程监听即就绪）"状态不再成立（探针将被无条件附加）；该静态文件服务的 server（`experimental/js/vite_react_demo/server`，Go + 共享 bootstrap）经本特性的 bootstrap health 自动获得约定端点，部署后仍可就绪，无需单独适配。
- Go bootstrap 生态服务（`projects/` 与 `experimental/` 下的 Go 服务）经共享 bootstrap 的核心行为自动获得 health 端点，无需逐个改动；JS 侧"bootstrap"为各服务自带的 `bootstrap.ts` 模式而非共享运行时，统一的 JS bootstrap 公共库（含 health 能力）为后续独立工作——本特性中用于验证的 JS 服务（`experimental/`）自行实现 health 端点。
