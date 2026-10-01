# Contract: 组合清单与声明面（0.1.7-rc.2 线）

**Feature**: specs/070-dsh-upgrade/spec.md（FR-001/FR-003/FR-004/FR-005）
**Research**: specs/070-dsh-upgrade/research.md（D1/D4/D5/D6/D12）

## 1. catalog 契约（根 `pnpm-workspace.yaml`）

- 全部 `@deepseek-ai/dsh-*` 条目（含新增）精确 pin `0.1.7-rc.2`；删除 `@deepseek-ai/dsh-agent-presets` 条目。
- 互锁值：`"@deepseek-ai/cordis": "~4.0.4"`、loader `1.0.5`、include `1.0.9`、group `1.0.4`、timer `1.1.6`、`node-addon-require-builtin: "0.1.6"`、`"@deepseek-ai/schemastery": "^3.18.4"`、`"@deepseek-ai/dsh-client-ui-primitives": "0.1.7-rc.2"`。
- lockfile 不变量：每个 `@deepseek-ai/*` 包名仅一个版本；cordis 物理副本唯一；`node-addon-require-builtin` 的 `linux-x64-gnu` optionalDep 在场。

## 2. agent_v2 组合清单（`projects/game/agent_v2/cordis.yml`）

**行集合（顺序分层；迁移期为 22 行）**。迁移期 = grpc-runtime 行已新增、memory host 行仍在使用旧通路（Phase 6 删除后回落到终态 21 行，收口见 specs/074-dsh-grpc-runtime/tasks.md T019）。模板不经行面表达：单一事实源 = `preset-authoring` 行 Config 的 `templates` 快照（contracts/preset-model-mapping.md §2.3/§3；research.md D4 终态结论——2026-09-28 用户裁定移除 `preset-*` 声明行，`@deepseek-ai/dsh-agent-preset` 包不进入本组合与声明面）：

```yaml
- { id: timer,              name: '@deepseek-ai/cordis-plugin-timer' }
- { id: llm,                name: '@deepseek-ai/dsh-llm' }
- { id: session,            name: '@deepseek-ai/dsh-session' }
- { id: session-projection, name: '@deepseek-ai/dsh-session-projection' }   # 新增（agent-loop/registry 注入）
- id: system-prompt
  name: '@deepseek-ai/dsh-system-prompt'
  config: { includeHarnessIdentity: false, includeRuntimeContext: false }    # 不变
- { id: tools,              name: '@deepseek-ai/dsh-tools' }
- { id: agents,             name: '@deepseek-ai/dsh-agent' }
- { id: agent-loop,         name: '@deepseek-ai/dsh-agent-loop' }
- { id: invariants,         name: '@deepseek-ai/dsh-invariants' }
- { id: invariant-session,  name: '@deepseek-ai/dsh-session/invariant' }
- { id: invariant-agent,    name: '@deepseek-ai/dsh-agent/invariant' }
- { id: invariant-scope,    name: '@deepseek-ai/dsh-scope/invariant' }
- { id: llm-retry,          name: '@deepseek-ai/dsh-llm-retry' }
- id: agent-preset-registry                                                   # 新增
  name: '@deepseek-ai/dsh-agent-preset-registry'
  config: { default: '' }                                                     # fail-loud（D5 门禁验证）
- { id: grpc-runtime, name: '@dominion/dsh-grpc-runtime' }                    # 迁移期新增（specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §1）
- id: llm-glm / llm-opencode-go / desktop-bridge / preset-authoring / team / memory / saolei-loop   # @dominion/* 行（saolei-loop 在 Phase 6 增 config.plugins；memory host 行在 Phase 6 删除）
```

- **池模板（player/planner）**定义在 `preset-authoring` 行 Config：`templates`（键 = 模板 id，值 = `{ name, description, plugins }`，persona 子行 `prefix` + 角色专属工具行）与 `templateRules`（required/forbidden，校验输入为 `templates` 快照）。
- **派生子行**（`@deepseek-ai/dsh-persona`、`@dominion/dsh-saolei`、`@dominion/dsh-memory`）经宿主 node_modules 按裸包名解析，相关包仍声明于本宿主 package.json 并随闭包交付（specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md §4 同型）。

**移除**：`agent-presets` 行（及其 `default/includeUserRoot/roots` config）、`preset-templates/` 目录、`PRESET_TEMPLATES_ROOT` env；`preset-*` 声明行方案不采用（D4 终态结论）。

**断言契约**：`projects/game/agent_v2/src/dsh.test.ts` 以迁移期 22 行精确断言（行 id 顺序、grpc-runtime 行名、registry 行 `default: ''`、`templates` 快照键与定义、persona 子行 `prefix`、templateRules）；Phase 6 删除 memory host 行后由 T019 收口为终态 21 行断言。fail-loud 门禁 = registry 行 config 恰为 `{ default: '' }` 且清单内**零** `@deepseek-ai/dsh-agent-preset` 行——roster 条目只能经 preset-authoring 物化进入，其 id 语法（`/^[a-z0-9][a-z0-9-]*$/`）拒绝空串，空 default 无匹配；保持"断言精确性"不放松（SC-002）。

## 3. demo 组合清单（`experimental/dsh/demo/agent/cordis.yml`）

同构迁移：`agent-presets` 行 + `presets-templates/` 目录 → `agent-preset-registry` 行 + `preset-authoring` 行 Config 的 `templates` 快照（键 = 模板 id：`demo-standard`（仅 persona 行）/`demo-tools`（persona + `@dominion/dsh-demo-echo` 工具行）；模板不经行面表达，2026-09-28 用户裁定，research.md D4 终态结论）。registry `default: ''`——会话创建本就必须显式 store preset（`session.ts` compose 前置 `INVALID_ARGUMENT`），registry default 恒惰性，空值是"无默认"的诚实表达；原 `demo-standard`"允许默认回落"能力随声明行移除而不存在。`system-prompt` 行的 deployment persona 以 0.1.7 Config 的 `personaPrefix` 键表达（`persona` 键已不存在）。LLM 行为 `llm-opencode-go`（`@dominion/dsh-llm-opencode-go`，route `opencode-go`）：0.1.7 官方 DeepSeek 面拆为 Messages transport 库 + 伴生 provider 插件，与本 demo 的 OpenAI Chat Completions fake-llm 不兼容，demo 改用仓库内 OpenAI 适配器（依据 `specs/070-dsh-upgrade/research.md` D7 终态结论）。

## 4. 声明面扩展（package.json + BUILD.bazel）

| 消费方 | 增（安装级） | 增（成行，同步进 npm_deps/data） | 删 |
|---|---|---|---|
| `projects/game/agent_v2` | settings、typert-protocol、util-values、brand、session-persistence、sandbox、sandbox-policy、ptc-runtime、user-approval | session-projection、agent-preset-registry | agent-presets、agent-preset |
| `experimental/dsh/demo/agent` | 上行 + settings、timeout | session-projection、agent-preset-registry、llm-opencode-go（row；workspace 包走 runtime_deps） | agent-presets、agent-preset |
| `third_party/dsh/core` | （11 包清单不变，版本升线） | — | — |
| `common/js/dsh-plugins/preset-authoring` | — | （deps）agent-preset-registry | agent-presets |
| 其余 plugins | 版本随 catalog 升线，依赖集合不变 | — | — |

> demo 的 LLM 行终态为 `llm-opencode-go`，不再声明 DeepSeek peer 面（`dsh-llm-deepseek`/`dsh-fs`/`dsh-credentials`/`dsh-attachment`/`dsh-anonymous-user-id`/`dsh-deepseek-llm-api-extensions`）；依据 `specs/070-dsh-upgrade/research.md` D7 终态结论（2026-09-28）。

> `@deepseek-ai/dsh-agent-preset` 不进入任何声明面（2026-09-28 用户裁定，模板经 preset-authoring `templates` 快照表达——research.md D4 终态结论）：agent_v2/demo package.json 与 BUILD 依赖、catalog 条目随之移除；闭包内无 deps/peers 边可达（`dsh-agent-preset-registry` 仅 devDependencies 引用），closure audit ② expected set 数据驱动收缩。

## 5. closure audit 契约（`experimental/dsh/demo/testplan/closure_audit_test.go`）

- 断言①：`d6CorePackages` 口径与核心 11 包清单**不变**（0.1.7 线下仍是 plugin-free 框架核心）。
- 断言②：expected set 数据驱动（core ∪ demo agent package.json deps 沿 tar 边闭包）——demo 声明面扩展后自动重算，测试代码零改动（如 tar 布局/解析不变）；2026-09-28 修订后 expected set 收缩（`@deepseek-ai/dsh-agent-preset` 移出 demo 声明根且无闭包边可达，见 §4 注记），断言② 仍零代码改动（D4 终态结论）。
- 断言③：版本唯一性 → "一个版本单元"硬校验。
- `third_party/dsh/core/version.ts`：`DSH_CORE_SNAPSHOT = 'dsh-core@0.1.7-rc.2'`（注释同步为 0.1.7 互锁事实）。

## 6. 验收

1. `bazel build //third_party/dsh/core:runtime_pkg //projects/game/agent_v2/... //experimental/dsh/demo/... //common/js/dsh-plugins/...` 通过。
2. `bazel test //...`（上述范围）全绿，含重写后的 dsh.test.ts / composition.test.ts。
3. closure audit ①②③ 通过（P4 门禁，随 demo testplan）。
4. lockfile 检查：无第二份 cordis、无 agent-presets 残留（SC-001）。
