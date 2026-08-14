# Contract: Go Config SDK (`common/gopkg/config`)

**Feature**: 045-deploy-config | **Spec**: [spec.md](../spec.md) | **Runtime**: [runtime-contract.md](runtime-contract.md)

Go 配置读取 SDK，遵循 Go 社区 "unmarshal over defaults" 惯用法。包路径 `common/gopkg/config`，package `config`。

---

## 1. 公共 API

```go
package config // import "dominion/common/gopkg/config"

// Read 读取配置条目 (block, key)，将其内容深度合并到 defaults 之上，
// 返回合并后的新值。defaults 不被修改。
//
// 配置文件通过平台注入的 DOMINION_CONFIG_DIR 环境变量发现，
// 路径为 {DOMINION_CONFIG_DIR}/{block}/{key}。
// 文件一律以 YAML 解析（兼容 json 与 yaml 内容）。
//
// 深度合并语义：对象/映射递归合并；数组和标量由配置值整体替换。
// 详见包文档与 specs/045-deploy-config/contracts/runtime-contract.md。
//
// 错误情况：
//   - DOMINION_CONFIG_DIR 未设置
//   - 配置文件不存在（配置块未被 deploy.yaml 选择）
//   - 文件内容不可解析
func Read[T any](block, key string, defaults T) (T, error)
```

### 用法示例

```go
type Greeting struct {
    Message string `yaml:"message"`
    Times   int    `yaml:"times"`
}

defaultGreeting := Greeting{Message: "hello", Times: 1}

cfg, err := config.Read("service_config", "greeting", defaultGreeting)
if err != nil {
    log.Fatal(err)
}
// 若配置文件内容为 "message: hi\ntimes: 5"（yaml），则 cfg = {Message: "hi", Times: 5}
```

---

## 2. 实现规范

### 深度合并路径（research.md R3）

为保证 FR-015（对象/map 递归合并）严格成立，采用 map 层递归合并：

1. 读取文件 `{DOMINION_CONFIG_DIR}/{block}/{key}`。
2. `yaml.Unmarshal` 文件内容 → `map[string]any`（cfgMap）。
3. `yaml.Marshal(defaults)` → `yaml.Unmarshal` → `map[string]any`（defMap，深拷贝 defaults）。
4. 递归深合并 cfgMap over defMap：两者均为 map 则递归；否则 cfgMap 值替换。
5. `yaml.Marshal(merged)` → `yaml.Unmarshal` → 新的 `T`（out）。
6. 返回 out。

> 选用 `gopkg.in/yaml.v3`（go.mod 已有）。YAML 解析器兼容 JSON 内容（research.md R4）。

### 不修改 defaults

步骤 3 经 marshal 往返生成全新 defMap，原始 `defaults` 不被触碰。

### 错误包装

所有错误用 `fmt.Errorf` 包裹上下文（含 block、key、路径），不泄漏文件内容。

---

## 3. 包文档职责（FR-019）

包级 doc comment **必须**说明 deploy 配置中 config 的完整使用方式：

1. **service.yaml 声明**：顶层 `configs` 定义配置块池（含示例）。
2. **deploy.yaml 选择**：artifact `configs` 选择配置块名（仅选择不覆盖）。
3. **运行时读取**：`DOMINION_CONFIG_DIR` 发现 + `Read(block, key, defaults)` 读取。
4. **深度合并**：增量覆盖语义（对象合并、数组/标量替换）。
5. **与非敏感约束**：config 不传递敏感数据（用 secret 机制）。

文档引用：`specs/045-deploy-config/contracts/yaml-schema.md`、`runtime-contract.md`。

---

## 4. 测试要求

- 单测覆盖深度合并矩阵（[data-model.md](../data-model.md) "Deep Merge Semantics"）。
- 单测覆盖错误情况（env 未设置、文件缺失、解析失败）。
- 单测验证 `defaults` 不被修改（合并前后 defaults 相等）。
- json-type 与 yaml-type 文件均正确解析（R4 验证）。
