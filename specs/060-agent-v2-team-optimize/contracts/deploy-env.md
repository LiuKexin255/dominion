# Contract: 产物位置保留环境变量与部署清单收敛

> deploy 平台（`projects/infra/deploy/runtime/k8s` + `tools/release/deploy`）注入契约与 `projects/game` 部署清单收敛。
> 决策依据：[research.md](../research.md) R1/R3/R11。

## 1. `DOMINION_ARTIFACT_DIR`（新增平台保留变量）

- **注入**：每个 **artifact 服务**（stateful/stateless workload）容器 env 追加 `DOMINION_ARTIFACT_DIR`，值 = 该服务产物在容器内的放置目录（`/dominion/{app}/{service}`，打包布局由 `artifact_pkg_go`/`artifact_pkg_js` 决定，`tools/release/deploy/README.md` §镜像布局）。infra 服务（mongodb 等）不注入（非 artifact workload）。
- **注入位置与顺序**：保留变量块（`SERVICE_APP`/`DOMINION_ENVIRONMENT`/`POD_NAMESPACE` 之后）追加；用户 env 同名声明被平台值覆盖（K8s last-wins，对齐 `DOMINION_SECRET_DIR`/`DOMINION_CONFIG_DIR` 既有语义，builder_test.go L1906-1955 同型断言）。
- **保留名清单**：`tools/release/deploy/README.md` 保留变量列表（§服务环境变量）补入 `DOMINION_ARTIFACT_DIR`；常量库收录（const-lib.md §1）。
- **用途边界**：声明**打包产物放置位置**——服务据此定位镜像内自带数据（如 agent_v2 preset 模板根）；不承载运行时可写路径语义（可写需求走系统临时目录或平台 secret/config 面）。

## 2. agent-v2 模板根派生（消费侧）

解析顺序（`projects/game/agent_v2/src/dsh.ts`，boot 前完成并注入组合 env——既有宿主注入模式）：

1. `PRESET_TEMPLATES_ROOT` 已设 → 直用（本地/测试显式覆盖）。
2. 否则 `DOMINION_ARTIFACT_DIR` 已设 → `${DOMINION_ARTIFACT_DIR}/preset-templates`。
3. 两者皆缺 → boot fail-loud（明确错误指明两个变量名；对齐 roster 根解析失败退出语义）。

## 3. 部署清单收敛（终态）

- `projects/game/deploy.yaml` agent-v2 服务块：env 字段**删除**（`PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT` 移除；secret 绑定保留——env 块仅在仍有非 secret 配置时存在，本例 env 块整体移除）。
- `projects/game/testplan/deploy_agent_v2.yaml`、`deploy_agent_v2_drop.yaml`、`deploy_agent_v2_memory_down.yaml`：同步骤移除两项 preset env。
- 原则：**默认设置不在部署清单声明**（用户"非必要不配置"裁定）；清单只承载偏离默认的配置。

## 4. 兼容性

- 平台新增注入为非破坏性加法：既有服务获得新 env 但无消费方时无行为影响。
- 部署清单移除 env 与服务侧默认派生**同批交付**（清单先行移除而服务未派生 = boot fail-loud，交付原子性由同一 feature 变更承载）。
