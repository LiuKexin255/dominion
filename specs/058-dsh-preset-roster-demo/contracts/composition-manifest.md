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
      - path: !!js process.env.PRESET_WRITABLE_ROOT    # 容器临时可写层（§2）；第一个且唯一 user root
        trust: user
- { id: preset-authoring, name: '@dominion/dsh-preset-authoring' }
```

**移除**：`agent-spine-demo` 行（R12）。**不挂**：invariant subpath 行、`llm-retry`（R12 差异理由）。

## 2. roots 与环境变量（R9）

| env | 注入方 | 值 |
|---|---|---|
| `PRESET_TEMPLATES_ROOT` | 镜像内固定路径（部署清单声明） | `/dominion/dsh-demo/agent/presets-templates`（artifact_pkg_js data 目录，随包分发） |
| `PRESET_WRITABLE_ROOT` | 部署清单声明 | 容器临时可写层固定路径 `/var/lib/dsh-demo/presets`（非卷挂载点，见下） |

- 可写 root 位于容器临时可写层，无卷声明：deploy 平台不提供用户服务卷通道——deploy v3 schema 的 artifact 服务属性仅 `path`/`name`/`replicas`/`env`/`secrets`/`configs` 且 `additionalProperties: false`（`tools/release/deploy/pkg/schema/deploy.schema.json`）；k8s builder 为用户服务生成的卷均为平台管理的只读投影卷（TLS/secrets/configs，`projects/infra/deploy/runtime/k8s/builder.go`，唯一可写卷是 infra MongoDB 专用的数据卷）。根目录无须预置——roster 首次 `copy()` 自动创建。
- 临时可写层随 Pod 重建即丢失——与内存态 preset store 的重启丢失已知限制一致（`specs/058-dsh-preset-roster-demo/spec.md` Edge Cases「服务重启」）；模板 root 为镜像数据，不受影响。
- 可写 root MUST 是 roster roots 中**第一个 user-trust root**（roster `copy()`/`remove()` 的目标约束）。
- 两 root 的 config 经 `!!js process.env.*` 读取——boot 前必须已设（deploy env 注入；本地/单测直设），未设则 roster roots 解析失败 → boot fail-loud：boot 任一步失败即非零退出并携带诊断、不存在半启动状态（fail-loud 定义：`specs/047-dsh-chat-demo/contracts/dsh-agent-service.md` §1；两 root 配置语境：`specs/058-dsh-preset-roster-demo/spec.md` FR-001）。

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

- `npm_deps`：删 `@deepseek-ai/dsh-agent-spine-demo`；增 `dsh-session`、`dsh-tools`、`dsh-agent-loop`、`@deepseek-ai/dsh-agent-presets`、`@deepseek-ai/dsh-persona`（preset 行解析需物理在场；`dsh-llm-deepseek` 保留）。npm_deps 通道仅适用于 registry 包——link target 携带 store 真实文件；workspace 包的 link target 不含可打包文件，经 npm_deps 打包是 no-op。
- workspace 包通道：`@dominion/dsh-demo-echo` 与 `@dominion/dsh-preset-authoring` 各自以 `js_runtime_library :runtime_pkg` 暴露 `JsRuntimePackageInfo`，经 agent 的 `runtime_deps` 进入打包闭包（artifact_pkg_js 拷贝为 `node_modules/{pkg_name}/` 真实文件，与既有 `//common/js/*:runtime_pkg` 同型）。
- `artifact_pkg_js.data_files`：增 `presets-templates`（模板随包分发）。
- `runtime_deps`：`//third_party/dsh/core:runtime_pkg` 不变（基线行物化）。
- `closure_audit_test`：expected 集随 package.json 重算（047 US3 语义保持：基线零插件、服务声明可溯源）；cordis 行解析器 fail-closed——每个 `- ` item 必须产出一行，无法识别的行格式报错而非静默跳过。

## 5. workspace 与 catalog（pnpm-workspace.yaml）

- `packages` 增：`experimental/dsh/demo/agent-plugins/*`。
- `catalog` 增：`@deepseek-ai/dsh-agent-loop: 0.1.1-rc.2`、`@deepseek-ai/dsh-agent-presets: 0.1.1-rc.2`、`@deepseek-ai/dsh-persona: 0.1.1-rc.2`。
