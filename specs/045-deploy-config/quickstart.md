# Quickstart: Deploy Config Validation

**Feature**: 045-deploy-config | **Spec**: [spec.md](spec.md)

本指南定义可运行的端到端验证场景，证明 config 特性全链路工作：声明（service.yaml）→ 选择（deploy.yaml）→ 校验（compiler）→ 期望状态（proto）→ reconcile（控制面创建 ConfigMap）→ 挂载（builder）→ SDK 读取 + 深度合并。

deploy 控制面自身无法自举大型测试（见 `projects/infra/deploy` README），故以 `experimental/` 服务为被测对象，经真实控制面部署验证。

---

## 前置条件

- 可用的 deploy 控制面（测试环境）与集群。
- `bazel` 构建工具。
- testplan skill（`tools/test/guitar`）用于大型测试执行。
- 阅读规范：[contracts/yaml-schema.md](contracts/yaml-schema.md)、[contracts/runtime-contract.md](contracts/runtime-contract.md)、[contracts/sdk-go.md](contracts/sdk-go.md)、[contracts/sdk-js.md](contracts/sdk-js.md)。

---

## 场景 1：Go 服务读取 config（深度合并验证）

**被测对象**：`experimental/golang/grpc_hello_world/service`

### 验证目标

服务通过 Go SDK `config.Read` 读取配置块，深度合并默认值；部署后 `SayHello` 响应反映 config 覆盖的问候语，证明挂载与读取均生效。

### 关键步骤（实现细节见 tasks.md）

1. **service.yaml**（`experimental/golang/grpc_hello_world/service/service.yaml`）顶层新增配置块声明（schema 见 [yaml-schema.md](contracts/yaml-schema.md) §1）：
   - 配置块 `service_config`，条目 `greeting`（type: `yaml`），value 含 `message` 与 `times` 字段。
2. **deploy.yaml**（`experimental/golang/grpc_hello_world/testplan/deploy.yaml`）artifact 选择 `configs: [service_config]`（schema 见 [yaml-schema.md](contracts/yaml-schema.md) §2）。
3. **服务代码**（`service/main.go`）：
   - 定义 `Greeting` 结构体与默认值（如 `{Message: "hello", Times: 1}`）。
   - 调用 `config.Read("service_config", "greeting", defaultGreeting)`（API 见 [sdk-go.md](contracts/sdk-go.md)）。
   - `SayHello` 使用合并后的 `Message` 与 `Times` 构造响应。
4. **BUILD.bazel**：`runtime_deps` 加入 `//common/gopkg/config:runtime_pkg`（或等价 target）。

### 验证（大型测试）

通过 testplan skill 执行 `experimental/golang/grpc_hello_world/testplan/interface_test.yaml`：

```
guitar run experimental/golang/grpc_hello_world/testplan/interface_test.yaml
```

**期望结果**：
- 部署成功（config 块被校验通过、ConfigMap 被创建、卷被挂载）。
- `SayHello` 响应的问候语 = service.yaml `greeting` 条目中声明的 `message`（**非**默认值），证明 config 覆盖生效。
- `SayHello` 响应的重复次数 = config 中声明的 `times`（覆盖默认），且若 config 省略某字段则保留默认值（深度合并）。

---

## 场景 2：TS 服务读取 config（深度合并验证）

**被测对象**：`experimental/ts/grpc_hello_world`

### 验证目标

服务通过 JS SDK `readConfig` 读取配置块，深度合并默认值；部署后响应反映 config 覆盖。

### 关键步骤（实现细节见 tasks.md）

1. **service.yaml**（`experimental/ts/grpc_hello_world/service.yaml`）新增配置块（同场景 1 结构）。
2. **deploy.yaml**（`experimental/ts/grpc_hello_world/testplan/deploy.yaml`）选择配置块。
3. **服务代码**（`src/server.ts`）：
   - 定义 `Greeting` 接口与默认值。
   - 调用 `readConfig<Greeting>("service_config", "greeting", defaultGreeting)`（API 见 [sdk-js.md](contracts/sdk-js.md)）。
   - `SayHello` 使用合并结果构造响应。
4. **BUILD.bazel**：`runtime_deps` 加入 `//common/js/config:runtime_pkg`；`package.json` 加入 `@dominion/common-js-config` 依赖。

### 验证（大型测试）

```
guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml
```

**期望结果**：与场景 1 对称——响应问候语反映 config 覆盖，深度合并生效。

---

## 场景 3：校验门禁（CLI 侧）

证明 config 校验在部署提交期望状态前 fail-fast。

### 验证步骤

1. **格式校验（FR-003）**：构造 service.yaml 配置条目 `type: json` 但 value 非合法 JSON → 执行 `deploy apply` → 期望被拒绝并提示格式错误。
2. **未定义选择（FR-007）**：构造 deploy.yaml 选择 service.yaml 未定义的配置块名 → 执行 `deploy apply` → 期望被拒绝并提示配置块未定义。
3. **重复名（FR-004）**：service.yaml 含两个同名配置块 → 期望被拒绝。

> 这些可通过 deploy CLI 的单元测试覆盖（`tools/release/deploy/pkg/config` 与 `v2/compiler` 的测试），无需完整部署。

---

## 场景 4：向后兼容（FR-020）

证明未声明 config 的现有服务不受影响。

### 验证步骤

- 现有 `experimental/golang/grpc_hello_world/gateway/service.yaml`（不含 `configs`）与对应 deploy.yaml（artifact 不含 `configs`）应继续校验通过、部署成功、行为不变。
- 该场景已由场景 1 的 testplan 隐式覆盖（gateway 作为 HTTP 后端同部署）。

---

## 验收标准映射

| Spec 条目 | 验证场景 |
|-----------|----------|
| FR-003（格式校验） | 场景 3.1 |
| FR-004（重复名拒绝） | 场景 3.3 |
| FR-007（未定义选择拒绝） | 场景 3.2 |
| FR-009/FR-010（声明式提供 + 目录发现） | 场景 1/2（SDK 经 `DOMINION_CONFIG_DIR` 定位文件） |
| FR-013/FR-014/FR-015（SDK 读取 + 深度合并） | 场景 1/2（响应反映覆盖 + 默认值保留） |
| FR-020（向后兼容） | 场景 4 |

> 大型测试执行规范见 `style/large_test.md`；执行前必读。验收须通过 testplan skill 完成完整 部署→测试→清理 闭环，所有用例通过（宪章原则 VI）。
