# Quickstart: Game Agent v2 — dsh 迁移 Step 1 验证指南

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-28

本指南给出从零验证本 feature 的可执行步骤：构建/单测门禁 → 大型测试闭环（MVP 验收）→ 真实 GLM 端点手工冒烟。实现细节见 [tasks.md](tasks.md)；接口契约见 [contracts/](contracts/)。

## 0. 前置

- 仓库构建入口 bazel（`AGENTS.md`）；大型测试经 testplan skill（`tools/test/guitar`，规范 `style/large_test.md`）。
- 交付物（实现后）：
  - 插件 `common/js/dsh-plugins/llm-glm`（[contracts/glm-llm-plugin.md](contracts/glm-llm-plugin.md)）
  - 服务 `projects/game/agent_v2`（有状态 dsh 宿主，[contracts/conversation-api.md](contracts/conversation-api.md)）
  - 服务 `projects/game/web`（[contracts/web-frontend.md](contracts/web-frontend.md)）
  - 存量增量：proxy ConversationService 转发面（owner 亲和路由）、`projects/game/pkg/bind` server-streaming 泵、gateway `/api/v2` 路由（经 proxy 两跳）、fake-llm `/v1/responses`、game deploy.yaml（agent_v2+web+secret 绑定）
  - 大型测试 `projects/game/testplan/deploy_agent_v2.yaml`（部署拓扑）+ 既有 `system_test.yaml` 新增 agent_v2 suite（用例集与执行入口）

## 1. 构建与单测门禁（每次变更，constitution 原则 IV）

```bash
bazel build //projects/game/... //common/js/dsh-plugins/...
bazel test  //projects/game/... //common/js/dsh-plugins/...
```

覆盖：插件序列化/wire/协议义务单测、agent_v2 会话注册表/队列/历史单测、bind server-streaming 泵单测、proxy ConversationHandler 单测、gateway 路由单测、前端组件（US3 构造数据）与 reducer 单测、fake-llm Responses 端点单测。

## 2. 大型测试（MVP 验收，constitution 原则 VI——必须实际执行）

> 真实 GLM 端点**不进**大型测试（零外部网络/确定性，SC-001）；模型端点由 fake-llm 的 `/v1/responses` 替换（`GLM_LLM_TARGET` → 服务发现注入，[contracts/fake-responses-wire.md](contracts/fake-responses-wire.md) §4）。

```bash
# 经 testplan skill 实际执行（部署→测试→清理闭环）；suite 挂入既有 system_test.yaml（style/large_test.md §测试计划数量）：
guitar run projects/game/testplan/system_test.yaml
```

**部署拓扑**（deploy_agent_v2.yaml）：mongo + session + fake-llm + proxy + agent_v2 + web + gateway（ingress：`game.liukexin.com`，`/api/v1/`+`/api/v2/`→gateway、`/`→web；agent_v2 为有状态服务，`/api/v2` 经 gateway→proxy→agent_v2 实例两跳 owner 亲和路由）。

**用例集与预期**（全部通过 = 验收，零 failed/flaky）：

| # | 用例 | 断言要点（spec 场景锚点） |
|---|---|---|
| 1 | session 管理闭环（US4） | 新建→列表可见（含时间）→进入对话→返回→删除成功消失；与桌面共用的 saolei template 空间 |
| 2 | 端到端流式对话（US1-1/US2） | Send → 事件序 `turn_start→(THINK 块渐进)→(TEXT 块渐进)→turn_end{COMPLETED}`；text/think 分类可区分获取（SC-002）；纯 text 模板零 THINK 块（US2 场景 2） |
| 3 | 多轮连续性（US1-2） | 第二轮回复命中 history_keywords 模板（内容依赖首轮上下文） |
| 4 | 会话隔离（US1-3） | 并发双会话互不串扰、不阻塞 |
| 5 | 排队（FR-012） | 长延迟模板回合中 Send → 首帧 `queued{position}` → 前序 turn_end 后自动 turn_start 按序完成 |
| 6 | 刷新回填一致性（FR-014） | 回合完成后 `:history` 内容与流式终态一致；中途断开后重连回填前缀→完整 |
| 7 | 删除生命周期（FR-015/US4-3） | 回合中删除（DELETE /api/v1 + :dispose）→ 在途流收 `turn_end{ABORTED}`；同资源名新建为全新会话（无历史残留） |
| 8 | 模型故障恢复（Edge） | 失败模板 → `turn_end{ERROR}` 呈现；agent_v2/web 进程存活；后续轮次成功 |
| 9 | 非法输入（Edge） | 空文本 Send → 400；服务不崩 |
| 10 | web 页面托管（FR-013） | `GET /` 返回入口 HTML；静态资源可解析；（US3 渲染能力由前端组件单测覆盖） |

tools 渲染能力（US3/SC-002）在页面/接口层以构造数据验证（vitest 组件测试，§1 门禁内），端到端验证推迟至后续第一个工具 step（FR-005）。

## 3. 真实 GLM 端点手工冒烟（SC-003，交付文档记录）

```bash
# 1) 运维预置 k8s secret：llm-secrets 增加 key glm-codingplan（GLM codingplan API Key，
#    https://docs.bigmodel.cn/cn/coding-plan/quick-start 套餐页新建）
# 2) 部署 game 域（deploy.yaml 含 agent_v2 secret 绑定 glm-api-token → llm-secrets/glm-codingplan）
# 3) 浏览器打开 https://game.liukexin.com/
#    新建 session → 发送 "你好，介绍一下你自己" → 观察思考折叠（THINK）与正文（TEXT）流式渐进
#    第二条消息验证多轮连续性；刷新页面验证历史回填
```

预期：回复来自 `glm-5.2`（`GLM_MODEL` 可替换）；思考内容默认折叠可展开（US2）；token 不出现在任何页面/日志/交付物（SC-004——冒烟时可 grep 交付物复核）。

## 4. 存量零回归（SC-005）

```bash
bazel test //projects/game/gateway/... //projects/game/agent/... //projects/game/proxy/... //projects/game/pkg/bind/... //projects/game/desktop/...
# 既有 game 大型测试（如需）：guitar run projects/game/testplan/system_test.yaml
```

断言：既有 `/api/v1` 路由与行为不变（gateway 仅新增 `/api/v2` 注册，且经 proxy 转发）；proxy 既有 TeamService 行为与既有测试不变（仅增量注册 ConversationService 转发面）；`pkg/bind` v1 双向 `Bind` 行为不变（仅新增 server-streaming 泵）；agent/desktop 构建与测试不受影响。

## 5. 交付核对清单

- [ ] `bazel build //...` 全仓通过
- [ ] §1 单测全绿（含 US3 构造数据组件测试）
- [ ] §2 大型测试经 `guitar run` 实际执行，全部用例通过（非构建检查替代）
- [ ] §3 真实端点冒烟步骤记录于本文件与本节（可手工复验）
- [ ] §4 存量零回归
- [ ] 交付物零明文 token（代码/配置/镜像/文档，SC-004）
