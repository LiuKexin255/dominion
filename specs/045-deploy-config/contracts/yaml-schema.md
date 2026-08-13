# Contract: service.yaml & deploy.yaml Config Schema

**Feature**: 045-deploy-config | **Spec**: [spec.md](../spec.md)

本契约定义 config 特性对 `service.yaml` 与 `deploy.yaml` 的 schema 扩展。权威校验由 JSON Schema（`tools/release/deploy/pkg/schema/service.schema.json`、`deploy.schema.json`）与 Go 解析层（`tools/release/deploy/pkg/config/config.go`）共同实施。

---

## 1. service.yaml — 顶层配置块声明

在 service.yaml **顶层**新增 `configs` 字段（所有 artifact 共享的配置块定义池）。

### 完整示例

```yaml
version: "3.0"
name: service
app: grpc-hello-world
kind: stateless
desc: grpc hello world service
ports:
  - name: grpc
    port: 50051
configs:                                    # NEW: 顶层配置块池
  - name: service_config                    #   配置块名（SDK 第一寻址参数）
    data:
      - name: greeting                      #   条目名（SDK 第二寻址参数）
        value: |                            #   原始数据文本（字符串，非 base64）
          message: "hello from config"
          times: 3
        type: yaml                          #   格式类型 json|yaml（部署期校验用）
      - name: limits
        value: '{"maxConn": 100}'
        type: json
artifacts:
  - name: service
    target: :cmd_image
    tls: true
```

### 字段约束

| 路径 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `configs` | `array` | 可选；顶层 | 配置块列表 |
| `configs[].name` | `string` | 非空，`^[a-z][a-z0-9_-]{0,63}$`，列表内唯一 | 配置块名 |
| `configs[].data` | `array` | minItems 1 | 数据条目列表 |
| `configs[].data[].name` | `string` | 非空，`^[a-z][a-z0-9_-]{0,63}$`，所属块内唯一 | 条目名 |
| `configs[].data[].value` | `string` | 非空（minLength 1） | 原始数据文本（JSON 或 YAML）；空字符串被拒绝（见下方注） |
| `configs[].data[].type` | `enum` | `json` \| `yaml` | 格式类型 |

> **空字符串 value**：`value` 为空字符串时由 schema `minLength 1` 直接拒绝（spec Edge Case 已对齐：不产生"空文档"语义；空串对 `type: json` 亦非合法 JSON，无法通过 FR-003 校验）。

### JSON Schema 片段（追加到 `service.schema.json` 顶层 `properties`）

```json
"configs": {
  "type": "array",
  "description": "所有 artifact 共享的配置块定义池（非敏感数据）",
  "items": {
    "type": "object",
    "additionalProperties": false,
    "required": ["name", "data"],
    "properties": {
      "name": {
        "type": "string",
        "minLength": 1,
        "pattern": "^[a-z][a-z0-9_-]{0,63}$",
        "description": "配置块名"
      },
      "data": {
        "type": "array",
        "minItems": 1,
        "items": {
          "type": "object",
          "additionalProperties": false,
          "required": ["name", "value", "type"],
          "properties": {
            "name": {
              "type": "string",
              "minLength": 1,
              "pattern": "^[a-z][a-z0-9_-]{0,63}$"
            },
            "value": {
              "type": "string",
              "minLength": 1,
              "description": "原始数据文本（JSON 或 YAML），非 base64"
            },
            "type": {
              "type": "string",
              "enum": ["json", "yaml"]
            }
          }
        }
      }
    }
  }
}
```

> **命名规则一致**：`name` 的 pattern 与 secret 逻辑名（`specs/002-deploy-secret-config`）一致 `^[a-z][a-z0-9_-]{0,63}$`，因为 name 同时作为运行时文件名，须路径安全。

---

## 2. deploy.yaml — artifact 配置块选择

在 deploy.yaml 的 `services[].artifact` 新增 `configs` 字段（配置块名列表，仅选择不覆盖）。

### 完整示例

```yaml
version: "3.0"
name: liukexin.demo
type: test
desc: "开发环境"
services:
  - artifact:
      path: //experimental/golang/grpc_hello_world/service/service.yaml
      name: service
      configs:                               # NEW: 选择的配置块名列表
        - service_config
  - artifact:
      path: //experimental/golang/grpc_hello_world/gateway/service.yaml
      name: gateway
    http:
      hostnames:
        - hello.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /v1
```

### 字段约束

| 路径 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `services[].artifact.configs` | `array<string>` | 可选；每项非空，`^[a-z][a-z0-9_-]{0,63}$`，列表内唯一（`uniqueItems`） | 选中的配置块名（须存在于 service.yaml 配置块池） |

> **单向选择**：与 secret 的双向校验（声明须全部绑定）不同，config 是从池中选择子集——deploy 选中的名必须存在于 service 配置块池（FR-007），但池中未被选中的块不影响部署。deploy.yaml 中 `configs` 仅有名字段，**无 value/type**（FR-008：不覆盖）。
>
> **列表内唯一**：`configs` 列表使用 `uniqueItems: true` 拒绝重复选择（与 service.yaml 配置块名唯一性 FR-004 对称）；重复选择会导致编译后的 `ConfigBlock` 出现重复块名（同名块映射同一 ConfigMap `{workload}-config-{block}` 冲突），与控制面 domain 校验（VR-CB-6，见 [data-model.md §4](../data-model.md)）冲突，故在 schema 层 fail-fast。

### JSON Schema 片段（追加到 `deploy.schema.json` 的 `services[].items.artifact.properties`）

```json
"configs": {
  "type": "array",
  "uniqueItems": true,
  "description": "选择的配置块名列表（列表内唯一；须存在于 service.yaml 配置块池）",
  "items": {
    "type": "string",
    "minLength": 1,
    "pattern": "^[a-z][a-z0-9_-]{0,63}$"
  }
}
```

---

## 3. 校验层职责划分

| 校验项 | 实施位置 |
|--------|----------|
| 字段存在性、类型、pattern、enum、minLength | JSON Schema（`schema.ValidateServiceYAML` / `ValidateDeployYAML`） |
| 重复配置块名 / 同块内重复条目名 | Go `ParseServiceConfig`（schema 的 `uniqueItems` 仅适用于标量数组，对象数组需 Go 校验） |
| value 格式与 type 一致（json 可解析 / yaml 可解析） | Go `ParseServiceConfig`（FR-003） |
| deploy 选择的配置块名存在于 service 配置块池 | Go `compiler.Compile`（FR-007，需 serviceConfig 信息） |
