# Feature Specification: Guitar CLI Enhancements

**Feature Branch**: `008-guitar-cli-enhancements`

**Created**: 2026-06-11

**Status**: Draft

**Input**: User description: "为 guitar 测试编排工具做优化：1. 优化控制台输出，多 suite 时通过颜色和缩进帮助展示每个 suite 执行步骤以及结果；2. 增加 --suite 参数运行指定 suite；3. 为 suite 增加 timeout 配置，执行测试时设置超时时间"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Console Output with Suite-Level Visual Differentiation (Priority: P1)

作为测试工程师，当运行包含多个 suite 的大型测试计划时，我需要在控制台输出中清晰地看到每个 suite 的边界、执行步骤和结果，以便快速定位是哪个 suite 的哪一步出了问题。

**Why this priority**: 这是用户反馈的直接痛点——多 suite 执行时控制台输出混在一起，无法区分不同 suite 的日志和结果。直接影响调试效率。

**Independent Test**: 准备一个包含 2-3 个 suite 的 YAML 测试计划，运行 `guitar run`，观察控制台输出是否有清晰的 suite 分隔、缩进和颜色区分。

**Acceptance Scenarios**:

1. **Given** 一个包含 3 个 suite 的测试计划, **When** 执行 `guitar run <plan.yaml>`, **Then** 每个 suite 的输出段落有明确的标题分隔线，且标题包含 suite 名称，suite 内的部署、测试、清理步骤使用缩进展示，不同 suite 使用不同颜色标记
2. **Given** 一个包含 2 个 suite 的测试计划且第二个 suite 失败, **When** 执行 `guitar run <plan.yaml>`, **Then** 失败 suite 的标题和错误信息用醒目的颜色（如红色）标识，成功 suite 用绿色标识，用户可快速定位失败的 suite
3. **Given** 一个只包含 1 个 suite 的测试计划, **When** 执行 `guitar run <plan.yaml>`, **Then** 输出格式保持一致，suite 标题和步骤缩进正常展示
4. **Given** 测试计划的输出被重定向到非终端（如管道或文件）, **When** 执行 `guitar run <plan.yaml> | tee output.log`, **Then** 输出自动禁用颜色代码，保证日志文件内容干净可读

---

### User Story 2 - Run Specific Suite by Name (Priority: P2)

作为测试工程师，我需要通过 `--suite` 参数只运行测试计划中的某一个指定 suite，以便在开发调试阶段快速迭代，而不必等待整个测试计划的所有 suite 都跑完。

**Why this priority**: 显著提升调试效率——当只关注某个 suite 时，无需执行其他 suite 的部署、测试和清理，节省大量时间。

**Independent Test**: 准备一个包含 3 个 suite 的 YAML 测试计划，运行 `guitar run --suite <suite_name> <plan.yaml>`，验证只执行了指定的 suite。

**Acceptance Scenarios**:

1. **Given** 一个包含 suite-a、suite-b、suite-c 的测试计划, **When** 执行 `guitar run --suite suite-b <plan.yaml>`, **Then** 只执行 suite-b 的部署、测试和清理，跳过 suite-a 和 suite-c
2. **Given** 一个测试计划, **When** 执行 `guitar run --suite nonexistent <plan.yaml>`, **Then** 报错提示指定的 suite 名称不存在，并列出可用的 suite 名称
3. **Given** 一个测试计划, **When** 不使用 `--suite` 参数执行 `guitar run <plan.yaml>`, **Then** 所有 suite 按原有顺序执行，行为与当前版本完全一致
4. **Given** 一个测试计划且指定 suite 名称匹配多个 suite, **When** 执行 `guitar run --suite <name> <plan.yaml>`, **Then** 仅执行第一个匹配的 suite

---

### User Story 3 - Per-Suite Timeout Configuration (Priority: P3)

作为测试工程师，我需要在 YAML 测试计划的 suite 级别配置超时时间（单位：秒），以便对不同复杂度的 suite 设置不同的测试超时，避免简单 suite 等待过久或复杂 suite 被全局超时截断。

**Why this priority**: 增强灵活性——当前只有全局超时，无法针对不同 suite 粒度控制。优先级低于视觉优化和 suite 过滤，但对实际使用体验有显著提升。

**Independent Test**: 准备一个 suite 配置了 5 秒超时的测试计划，运行后验证超时生效。

**Acceptance Scenarios**:

1. **Given** 一个 suite 配置了 `timeout: 60`（60 秒）, **When** 执行该 suite 的测试, **Then** 测试命令使用 60 秒超时，超时后终止并报错
2. **Given** 一个 suite 未配置 timeout 字段, **When** 执行该 suite 的测试, **Then** 使用命令行全局 `--timeout` 参数的值作为回退超时
3. **Given** 一个包含两个 suite 的测试计划，suite-a 配置 timeout: 30，suite-b 配置 timeout: 120, **When** 执行 `guitar run <plan.yaml>`, **Then** suite-a 使用 30 秒超时执行，suite-b 使用 120 秒超时执行
4. **Given** 一个 suite 配置了 `timeout: 0`（无超时）, **When** 执行该 suite 的测试, **Then** 不设置测试级别的超时限制，仅受全局超时约束

---

### Edge Cases

- 当测试计划 YAML 中的 suites 列表为空时，`guitar run` 应如何表现？
- `--suite` 参数值为空字符串时，应报错提示。
- suite 的 timeout 配置为负数时，应在校验阶段报错。
- 终端不支持颜色时（通过环境变量 TERM 判断），输出应自动降级为无颜色模式。
- 当使用 `--suite` 过滤后只执行单个 suite 时，全局超时应只覆盖该 suite 的执行周期。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统必须在控制台输出中为每个 suite 生成带有 suite 名称的标题行，并使用分隔线与上下文区分
- **FR-002**: 系统必须使用颜色标识 suite 的执行状态：成功为绿色，失败为红色，进行中为默认色或黄色
- **FR-003**: 系统必须在 suite 内的部署、测试、清理步骤使用缩进层级展示，使 suite 边界清晰可辨
- **FR-004**: 系统必须在输出被重定向到非终端设备时自动禁用颜色代码
- **FR-005**: 系统（`guitar run`）必须支持 `--suite` 命令行参数，参数值为 suite 名称，仅执行匹配名称的 suite
- **FR-006**: 当 `--suite` 指定的名称在测试计划中不存在时，系统必须报错并显示所有可用的 suite 名称
- **FR-007**: 当未指定 `--suite` 参数时，系统必须按原有行为执行所有 suite
- **FR-008**: 系统必须支持在 YAML 配置的 suite 级别新增 `timeout` 字段，值为正整数（单位：秒）
- **FR-009**: 当 suite 配置了 `timeout` 时，系统必须在执行该 suite 的测试命令时使用该超时值
- **FR-010**: 当 suite 未配置 `timeout` 时，系统必须使用命令行全局 `--timeout` 参数的值
- **FR-011**: `guitar validate` 必须校验 `timeout` 字段为非负整数，负数或非数值应在校验阶段报错

### Key Entities

- **Suite 超时配置**: 每个 suite 可独立配置的执行超时（秒），作为 suite 实体的一个可选属性
- **Suite 过滤器**: `--suite` 命令行参数建立的运行时过滤器，控制哪些 suite 参与执行

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 用户在运行包含 3 个及以上 suite 的测试计划时，能在一秒内从控制台输出中定位任意一个 suite 的开始、结束和执行结果
- **SC-002**: 使用 `--suite` 参数运行指定 suite 时，只执行该 suite 的部署、测试和清理流程，不执行其他任何 suite 的操作
- **SC-003**: 每个 suite 独立配置的超时时间生效，超时后测试进程被终止并产生明确的超时错误信息
- **SC-004**: 输出重定向到文件时不包含颜色转义码，保证日志文件干净可读
- **SC-005**: 现有不使用新参数和配置的测试计划能保持完全兼容，无需任何修改即可正常运行

## Assumptions

- 颜色输出使用 ANSI 转义码，且主要目标终端为现代 Linux/macOS 终端，兼容性由 `isatty` 检测保证
- `--suite` 参数仅支持精确匹配 suite 名称，不支持通配符或正则表达式
- suite 级别的 `timeout` 仅作用于测试执行阶段（`bazel test`），不覆盖部署和清理阶段
- suite 的 `timeout` 字段是可选的，省略时回退到全局超时
- 当全局超时和 suite 超时同时存在时，取两者中较短的作为实际超时上限
- 示例 YAML 配置文件（`example.guitar.yaml`）需要更新以展示新增的 `timeout` 字段
