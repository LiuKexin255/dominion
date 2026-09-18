# Quickstart: 058 dsh preset roster demo

> 端到端验证指南（构建/测试命令 + 验证场景）。实现细节见 [contracts/](contracts/) 与 tasks.md。
> 验收锚点：spec [Success Criteria](spec.md#success-criteria-mandatory)；大型测试 = 原则 VI 闭环。

## 1. 前置

- guitar / deploy 工具（大型测试；未安装先 `bazel run //:guitar_install` + `bazel run //:deploy_install`，`experimental/dsh/demo/README.md`）。
- dsh 依赖变更（catalog 三项 + workspace 两新包）后：`bazel run @pnpm -- --dir /mnt/code/dominion up`（或按 AGENTS.md pnpm 流程）→ `bazel run //:gazelle` → `bazel mod tidy`。

## 2. 构建与单测（每次代码变更，原则 IV）

```bash
bazel build //experimental/dsh/demo/... //common/js/dsh-plugins/... //third_party/dsh/core/...
bazel test  //experimental/dsh/demo/... //common/js/dsh-plugins/... //third_party/dsh/core/...

# 依赖闭包审计（047 US3 语义保持：基线零插件、服务声明可溯源）
bazel test //experimental/dsh/demo/testplan:closure_audit_test
```

单测覆盖（研究 R11 承载表）：物化算法（回滚/patch 正确性）、Store CRUD、demo-echo 成对注册、会话 compose 接线、deployment persona 遮蔽、scope 链/共享 mount、broken preset 行为、fake-llm Go 单测（system_keywords 命中/兼容）。

## 3. 大型测试（验收闭环，原则 VI）

```bash
guitar run experimental/dsh/demo/testplan/interface_test.yaml
```

- 部署三服务（含容器临时可写层 writable root 与模板数据 env 注入，contracts/composition-manifest.md §2）。
- **047 既有用例全部保持通过**（聊天往返/多轮/并发——回归门禁）。
- 新增 preset 场景用例（覆盖 spec US1×4 + US2×4 验收场景）：
  1. CreateConversation(preset=demo-tools) + SendMessage → system_keywords 断言 tools persona + `demo_echo` guidance（V1-1/V2-3）
  2. CreateConversation(不传 preset) → default（demo-standard）生效（V1-2）
  3. 同 preset 两会话行为一致（共享 mount 的行为面）+ 单测断言注册份数（V2-2）
  4. CreatePreset(template=demo-tools, persona=标记词) → 新会话即用（V3-1 热创作）
  5. UpdatePreset(persona=新标记词) → 旧会话回复不变、新会话命中新标记（V3-2 generation）
  6. DeletePreset → 旧会话继续对话、新 CreateConversation 拒绝（V4-3）
  7. 重复 CreateConversation：同 preset 幂等 / 异 preset 重建（R4）
  8. 未创建会话 SendMessage → FAILED_PRECONDITION（FR-002）
- 全部用例通过（零 failed/flaky）才算验收——构建检查不替代执行。

## 4. 手动冒烟（可选）

部署后对公共入口 `https://apitest.liukexin.com/experimental/dsh-demo`：

```bash
# 创作 preset（persona 带可辨识标记）
curl -X POST ".../presets?preset_id=smoke-1" -d '{"template":"demo-tools","persona":"You always mention SMOKE-MARK."}'
# 建会话 + 对话 → 期望回复携带 persona 痕迹（fake-llm 模板命中）
curl -X POST ".../conversations" -d '{"conversation_id":"c1","preset":"smoke-1"}'
curl -X POST ".../conversations/c1:sendMessage" -d '{"message":"hello"}'
```

## 5. 验证后收尾（FR-010）

全部用例通过后，按 `discussion-2026-09-08.md` §9 指引将实证结论写入 `survey/`（roster 机制实证对照、C1 实践摩擦、边界最佳实践与 agent_v2 迁移建议）；被实践修订的纸面结论在 survey 中对照记录。

## 6. 排障

- boot 失败（fail-loud）：先查 `PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT` 是否注入（roster roots 解析失败即退出）。
- 组合行为异常：roster 发现是热读取——`list()` 每次重扫，检查 writable root 目录内容与 stamp（mtime/size）。
- 大型测试失败排查：signoz skill 查 tracing/log（AGENTS.md 惯例）。
