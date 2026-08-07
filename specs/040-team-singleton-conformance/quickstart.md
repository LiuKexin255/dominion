# Quickstart: Team 单例 AIP-156 一致化 验证

**Feature**: `040-team-singleton-conformance` | **Spec**: [spec.md](spec.md)

> 端到端验证脚本，证明本特性按契约工作。引用 [contracts/api-contract.md](contracts/api-contract.md) 与 [contracts/team-rebuild-contract.md](contracts/team-rebuild-contract.md)，不重复契约细节。大型测试规范见 `style/large_test.md`，经 testplan skill（`tools/test/guitar`）执行。

---

## 前置

- 完整拓扑部署（gateway/proxy/agent/prompt/desktop），测试部署脚本：`projects/game/testplan/deploy_agent.yaml`（mongo `persistence: {enabled: false}`）。
- 至少一个 TeamProfile 存在（如 `templates/saolei/profiles/default`），供物化引用。
- 经 testplan skill 执行：`guitar run <plan.yaml>`（部署→测试→清理闭环，宪法原则 VI）。

---

## 场景 1：单例物化（P1，FR-001/FR-002/FR-004）

**验证**：Team 经 `UpdateTeam(allow_missing=true)` 物化，GetTeam 读回含 profile；无 CreateTeam RPC。

1. 创建 Session（`POST /api/v1/templates/saolei/sessions`）。
2. `PATCH /api/v1/templates/saolei/sessions/{sid}/team?allow_missing=true`，body `{"name":".../team","profile":"templates/saolei/profiles/default"}` → 200，响应 Team.profile = default。
3. `GET .../team` → 200，profile = default。
4. 断言：API 表面无 `POST .../team`（CreateTeam 已移除）。

**预期**：物化成功；GetTeam 含 profile。参考大型测试 `projects/game/testplan/saolei_team_test.go`（原 CreateTeam 用例改为 UpdateTeam PATCH）。

---

## 场景 2：幂等与多标签并发（P2，FR-002/FR-007）

**验证**：重复同 profile 物化幂等；并发收敛；无 ALREADY_EXISTS。

1. 对同一未物化 session 并发发起 2 个 `UpdateTeam(allow_missing=true, profile=default)`（模拟多标签）。
2. 二者均 200，Team 物化一次，owner 收敛同一 agent 实例。
3. 再 `UpdateTeam(allow_missing=true, profile=default)`（重复）→ 200，返回既有 Team（无错误）。
4. 断言：全程无 409 ALREADY_EXISTS（偏离已消除）。

**预期**：owner 分配 get-or-create + 竞态重读收敛；agent 单飞仅建一次 graph。参考 `contracts/api-contract.md` §2.5。

---

## 场景 3：profile 变更重建（P3，FR-005/FR-006/SC-003）

**验证**：变更 profile 重建 graph，历史零丢失；in-flight 拒绝。

1. 物化 Team（profile=P1），进行若干 turn 产生 playerMessages/plannerMessages 历史。
2. 记录历史消息计数与内容（`GET .../team/agents/player/messages`、`.../planner/messages`）。
3. `PATCH .../team` body `{"name":".../team","profile":"templates/saolei/profiles/P2"}`（无 allow_missing 也行，已存在）→ 200，响应 profile=P2。
4. 再次拉取两 agent 历史 → 计数与内容与步骤 2 一致（零丢失，SC-003）。
5. 发起一个 turn，在其 in-flight 期间 `PATCH .../team`（profile=P3）→ 409 FAILED_PRECONDITION；既有 Team/turn 不受影响。
6. turn 结束后再 `PATCH .../team`（profile=P3）→ 200；下一 turn 使用 P3 model/prompt（fake provider 可断言 model 切换）。

**预期**：重建复用 checkpointer（`projects/game/agent/src/team/graph.ts:146`），buffer/bridge/sink/MCP 复用。参考 `contracts/team-rebuild-contract.md`。

---

## 场景 4：错误语义（FR-003/FR-008/FR-009）

1. 未物化 session `GET .../team` → 404 NOT_FOUND（无懒加载）。
2. 未物化 session `PATCH .../team?allow_missing=false` → 404 NOT_FOUND（AIP-134 标准）。
3. `PATCH .../team` profile 模板段（如 `templates/other/...`）与 name 模板段（`saolei`）不一致 → 400 INVALID_ARGUMENT。
4. `PATCH .../team` profile 引用不存在的 TeamProfile → 404 NOT_FOUND（透传 PromptService）。

---

## 验收门禁

- `bazel build //projects/game` + 各服务 `bazel test`（proxy/agent/desktop）全通过（宪法原则 IV）。
- testplan skill 实跑上述场景（部署→测试→清理闭环），**全部用例通过**（宪法原则 VI；任何 failed/flaky 视为未通过，须修复重跑）。
- 契约审查：API 表面符合 AIP-156（SC-001/SC-004）。
