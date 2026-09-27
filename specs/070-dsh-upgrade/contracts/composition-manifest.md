# Contract: 组合清单与声明面（0.1.7-rc.2 线）

**Feature**: specs/070-dsh-upgrade/spec.md（FR-001/FR-003/FR-004/FR-005）
**Research**: specs/070-dsh-upgrade/research.md（D1/D4/D5/D6/D12）

## 1. catalog 契约（根 `pnpm-workspace.yaml`）

- 全部 `@deepseek-ai/dsh-*` 条目（含新增）精确 pin `0.1.7-rc.2`；删除 `@deepseek-ai/dsh-agent-presets` 条目。
- 互锁值：`"@deepseek-ai/cordis": "~4.0.4"`、loader `1.0.5`、include `1.0.9`、group `1.0.4`、timer `1.1.6`、`node-addon-require-builtin: "0.1.6"`、`"@deepseek-ai/schemastery": "^3.18.4"`、`"@deepseek-ai/dsh-client-ui-primitives": "0.1.7-rc.2"`。
- lockfile 不变量：每个 `@deepseek-ai/*` 包名仅一个版本；cordis 物理副本唯一；`node-addon-require-builtin` 的 `linux-x64-gnu` optionalDep 在场。

## 2. agent_v2 组合清单（`projects/game/agent_v2/cordis.yml`）

**行集合（完整目标态，顺序分层）**：

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
- id: preset-player                                                          # 新增声明（模板）
  name: '@deepseek-ai/dsh-agent-preset'
  config:
    id: player
    name: <原 preset.yml name>
    description: <原 preset.yml description>
    plugins:
      - { name: '@deepseek-ai/dsh-persona', config: { prefix: <player 模板文案> } }
      - { name: '@dominion/dsh-saolei' }
- id: preset-planner                                                         # 新增声明（模板）
  name: '@deepseek-ai/dsh-agent-preset'
  config:
    id: planner
    name: <原 preset.yml name>
    description: <原 preset.yml description>
    plugins:
      - { name: '@deepseek-ai/dsh-persona', config: { prefix: <planner 模板文案> } }
      - { name: '@dominion/dsh-memory' }
- id: llm-glm / llm-opencode-go / desktop-bridge / preset-authoring / team / memory / saolei-loop   # @dominion/* 行不变
```

**移除**：`agent-presets` 行（及其 `default/includeUserRoot/roots` config）、`preset-templates/` 目录、`PRESET_TEMPLATES_ROOT` env（部署面同步清理）。

**断言契约**：`projects/game/agent_v2/src/dsh.test.ts` 重写为新行集合精确断言（行 id 顺序、registry 行 `default: ''`、模板声明行 config、persona 子行 `prefix`）；保持"断言精确性"不放松（SC-002）。

## 3. demo 组合清单（`experimental/dsh/demo/agent/cordis.yml`）

同构迁移：`agent-presets` 行 + `presets-templates/` 目录 → `agent-preset-registry` 行（`default` 指向 demo-standard，demo 允许默认回落——demo 无必选语义）+ `preset-demo-standard` / `preset-demo-tools` 声明行（persona `prefix` + demo-echo 子行）。`system-prompt` 行 config（`persona` 键）按 0.1.7 Config 校验（`persona` 全局键语义变化在编译/boot 验证，必要时迁移为 persona-prefix 形态）。

## 4. 声明面扩展（package.json + BUILD.bazel）

| 消费方 | 增（安装级） | 增（成行，同步进 npm_deps/data） | 删 |
|---|---|---|---|
| `projects/game/agent_v2` | settings、typert-protocol、util-values、brand、session-persistence、sandbox、sandbox-policy、ptc-runtime、user-approval | session-projection、agent-preset-registry、agent-preset | agent-presets |
| `experimental/dsh/demo/agent` | 上行 + llm-deepseek peer 面（fs/credentials/attachment/anonymous-user-id/deepseek-llm-api-extensions/settings/timeout） | session-projection、agent-preset-registry、agent-preset | agent-presets |
| `third_party/dsh/core` | （11 包清单不变，版本升线） | — | — |
| `common/js/dsh-plugins/preset-authoring` | — | （deps）agent-preset-registry | agent-presets |
| 其余 plugins | 版本随 catalog 升线，依赖集合不变 | — | — |

## 5. closure audit 契约（`experimental/dsh/demo/testplan/closure_audit_test.go`）

- 断言①：`d6CorePackages` 口径与核心 11 包清单**不变**（0.1.7 线下仍是 plugin-free 框架核心）。
- 断言②：expected set 数据驱动（core ∪ demo agent package.json deps 沿 tar 边闭包）——demo 声明面扩展后自动重算，测试代码零改动（如 tar 布局/解析不变）。
- 断言③：版本唯一性 → "一个版本单元"硬校验。
- `third_party/dsh/core/version.ts`：`DSH_CORE_SNAPSHOT = 'dsh-core@0.1.7-rc.2'`（注释同步为 0.1.7 互锁事实）。

## 6. 验收

1. `bazel build //third_party/dsh/core:runtime_pkg //projects/game/agent_v2/... //experimental/dsh/demo/... //common/js/dsh-plugins/...` 通过。
2. `bazel test //...`（上述范围）全绿，含重写后的 dsh.test.ts / composition.test.ts。
3. closure audit ①②③ 通过（P4 门禁，随 demo testplan）。
4. lockfile 检查：无第二份 cordis、无 agent-presets 残留（SC-001）。
