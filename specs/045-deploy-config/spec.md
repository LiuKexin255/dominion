# Feature Specification: Deploy Config Support

**Feature Branch**: `045-deploy-config`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "为 deploy 工具的配置增加 config 参数支持。config 在 service.yaml 顶层声明（所有 artifact 共享定义池），含若干命名配置块，每个配置块含带类型（json/yaml）的数据条目；deploy.yaml 中 artifact 仅选择配置块名（不覆盖）；提供 Go 与 JS 的配置读取 SDK，支持以带默认值的结构为基准做深度合并反序列化；config 不传递敏感数据，且与环境变量参数互不影响。"

## Clarifications

### Session 2026-08-13

- Q: 配置数据条目的 value 在解析后是否必须为对象/映射，还是可以是任意合法的 JSON/YAML 值（标量/数组）？ → A: value 是字符串，字符串必须是合法的 JSON 或 YAML 格式（由 type 决定）；格式校验检查字符串是否为良构的 JSON/YAML 文本，不额外约束解析后的结构类型。
- Q: 配置条目的 value 可以为空字符串吗？ → A: 不可以。value 须为非空字符串（schema minLength 1）；空字符串对 type=json 而言本就不是合法 JSON，无法通过格式校验（FR-003），对 type=yaml 也按非空约束拒绝，不产生"空文档"语义。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 服务声明配置数据块 (Priority: P1)

服务维护者在 service.yaml 的顶层声明命名配置块，每个配置块包含若干带类型（json/yaml）的数据条目。这让运行时可调参数成为服务定义的一部分，部署者无需修改服务代码即可获得不同的配置内容。

**Why this priority**: 配置声明是整个特性的基础。没有服务侧的配置块定义，部署侧无从选择配置，SDK 也无从读取。

**Independent Test**: 准备一个包含配置块声明的 service.yaml 并校验；校验通过后，部署者可以明确看到该服务定义了哪些配置块与条目，及其数据类型。

**Acceptance Scenarios**:

1. **Given** service.yaml 顶层声明配置块 `service_config`，其数据条目 `key` 的 value 为合法 JSON 文本、type 为 `json`，**When** 部署者校验 service.yaml，**Then** 系统接受该配置并识别出配置块 `service_config` 含有条目 `key`。
2. **Given** service.yaml 中某配置条目的 type 为 `json` 但 value 不是合法 JSON，**When** 部署者校验 service.yaml，**Then** 系统拒绝该配置并指出具体的格式错误。
3. **Given** service.yaml 声明了两个同名的配置块，或同一配置块内存在两个同名的数据条目，**When** 部署者校验 service.yaml，**Then** 系统拒绝该配置并指出重复的名称。

---

### User Story 2 - 部署按环境选择配置块 (Priority: P1)

部署者在 deploy.yaml 的 artifact 条目中选择要为该产物启用的配置块名称。不同环境可以启用不同的配置子集，而无需修改服务代码或配置定义。deploy.yaml 仅做选择，不覆盖或修改配置块中的数据。

**Why this priority**: 配置选择让同一服务在不同环境中拥有不同的运行参数，是配置从定义到运行时生效的关键一环。

**Independent Test**: 准备定义了配置块的 service.yaml，以及一个选择了其中部分配置块的 deploy.yaml；执行部署校验，系统接受配置并展示该产物启用了哪些配置块。

**Acceptance Scenarios**:

1. **Given** service.yaml 定义配置块 `service_config` 与 `feature_flags`，deploy.yaml 中某 artifact 仅选择 `configs: [service_config]`，**When** 部署者执行部署，**Then** 仅有 `service_config` 的数据在运行时提供给该产物。
2. **Given** deploy.yaml 中某 artifact 选择了 service.yaml 未定义的配置块名，**When** 部署者执行部署，**Then** 部署在提交环境变更前终止，并明确提示该配置块未在服务定义中声明。
3. **Given** deploy.yaml 中某 artifact 未选择任何配置块，**When** 部署者执行部署，**Then** 该产物的部署行为与未引入配置特性时完全一致。

---

### User Story 3 - 通过 SDK 读取配置（深度合并默认值）(Priority: P2)

服务开发者使用配置 SDK（Go 与 JavaScript）按（配置块名, 条目名）读取配置数据，并传入一个带默认值的结构作为反序列化基准。SDK 将配置数据深度合并到默认值之上，再把结果反序列化到与默认值同类型的输出对象；开发者无需了解配置文件在容器中的物理位置。

**Why this priority**: SDK 是配置真正被服务消费的入口，使配置以类型安全、增量覆盖的方式生效；但其价值依赖前两个故事（声明与选择）落地。

**Independent Test**: 部署一个选择了配置块的服务后，在运行时通过 SDK 读取配置；SDK 返回深度合并后的结果，结构键与默认值一致，被配置覆盖的字段取配置值。

**Acceptance Scenarios**:

1. **Given** 配置块 `service_config` 的条目 `key` 的内容为 `{"B": 222}`（type: `json`），SDK 调用时默认值为 `{A: "abc", B: 111}`，**When** 服务通过 SDK 读取该条目，**Then** 输出为 `{A: "abc", B: 222}`——配置覆盖默认值，未被配置覆盖的字段保留默认值。
2. **Given** 服务运行时已通过平台机制获得配置目录，**When** 服务通过 SDK 读取任意已选配置块条目，**Then** SDK 经平台注入的目录发现机制定位文件，服务代码中不硬编码任何配置文件路径。
3. **Given** 服务通过 SDK 读取一个未被 deploy.yaml 选中的配置块，**When** SDK 尝试读取该配置，**Then** SDK 返回错误以表明运行环境与代码预期不一致。

---

### Edge Cases

- service.yaml 未声明任何配置块时，校验与部署行为与现有行为完全一致，不产生配置挂载或目录约定。
- 配置条目的 value 为空字符串时，校验阶段拒绝该 service.yaml（value 须为非空原始数据文本；空字符串对 type=json 亦非合法 JSON，无法通过 FR-003 格式校验）。
- 配置块虽在 service.yaml 中定义，但未被任何 deploy.yaml 的 artifact 选择时，该配置块不会被提供给任何产物，不影响部署。
- 配置参数与通过环境变量设置的参数互不影响；两者可同时存在、独立工作，不产生覆盖或冲突。
- 配置不用于传递敏感数据；敏感数据应继续使用已有的 secret 机制（见 `specs/002-deploy-secret-config`）。
- 多个配置块被同一 artifact 选择时，各配置块独立提供，其条目通过各自的（配置块名, 条目名）定位，互不干扰。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 允许在 service.yaml 顶层声明零个或多个命名配置块，每个配置块包含名称与数据条目列表。
- **FR-002**: 每个配置数据条目 MUST 包含名称（name）、原始值（value）与类型（type）；value MUST 为字符串，包含按 type 解释的格式化文本（JSON 或 YAML）；类型 MUST 为 `json` 或 `yaml`。
- **FR-003**: 系统 MUST 对配置数据进行格式校验：type 为 `json` 的条目 value 必须可被 JSON 解析器解析；type 为 `yaml` 的条目 value 必须可被 YAML 解析器解析。校验失败的 service.yaml MUST 被拒绝。
- **FR-004**: 系统 MUST 拒绝包含重复配置块名、或同一配置块内重复条目名的 service.yaml。
- **FR-005**: service.yaml 顶层定义的配置块构成所有产物共享的同一配置定义池。
- **FR-006**: 系统 MUST 允许在 deploy.yaml 的 artifact 条目中选择零个或多个配置块名，以启用对应配置块的数据。
- **FR-007**: 系统 MUST 拒绝 deploy.yaml 中引用了 service.yaml 未定义的配置块名的部署，拒绝 MUST 发生在提交环境变更之前。
- **FR-008**: deploy.yaml 对配置 MUST 仅做名称选择，MUST NOT 覆盖或修改配置块中的任何数据。
- **FR-009**: 系统 MUST 将被选中的配置数据以声明式方式提供给产物运行时，产物侧无须了解配置的物理存储细节。
- **FR-010**: 系统 MUST 通过平台级环境变量提供配置目录发现机制，使运行时 SDK 能定位配置根目录，而无需在服务代码中硬编码路径。
- **FR-011**: 系统 MUST 防止 deploy.yaml 中用户自定义的环境变量覆盖配置目录发现环境变量。
- **FR-012**: 系统 MUST 提供配置读取 SDK，至少覆盖 Go 与 JavaScript 两种语言。
- **FR-013**: SDK MUST 支持按（配置块名, 条目名）读取配置，并接受一个带默认值的结构作为反序列化基准；默认值与输出对象 MUST 为相同类型。
- **FR-014**: SDK MUST 将读取到的配置数据深度合并到默认值之上，并将结果反序列化到输出对象。
- **FR-015**: 深度合并 MUST 对对象/映射类型递归合并：配置中存在的键覆盖默认值对应键，配置中不存在的键保留默认值。
- **FR-016**: 配置机制 MUST 与通过环境变量设置参数互不影响，两者独立工作。
- **FR-017**: 配置 MUST NOT 用于传递敏感数据；敏感数据继续使用已有的 secret 机制。
- **FR-018**: 系统 MUST 将被选中的配置块数据（block/key/type/value）纳入环境期望状态，以便部署控制面在重启或重平衡后能重建相同的配置状态。
- **FR-019**: SDK 的文档/注释 MUST 说明 deploy 配置中 config 的使用方式，包括如何在 service.yaml 声明配置块、如何在 deploy.yaml 选择配置块。
- **FR-020**: 系统 MUST 保持未声明配置块的现有 service.yaml 与 deploy.yaml 的校验与部署行为不变（向后兼容）。

### Key Entities *(include if feature involves data)*

- **配置块 (Config Block)**: service.yaml 顶层声明的命名配置单元，含若干数据条目；是所有产物共享的配置定义池中的一个条目，由 SDK 以其名称寻址。
- **配置数据条目 (Config Data Entry)**: 配置块内的一个键值单元，由名称、原始值（value）与类型（json/yaml）组成；是 SDK 读取与深度合并的最小单位。
- **配置选择 (Config Selection)**: deploy.yaml 中 artifact 对配置块名的引用列表，决定哪些配置块的数据在运行时提供给该产物。
- **配置运行时约定 (Config Runtime Contract)**: 运行时承诺——被选中的配置数据通过平台目录发现机制（环境变量）以文件形式暴露给服务，SDK 据此定位并读取，服务代码不感知物理路径。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% 声明了配置块的 service.yaml，在配置数据格式（json/yaml）非法时于校验阶段被拒绝。
- **SC-002**: 100% 的成功部署中，被 deploy.yaml 选中的配置块的数据条目在运行时可被服务读取。
- **SC-003**: 100% 通过 SDK 读取的配置结果中，未在配置数据中出现的字段保留默认值、出现的字段被配置值覆盖（深度合并生效）。
- **SC-004**: 未声明配置块的现有 service.yaml 与 deploy.yaml 在升级后无需任何修改即可继续校验与部署。
- **SC-005**: 服务开发者可在不硬编码配置文件路径的前提下，仅通过 SDK 与平台注入的目录发现机制读取任意已选配置。
- **SC-006**: 配置参数与环境变量参数在同一部署中可同时存在且互不干扰。

## Assumptions

- 配置数据条目的 `value` 为字符串，包含按 `type` 解释的原始文本（JSON 文本或 YAML 文本）；`type` 决定解析器的选择。这与用户所述"原始数据，不是 base64"一致。
- 配置目录发现机制遵循与 secret 相同的平台注入模式（参考 `specs/002-deploy-secret-config` 的 `DOMINION_SECRET_DIR` 约定）：平台注入一个目录环境变量指向配置根目录，配置文件按（配置块名 / 条目名）组织于其下。具体环境变量名与挂载路径在方案阶段确定。
- 深度合并（FR-015）对映射/对象递归合并；对数组与标量，配置值整体替换默认值（不按索引逐元素合并数组）。
- 服务代码调用 SDK 读取一个未被 deploy.yaml 选中的配置块时，SDK 返回错误（表明运行环境与代码预期不一致），使配置缺失被显式暴露而非静默吞没。
- 配置块名与条目名须为非空、在各自作用域（配置块名在配置块列表内、条目名在所属配置块内）唯一、且路径安全的字符串；具体命名约束在方案阶段对齐 service.yaml 现有命名规范。
- 配置选择校验（deploy 选择的配置块须在 service.yaml 定义池中存在）在部署提交期望状态前由 deploy 工具完成，与现有 secret 绑定校验处于同一阶段（见 `tools/release/deploy/v2/compiler/compiler.go`）。
- 配置为非敏感数据，其基础设施载体对应 Kubernetes 中非敏感的资源类型（ConfigMap 而非 Secret）；具体映射在方案阶段确定。
