# Contract: 组合清单（demo agent cordis.yml）

> R12 基座对齐改造 + 本 feature 新增行的完整契约。改造后 `experimental/dsh/demo/agent/cordis.yml` 终态。
> 行序无依赖语义（cordis await by inject）；分层仅为可读性（`projects/game/agent_v2/cordis.yml` 同款注释约定）。

## 1. 行清单（终态）

```yaml
# ── 基线行（经 @dominion/dsh-core / //third_party/dsh/core:runtime_pkg 物化，
#    不进 demo agent package.json）────────────────────────────
- { id: timer,          name: '@deepseek-ai/cordis-plugin-timer' }
- { id: invariants,     name: '@deepseek-ai/dsh-invariants' }
- id: system-prompt
  name: '@deepseek-ai/dsh-system-prompt'
  config:
    includeHarnessIdentity: false   # 047 spine config 移入（部署自有完整 system prompt）
    includeRuntimeContext: false    # 无运行时上下文注入
    persona: 'You are a helpful demo chat assistant.'   # deployment persona——被 preset
                                     # 的 dsh-persona 行遮蔽（V1 单测断言一次遮蔽生效）
# ── 服务声明行（demo agent package.json / BUILD npm_deps）────
- { id: llm,            name: '@deepseek-ai/dsh-llm' }
- { id: session,        name: '@deepseek-ai/dsh-session' }
- { id: tools,          name: '@deepseek-ai/dsh-tools' }
- { id: agents,         name: '@deepseek-ai/dsh-agent' }
- { id: agent-loop,     name: '@deepseek-ai/dsh-agent-loop' }   # 047 经 spine 闭包，直组显式化
- id: llm-deepseek
  name: '@deepseek-ai/dsh-llm-deepseek'
  config:
    apiKeyEnv: FAKE_LLM_API_KEY
    baseURL: !!js process.env.FAKE_LLM_BASE_URL
    models:
      - id: fake-chat-v1
        contextWindow: 100000
# ── 本 feature 新增 ─────────────────────────────────────────
- id: agent-presets
  name: '@deepseek-ai/dsh-agent-presets'
  config:
    default: demo-standard
    includeUserRoot: false        # 确定性：不扫机器 <dshHome>/.agent-presets
    roots:
      - path: !!js process.env.PRESET_TEMPLATES_ROOT   # 镜像内模板数据（R9）
        trust: system
      - path: !!js process.env.PRESET_WRITABLE_ROOT    # emptyDir；第一个且唯一 user root
        trust: user
- { id: preset-authoring, name: '@dominion/dsh-preset-authoring' }
```

**移除**：`agent-spine-demo` 行（R12）。**不挂**：invariant subpath 行、`llm-retry`（R12 差异理由）。

## 2. roots 与环境变量（R9）

| env | 注入方 | 值 |
|---|---|---|
| `PRESET_TEMPLATES_ROOT` | 镜像内固定路径（部署清单声明） | `/dominion/dsh-demo/agent/presets-templates`（artifact_pkg_js data 目录，随包分发） |
| `PRESET_WRITABLE_ROOT` | 部署清单声明 | emptyDir 卷挂载点（如 `/var/lib/dsh-demo/presets`） |

- 可写 root MUST 是 roster roots 中**第一个 user-trust root**（roster `copy()`/`remove()` 的目标约束）。
- 两 root 的 config 经 `!!js process.env.*` 读取——boot 前必须已设（deploy env 注入；本地/单测直设），未设则 roster 解析失败 → boot fail-loud（FR-009 语义延续）。

## 3. 模板 preset（部署数据，`experimental/dsh/demo/agent/presets-templates/`）

### 3.1 `demo-standard/agent.cordis.yml`

```yaml
- id: persona
  name: '@deepseek-ai/dsh-persona'
  config:
    text: 'You are the demo standard assistant.'   # 占位文案；副本物化时被 patch
```

`preset.yml`：`name: demo-standard` / `description: 仅 persona 行的标准模板`（copy 时 name 被 roster 丢弃）。

### 3.2 `demo-tools/agent.cordis.yml`

```yaml
- id: persona
  name: '@deepseek-ai/dsh-persona'
  config:
    text: 'You are the demo tools assistant.'
- id: demo-echo
  name: '@dominion/dsh-demo-echo'    # 裸包名从 host base（demo agent node_modules）解析（R8）
```

`preset.yml`：`name: demo-tools` / `description: persona + demo_echo 工具行模板`。

**模板约定**（R3，物化 patch 的前提）：无注释依赖、无 `!!js` 表达式、有且仅有一行 `name: '@deepseek-ai/dsh-persona'`。

## 4. BUILD.bazel 增删要点

- `npm_deps`：删 `@deepseek-ai/dsh-agent-spine-demo`；增 `dsh-session`、`dsh-tools`、`dsh-agent-loop`、`dsh-llm-deepseek`（保留）、`@deepseek-ai/dsh-agent-presets`、`@deepseek-ai/dsh-persona`（preset 行解析需物理在场）、`@dominion/dsh-demo-echo`（同）、`@dominion/dsh-preset-authoring`。
- `artifact_pkg_js.data_files`：增 `presets-templates`（模板随包分发）。
- `runtime_deps`：`//third_party/dsh/core:runtime_pkg` 不变（基线行物化）。
- `closure_audit_test`：expected 集随 package.json 重算（047 US3 语义保持：基线零插件、服务声明可溯源）。

## 5. workspace 与 catalog（pnpm-workspace.yaml）

- `packages` 增：`experimental/dsh/demo/agent-plugins/*`。
- `catalog` 增：`@deepseek-ai/dsh-agent-loop: 0.1.1-rc.2`、`@deepseek-ai/dsh-agent-presets: 0.1.1-rc.2`、`@deepseek-ai/dsh-persona: 0.1.1-rc.2`。
