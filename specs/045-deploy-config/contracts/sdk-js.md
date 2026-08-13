# Contract: JS/TS Config SDK (`common/js/config`)

**Feature**: 045-deploy-config | **Spec**: [spec.md](../spec.md) | **Runtime**: [runtime-contract.md](runtime-contract.md)

TypeScript 配置读取 SDK，遵循 JS/TS 社区 `structuredClone` + 递归深合并惯用法。包路径 `common/js/config`，包名 `@dominion/common-js-config`。

---

## 1. 公共 API

```ts
import { readConfig } from "@dominion/common-js-config";

/**
 * 读取配置条目 (block, key)，将其内容深度合并到 defaults 之上，
 * 返回合并后的新对象。defaults 不被修改。
 *
 * 配置文件通过平台注入的 DOMINION_CONFIG_DIR 环境变量发现，
 * 路径为 {DOMINION_CONFIG_DIR}/{block}/{key}。
 * 文件一律以 YAML 解析（兼容 json 与 yaml 内容）。
 *
 * 深度合并语义：对象递归合并；数组和标量由配置值整体替换；
 * undefined 不覆盖，null 覆盖。
 *
 * 失败时抛出异常（Error），错误情况：
 *   - DOMINION_CONFIG_DIR 未设置
 *   - 配置文件不存在（配置块未被 deploy.yaml 选择）
 *   - 文件内容不可解析
 */
export function readConfig<T extends object>(
  block: string,
  key: string,
  defaults: T,
): T;
```

### 用法示例

```ts
import { readConfig } from "@dominion/common-js-config";

interface Greeting {
  message: string;
  times: number;
}

const defaultGreeting: Greeting = { message: "hello", times: 1 };

const cfg = readConfig<Greeting>("service_config", "greeting", defaultGreeting);
// 若配置文件内容为 "message: hi\ntimes: 5"（yaml），则 cfg = { message: "hi", times: 5 }
```

---

## 2. 实现规范

### 深度合并路径（research.md R3）

1. 读取文件 `{DOMINION_CONFIG_DIR}/{block}/{key}`（`fs.readFileSync`，同步，启动期一次性）。
2. `yaml.load(content)` 解析为对象（cfgObj）。用 `js-yaml`（新增 catalog 依赖）。
3. `structuredClone(defaults)` 深拷贝 → base。
4. 递归深合并 cfgObj over base：
   - 两者均为 plain object → 逐键递归合并；
   - 否则（标量/数组/类型不一致）→ cfgObj 值替换；
   - `undefined` 不覆盖；`null` 覆盖。
5. **原型污染防护**：跳过 `__proto__`、`constructor`、`prototype` 键。
6. 返回 base（类型断言为 `T`）。

### 同步读取

启动期一次性同步读取（`readFileSync`）是 JS 配置库社区共识（node-config 等）。配置不在热路径重复读取。

### 不修改 defaults

`structuredClone(defaults)` 生成独立深拷贝，原始 defaults 不被触碰。

### 错误抛出

用 `Error` 抛出，消息含 block、key、路径；不泄漏文件内容。

---

## 3. 依赖

- `js-yaml`：YAML 解析（兼容 JSON）。加入 `pnpm-workspace.yaml` catalog 统一版本管理。
- ~~`@types/js-yaml`~~：不需要——js-yaml v5 已用 TypeScript 重写并自带类型声明（`dist/js-yaml.d.ts`，package.json `types` 字段）；`@types/js-yaml@4` 描述的是 v4 API（`export =` 命名空间形态），与 v5 的 flat named exports 不兼容（[v5 迁移说明](https://github.com/nodeca/js-yaml/blob/master/docs/migrate_v4_to_v5.md)）。
- 无其他运行时依赖（保持 `common/js/*` 零业务依赖风格）。

---

## 4. 包文档职责（FR-019）

包 README 与导出函数 JSDoc **必须**说明 deploy 配置中 config 的完整使用方式（与 Go SDK 对称）：

1. **service.yaml 声明**：顶层 `configs` 定义配置块池（含示例）。
2. **deploy.yaml 选择**：artifact `configs` 选择配置块名。
3. **运行时读取**：`DOMINION_CONFIG_DIR` 发现 + `readConfig(block, key, defaults)`。
4. **深度合并**：增量覆盖语义。
5. **非敏感约束**。

文档引用：`specs/045-deploy-config/contracts/yaml-schema.md`、`runtime-contract.md`。

---

## 5. 测试要求

- 单测覆盖深度合并矩阵（[data-model.md](../data-model.md) "Deep Merge Semantics"）。
- 单测覆盖原型污染防护（`__proto__`/`constructor`/`prototype` 不生效）。
- 单测验证 `defaults` 不被修改。
- json-type 与 yaml-type 文件均正确解析。
- 使用 vitest（与 `common/js/*` 其他包一致）。
