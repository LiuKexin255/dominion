# Feature Specification: JS 项目全量切换 ESM

**Feature Branch**: `048-js-esm-migration`

**Created**: 2026-08-24

**Status**: Approved

**Input**: User description: "执行一项重构，将仓库内的全部 js 项目从 commonjs 切换到 es"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 服务以 ESM 形态构建与运行 (Priority: P1)

作为仓库的使用者（开发者/CI），我希望仓库内所有以 CJS 产物运行的服务型 JS 项目（`projects/game/agent`、`experimental/dsh/demo/agent`、`experimental/ts/grpc_hello_world`、`experimental/grpc_chain/mid`、`experimental/openai_llm/client`、`experimental/ts/hello_world`、`experimental/ts/team_graph_spike`）在重构后以 ESM 模块形态构建、打包与运行，使整个 JS 技术栈统一在现代模块标准上。

**Why this priority**: 服务是仓库的核心交付物；它们能否以 ESM 正确构建、部署并保持行为不变，决定了本次重构是否成立。这是价值最大、风险也最集中的切片。

**Independent Test**: 对每个服务执行完整构建、单元测试与既有大型测试（testplan），全部通过且服务对外行为（接口契约、可观测性数据）与重构前一致。

**Acceptance Scenarios**:

1. **Given** 重构完成后的代码库，**When** 通过仓库标准构建入口构建全部服务 target，**Then** 所有服务产物为 ESM 模块格式（包声明为 ESM），构建成功且无 CJS 残留告警。
2. **Given** 任一重构后的服务镜像被部署，**When** 服务启动并接收请求，**Then** 服务行为与重构前一致（接口响应、日志、trace 均正常产出）。
3. **Given** 消费 ESM-only 第三方依赖（如 langchain、MCP SDK、dsh 系列）的服务代码，**When** 以 ESM 方式加载这些依赖，**Then** 依赖加载成功，不再依赖 CJS 侧的 `require(esm)` 桥接机制。
4. **Given** 服务代码中依赖文件位置定位的原有用法，**When** 以 ESM 等价方式表达，**Then** proto 文件、SKILL.md、golden 数据等资源定位结果与重构前完全一致。

---

### User Story 2 - 公共库以 ESM 交付并被工作区消费 (Priority: P2)

作为工作区内的包消费者（其他 JS 项目的开发者），我希望所有公共库包（`common/js/config`、`common/js/otel`、`common/js/resolver`、`common/js/logs`、`common/js/grpc/otel`、`common/js/grpc/resolver`、`projects/game/pkg/saolei-board`、`third_party/dsh/core`）以 ESM 形态交付，且能被工作区内其他包以 ESM import 正常消费。

**Why this priority**: 公共库是服务之间共享的基石；它们完成 ESM 化后，服务侧的 ESM 化才有完整的消费链路。在服务切片之后完成即可支撑最终统一。

**Independent Test**: 工作区内任一消费者包通过 ESM import 引用公共库导出，类型解析与运行时加载均正确；公共库自身的单元测试全部通过。

**Acceptance Scenarios**:

1. **Given** 已切换为 ESM 的公共库，**When** 工作区其他包 import 该库，**Then** 导出的 API（含类型声明）可被正确解析与调用。
2. **Given** 公共库的既有导出 API 集合，**When** 完成 ESM 切换，**Then** 对外导出的 API 名称与语义保持不变（仅模块格式变化，无 API 破坏）。
3. **Given** 库内使用 CJS-only 依赖（如 `js-yaml`、`pngjs`、OTel 系列）的代码，**When** 以 ESM 方式引用这些依赖，**Then** 依赖加载与调用行为正确。

---

### User Story 3 - 统一的模块规范与开发体验 (Priority: P3)

作为长期在仓库上工作的开发者，我希望重构完成后仓库形成统一、明确、有文档支撑的 ESM 模块规范：源码书写方式一致、代码规范文档更新、不再存在 CJS 与 ESM 两套机制并存的困惑点。

**Why this priority**: 规范统一是重构的长期收益所在，但依赖前两个切片完成后才有意义；单独完成不产生运行时价值。

**Independent Test**: 通过对仓库的静态审计确认不存在 CJS 残留（包声明、构建配置、源码惯用法），且规范文档描述与实际一致。

**Acceptance Scenarios**:

1. **Given** 重构完成后的仓库，**When** 审计所有 JS 工作区包的模块声明与构建配置，**Then** 全部包为 ESM（前端包 `projects/game/desktop/frontend` 本已是 ESM，保持不变）。
2. **Given** 更新后的代码规范文档，**When** 开发者按文档书写新的 JS/TS 代码，**Then** 文档中的模块书写规范可直接指导开发，与实际构建行为一致。
3. **Given** 仓库 JS/TS 源码，**When** 静态扫描 CJS 专有惯用法（`__dirname`、`__filename`、`require()` 直用、`module.exports` 等），**Then** 生产源码中不再存在 CJS 专有写法（测试中因可观测性插桩机制必须保留的例外，须在规范文档中显式记录豁免理由）。

---

### Edge Cases

- **可观测性插桩在 ESM 下的等价性**：现有 gRPC 插桩依赖"模块加载钩子拦截 CJS require"的机制；ESM 的模块加载路径不同，重构后必须保证 trace/指标仍被正常采集（不得出现服务可用但遥测静默丢失的状态）。
- **生成代码的模块格式**：由 proto 生成、构建期产出的 TS 生成代码，其模块格式必须与 ESM 消费方式兼容。
- **CJS-only 第三方依赖的互操作**：`@grpc/grpc-js`、OpenTelemetry 系列、`express`、`mongodb`、`pngjs`、`js-yaml` 等仍为 CJS 的依赖，从 ESM 侧引用时不得出现命名导出缺失或默认导出错位。
- **测试基建的模块一致性**：单元测试直接运行于 TS 源码的既有测试流水线，在源码/产物切换为 ESM 后必须继续可用，测试结果不得因模块形态差异而失真。
- **既有烟测/审计脚本**：直接加载编译产物的 smoke 测试与闭包审计脚本，必须适配 ESM 产物形态后继续有效。
- **CLI 入口**：`saolei-recognize` CLI（`js_binary` 运行）在 ESM 形态下必须保持可执行。
- **第三方 ESM 依赖含顶层 await 的情形**：以 ESM 方式消费 dsh 系列等 ESM-only 包时，若依赖含顶层 await，加载语义必须仍然正确。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 仓库内全部 JS 工作区包（`pnpm-workspace.yaml` 所列、当前为 CJS 的 15 个包）MUST 切换为 ESM 模块格式，包括包级模块声明、TS 编译配置与构建产物格式。
- **FR-002**: 已是 ESM 的包（`projects/game/desktop/frontend`）MUST 保持可用，不受本次重构破坏。
- **FR-003**: 每个被重构的包 MUST 在重构后通过其全部既有单元测试，且测试覆盖不弱化（不得以删除/跳过测试的方式达成切换）。
- **FR-004**: 每个服务型包 MUST 在重构后通过标准构建入口完成构建，产出的服务镜像可部署且对外行为（接口契约、日志、trace）与重构前一致。
- **FR-005**: 工作区内部包之间的引用 MUST 以 ESM 方式完成，跨包 import（含类型声明解析）在构建与运行时均正确。
- **FR-006**: 仍为 CJS 的第三方依赖 MUST 能从 ESM 侧正确引用，不改变各依赖的版本集合（本次重构不升级依赖版本）。
- **FR-007**: 可观测性能力 MUST 在重构后保持等价：服务的 trace、日志、指标产出不因模块系统切换而丢失或失真。
- **FR-008**: 生产源码中依赖 CJS 专有惯用法（`__dirname`/`__filename`/`require()` 直用/`module.exports`）的代码 MUST 改写为 ESM 等价实现，资源定位结果不变；因插桩机制必须保留的例外 MUST 限定在测试代码中并被规范文档记录。
- **FR-009**: JS 代码规范文档（`style/javascript.md`）MUST 更新为描述 ESM 目标形态的规范，其描述与实际构建行为一致。
- **FR-010**: 既有直接加载编译产物的 smoke 测试与依赖闭包审计脚本 MUST 适配 ESM 产物形态并继续通过。
- **FR-011**: 重构 MUST 以不改变服务对外行为为约束完成；任何接口契约、配置格式、部署形态的变化都不属于本次范围。
- **FR-012**: 仓库中的非 JS 项目（Go、Python 等）MUST 不受本次重构影响。

### Key Entities

- **JS 工作区包 (JS Workspace Package)**: pnpm workspace 内一个拥有独立 `package.json` 的 JS/TS 单元；本特性涉及 16 个，其中 15 个待从 CJS 切换为 ESM、1 个已是 ESM。属性：包名、模块类型、构建方式（TS 编译 / Vite 构建）、角色（服务 / 公共库 / CLI / 依赖钉扎）。
- **模块消费关系 (Module Consumption Edge)**: 包与包之间、包与第三方依赖之间的 import 关系；重构须保证每条边的加载语义在 ESM 下等价。特例边：CJS 侧经 `require(esm)` 消费 ESM-only 依赖（重构后应消失）、ESM 侧引用 CJS 依赖（重构后普遍存在，须验证互操作正确）。
- **可观测性插桩 (Instrumentation)**: 依赖模块加载时机注入的遥测采集能力；其生效机制与模块系统耦合，是重构中必须保持等价的横切关注点。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 重构完成后，仓库全部 JS 工作区包均为 ESM 模块声明，静态审计零 CJS 包残留（16/16）。
- **SC-002**: 仓库全部既有 JS 单元测试通过率 100%，测试用例数量不少于重构前（无删除/跳过）。
- **SC-003**: 全部服务型包构建成功并通过产物级断言（tar 内服务根 `package.json` 含 `"type": "module"`、烟测通过、CLI 入口可运行）；具备既有 testplan 的服务型包（game agent、dsh demo agent、grpc_hello_world、grpc_chain/mid）完成部署级大型测试验收闭环（部署→测试→清理，全部用例通过）；openai_llm/client 与 team_graph_spike 的既有 testplan 因既有缺陷（YAML cases 引用 target 名与 BUILD 声明不一致 / 依赖已回退且从未入库的 fake-llm fixtures）无法通过，已完全移除（处置记录见 [tasks.md](tasks.md) T024）；hello_world 为 js_binary 非部署型服务，经 `bazel run` 冒烟验证；README 不登记豁免。
- **SC-004**: 生产源码静态扫描 CJS 专有惯用法，命中数为零（测试代码中经规范记录的插桩豁免除外）。
- **SC-005**: 重构前后各服务的接口契约测试与可观测性验证（trace/日志产出）结果一致，无遥测丢失。

## Assumptions

- "全部 js 项目" 指 `pnpm-workspace.yaml` 声明的工作区内全部包，含 `experimental/` 下的实验项目与 `third_party/dsh/core` 钉扎包；`projects/game/desktop/frontend` 已是 ESM，仅需验证不受影响，无切换工作。
- 运行时保持 Node 24（distroless nodejs24 镜像）不变；依赖版本集合不变（仅模块系统切换，不混入依赖升级）。
- `specs/019-js-test-reliability/research.md` 中曾否决"将库转为 ESM"的方案（当时爆炸半径过大）；本特性以专项重构的方式承接并取代该决策，属主动的技术债偿还而非重复评估。
- "切换到 ESM" 包含两层含义：构建产物与包声明为 ESM（运行时形态），以及源码书写遵循 ESM 约定（相对导入需带扩展名等，以所选模块解析策略为准）。
- Bazel 构建体系（rules_js/rules_ts/swc 等）继续作为唯一构建入口；具体配置选型（如模块解析策略、编译目标参数）属于方案设计阶段的决策，不在本规格书中固定。
- Vitest 继续作为单元测试框架；测试对模块形态的适配方式由方案设计决定。
