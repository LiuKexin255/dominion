# @dominion/common-js-config

Deploy config 读取 SDK（TypeScript）。服务按 `(配置块名, 条目名)` 读取部署平台提供的配置文件，
并将其内容深度合并到调用方默认值之上。Go 侧对应 SDK 见 `common/gopkg/config`。

## 配置全链路用法（FR-019）

### 1. service.yaml 声明配置块

服务在 `service.yaml` 顶层声明命名配置块池（所有 artifact 共享）。每个配置块含若干数据条目，
条目由名称、原始值文本与类型（`json`/`yaml`）组成。schema 与约束见
`specs/045-deploy-config/contracts/yaml-schema.md` §1：

```yaml
version: "3.0"
configs:
  - name: service_config
    data:
      - name: greeting
        value: |
          message: "hello from config"
          times: 3
        type: yaml
      - name: limits
        value: '{"maxConn": 100}'
        type: json
```

### 2. deploy.yaml 选择配置块

部署者在 `deploy.yaml` 的 artifact 中按名选择要启用的配置块（仅选择，不覆盖数据），
未选中的配置块不会提供给该产物。见 `specs/045-deploy-config/contracts/yaml-schema.md` §2：

```yaml
services:
  - artifact:
      path: //experimental/golang/grpc_hello_world/service/service.yaml
      name: service
      configs:
        - service_config
```

### 3. 运行时读取

被选中的配置数据由控制面物化为 ConfigMap 并投影到容器，平台注入环境变量
`DOMINION_CONFIG_DIR`（保留变量，用户 env 不可覆盖）指向配置根目录；文件布局为
`{DOMINION_CONFIG_DIR}/{block}/{key}`。详见 `specs/045-deploy-config/contracts/runtime-contract.md`。

SDK 通过该环境变量发现配置目录，服务代码不硬编码任何路径：

```ts
import { readConfig } from "@dominion/common-js-config";

interface Greeting {
  message: string;
  times: number;
}

const defaultGreeting: Greeting = { message: "hello", times: 1 };
const cfg = readConfig<Greeting>("service_config", "greeting", defaultGreeting);
```

### 4. 深度合并语义

配置文件内容（一律按 YAML 解析，兼容 json 与 yaml 类型条目）深度合并到 `defaults` 之上：

- 对象/映射递归合并：配置存在的键覆盖默认值，配置不存在的键保留默认值；
- 数组与标量整体替换（数组不按索引合并）；
- `undefined` 不覆盖，`null` 覆盖；
- `defaults` 不被修改（内部 `structuredClone`），返回全新对象；
- 跳过 `__proto__`/`constructor`/`prototype` 键，防原型污染。

### 5. 非敏感约束

config 承载**非敏感**数据（K8s ConfigMap）；敏感数据必须继续使用 secret 机制
（`specs/002-deploy-secret-config`）。config 与用户环境变量参数互不影响、独立工作。
读取未被 deploy.yaml 选中的配置块条目时 SDK 抛出异常，显式暴露运行环境与代码预期不一致。

## 依赖

- `js-yaml`：YAML 解析（v5，自带类型声明；JSON 是 YAML 子集，故 json/yaml 条目均可解析）。
- 无其他运行时依赖。
