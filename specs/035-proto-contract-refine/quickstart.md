# Quickstart: Proto 契约修正 验证指南

**Spec**: [spec.md](spec.md) | **Contracts**: [resource-fields.md](contracts/resource-fields.md), [frame-split.md](contracts/frame-split.md)

---

## 1. 前置条件

- Bazel 构建环境就绪（`bazel build //...` 通过）
- testplan skill 可用（`tools/test/guitar`）
- 现有大型测试基线（`projects/game/testplan/system_test.yaml`）通过

---

## 2. Part 1 验证：资源字段移除

### 2.1 单测验证

```bash
# Session handler 单测（移除 template/session_id 断言后通过）
bazel test //projects/game/session/handler:handler_test

# TeamProfile handler 单测（移除 body template 校验后通过）
bazel test //projects/game/prompt/handler:handler_test

# Desktop view model 单测（sessionViewFromProto 从 name 派生 sessionId）
bazel test //projects/game/desktop:view_model_test
```

**预期结果**：
- Session 响应中不再包含 `template` 和 `session_id` 字段。
- TeamProfile 创建不再需要 body `template` 字段；oneof spec 一致性校验仍生效（数据源为 parent 路径段）。
- `sessionViewFromProto` 从 `ParseSessionName(name)` 派生 `sessionId`，Wails JSON `sessionId` 字段不变。

### 2.2 大型测试验证

```bash
# 通过 testplan skill 执行
# guitar run projects/game/testplan/system_test.yaml
```

**关键验证点**：
- `testplan/session_test.go`：Session CRUD 正常；响应无 `template`/`sessionId` 字段，但 `name` 格式正确（`templates/{template}/sessions/{session}`）。
- `testplan/saolei_team_test.go`：TeamProfile 创建不再提交 body `template` 字段；template 一致性校验（提交不匹配的 oneof spec）仍返回 INVALID_ARGUMENT。

参考契约：[resource-fields.md](contracts/resource-fields.md) §2（行为变更）。

---

## 3. Part 2 验证：帧方向拆分

### 3.1 单测验证

```bash
# Proto roundtrip 测试（UserFrame/TeamFrame 序列化/反序列化）
bazel test //projects/game:proto_test

# Agent TS 单测（handler/turn-loop/operation-bridge 帧类型适配）
bazel test //projects/game/agent:agent_test

# Gateway/proxy 适配测试
bazel test //projects/game/gateway/cmd:main_test
bazel test //projects/game/proxy/handler:handler_test

# Desktop 适配测试
bazel test //projects/game/desktop:app_test
bazel test //projects/game/desktop/internal/chatstream:chatstream_test
```

**预期结果**：
- `proto_test.go`：UserFrame 和 TeamFrame 各自 roundtrip 正确；AgentFrame 不再存在。
- `operation-bridge.test.ts`：operation dispatch 帧包含完整信封字段（session_id/template_id/frame_id/create_time），修复了原缺陷。
- `handler.test.ts`：入站 UserFrame 无 sender 字段；出站 TeamFrame 无 sender 字段；用户输入路由门控不再依赖 sender 枚举（入站天然为用户）。
- `turn-loop.test.ts`：所有出站 TeamFrame 信封完整（`f.sessionId`/`f.templateId`/`f.frameId` 非空）。

### 3.2 大型测试验证

```bash
# 通过 testplan skill 执行完整端到端验证
# guitar run projects/game/testplan/system_test.yaml
```

**关键验证点**：
- `testplan/agent_dialog_test.go`：用户发送消息 → agent 回复 → 前端渲染全链路正常。WebSocket 帧使用 UserFrame（入站）/TeamFrame（出站）格式。
- `testplan/saolei_team_test.go`：Team connect lifecycle（CreateTeam → Connect → chat → operations → wait）；status ping-pong 正常。
- `testplan/agent_checkpoint_test.go`：重连后 ListMessages 返回的消息 `role` 字段正确（USER/AGENT，tool 消息为 AGENT）；SeedFromHistory 重放的 TeamFrame 中 role 正确。

参考契约：[frame-split.md](contracts/frame-split.md) §3（TeamFrame 信封完整性）、§7（客户端适配）。

---

## 4. 成功标准对照

| SC | 验证方式 | 预期 |
|----|----------|------|
| SC-001 | grep proto 字段 | Session/TeamProfile 无 template；Session 无 session_id |
| SC-002 | 单测 + 大型测试 | CRUD 行为不变，全部通过 |
| SC-003 | grep proto RPC | Connect 使用 UserFrame/TeamFrame，无 AgentFrame |
| SC-004 | 大型测试 | 端到端通信全链路通过 |
| SC-005 | operation-bridge 单测 | dispatch 帧含完整信封字段 |
