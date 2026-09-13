# Tasks: LLM 请求发送可靠性修复与 opencode-go 模型接入

**Input**: Design documents from `/specs/063-llm-reliability-opencode-go/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/ — 均已就绪。

**Organization**: Tasks grouped by user story（US1 瞬时失败恢复 P1 / US2 成员保持 P1 / US3 opencode-go 接入 P2）；编译+单测内嵌于各实现任务（宪章 IV，不单列 task）；大型测试验收独立成 Phase（宪章 VI）。

**文档清单约定**（宪章 V）：每个 Phase 开始前 MUST 完整阅读该 Phase 声明的全部文档（三分类：代码规范 / 官方文档 / 技术参考）。AGENTS.md 与本 feature spec 文件为必读，不在此重复列出。dsh 家族为公开上游仓库 [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（文档站 https://deepseek-harness.github.io/deepseek-harness/ ）的 0.1.1-rc.2 发布；清单同时给出上游 URL 与本地物化精确 pin 路径，本地路径即直读源（含正文、无需二次跳转）。各 phase 中的"现状代码"为编辑目标/参考实现，不属于文档三分类，单列列出。`style/javascript.md` 的 OTel 间接引用（`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`）经评估与本 feature 无关（无 OTel 装配变更）；`specs/019-js-test-reliability/` 的执行模型/mock 根因背景由 `style/javascript.md` 承载，其 shim 契约在 Phase 5 以 `specs/019-js-test-reliability/contracts/run-vitest-shim.md` 列入（新包测试 target 首次声明）；涉及包编辑的 phase 直接列入 `specs/048-js-esm-migration/contracts/esm-package-conventions.md`。`projects/game/fake-llm/README.md` 的其它指向：`specs/046-fake-llm-think-chunking/contracts/streaming-sequence.md` 为 Phase 2 所需的 chat SSE 序列契约（已列入）；`specs/046-fake-llm-think-chunking/quickstart.md` 与 `specs/044-llm-stall-recovery-fix/large-test-status.md` 经评估为既有 feature 的验证/执行记录、非实现参考，不列入。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (依赖地基)

**Purpose**: 新增依赖进入 catalog，llm-glm 可消费 dsh-timeout。

**文档清单**：

- 代码规范文档：`style/javascript.md`、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- 官方文档：[dsh-timeout（util/timeout 组）](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/util/timeout/README.md)；本地物化 `node_modules/.pnpm/@deepseek-ai+dsh-timeout@0.1.1-rc.2_@deepseek-ai+cordis@4.0.1_@deepseek-ai+dsh-invarian_13401da53fdd229a45877adbff64345d/node_modules/@deepseek-ai/dsh-timeout/README.md`（新增依赖的官方说明）
- 技术文章/技术参考文档：`specs/063-llm-reliability-opencode-go/plan.md`、`specs/063-llm-reliability-opencode-go/research.md`（D5 看护实现载体；遗留确认项：catalog 版本线与 bazel/pnpm 流程）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（包声明/依赖条目约定，T001 的 package.json 编辑面）

- [X] T001 在 `pnpm-workspace.yaml` catalog 增加 `"@deepseek-ai/dsh-timeout": "0.1.1-rc.2"`；在 `common/js/dsh-plugins/llm-glm/package.json` 的 dependencies 增加 `"@deepseek-ai/dsh-timeout": "catalog:"`；**手工**在 `common/js/dsh-plugins/llm-glm/BUILD.bazel` 三处追加 `:node_modules/@deepseek-ai/dsh-timeout`（`ts_project(:lib)` deps、`js_runtime_library(:runtime_pkg)` npm_deps、`vitest_test(:lib_test)` data——根 gazelle 仅注册 proto/go/python 语言（根 `BUILD.bazel:34-40`），不生成/维护 JS target，与 T012 同款说明）；执行依赖更新（`bazel run @pnpm -- --dir /mnt/code/dominion/common/js/dsh-plugins/llm-glm up`）→ `bazel mod tidy`；验证 `bazel build //common/js/dsh-plugins/llm-glm:lib && bazel test //common/js/dsh-plugins/llm-glm:lib_test`

**验证门禁**: `bazel build //common/js/dsh-plugins/llm-glm:lib && bazel test //common/js/dsh-plugins/llm-glm:lib_test` 通过。

---

## Phase 2: Foundational (fake-llm 故障注入设施，阻塞全部 story 的大型验收)

**Purpose**: fake-llm 支持有状态 transient 注入与 Responses wire 停滞（`specs/063-llm-reliability-opencode-go/contracts/fake-llm-fault-injection.md` 全量）。

**文档清单**：

- 代码规范文档：`style/golang.md`、[Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
- 官方文档：无
- 技术文章/技术参考文档：`specs/063-llm-reliability-opencode-go/contracts/fake-llm-fault-injection.md`、`specs/063-llm-reliability-opencode-go/research.md`（D14）、`projects/game/fake-llm/README.md`（能力/schema 契约入口）、`specs/046-fake-llm-think-chunking/contracts/template-config.md`（既有模板 schema 的 author-facing 契约，T002-T005 在其上增量）与 `specs/046-fake-llm-think-chunking/contracts/streaming-sequence.md`（chat SSE 帧序列/停滞语义基线，T003/T004 投影对照）
- 现状代码（非文档）：`projects/game/fake-llm/service/message_types.go`、`projects/game/fake-llm/service/responses.go`、`projects/game/fake-llm/service/handler.go`、`projects/game/fake-llm/service/matcher.go`、`projects/game/fake-llm/service/message_store_test.go`、`projects/game/fake-llm/service/BUILD.bazel`、`projects/game/testplan/agent_v2_helpers_test.go`（T005 触发词对齐常量）

- [ ] T002 在 `projects/game/fake-llm/service/message_types.go` 增加 `Transient` 模板字段（`times`/`http_status`/`retry_after`（秒）/`error_message`/`empty`/`failure`，yaml+json tag）与 per-template 并发安全计数器（`sync.Mutex`，仅成功匹配计数、`times` 缺省/0 视为 ∞）；含表驱动单测（`bazel test //projects/game/fake-llm/service:service_test`）
- [ ] T003 [P] 在 `projects/game/fake-llm/service/responses.go` 实现 transient 注入（http_status 直接返回+`Retry-After` 头、注入体 `error.message` 取 `error_message`（缺省最小 JSON）、empty=零内容块终局、failure×times）并将 `stall`/`stall_after` 投影到 `/v1/responses`（移除 `projects/game/fake-llm/service/responses.go:576-581` 故意排除及其注释，宪章 VII）；扩展 `projects/game/fake-llm/service/responses_test.go` 用例（depends on T002）
- [ ] T004 [P] 在 `projects/game/fake-llm/service/handler.go` 为 `/v1/chat/completions` 实现 transient `http_status`/`empty` 注入（与 Responses 行为一致，含 `error_message`）；扩展 `projects/game/fake-llm/service/handler_test.go`（depends on T002）
- [ ] T005 在 `projects/game/fake-llm/service/testdata/agent_v2_transient.yaml` 新增注入 fixtures（SC-001 `times:1,http_status:503,retry_after:1`（覆盖 FR-003 服务端退避指示路径）、SC-002 `times:6,http_status:500`（1 初始 + 默认 5 重试，恰耗尽默认预算）、SC-004a `stall`、SC-005 配额 `http_status:429`+`error_message:"insufficient quota"`、SC-005 认证 `http_status:401`，触发词对齐 `projects/game/testplan/agent_v2_helpers_test.go` 常量风格）；新增 `projects/game/fake-llm/service/testdata/opencode_go.yaml`（chat wire，SC-003 确定性驱动：关键词 `请开始扫雷` → planner 开场文本应答；team fixtures 为 responses_only、chat 端点不可用；工具链复用既有 `saolei.yaml`/`saolei_tools.yaml`）；确认 `service/BUILD.bazel` embedsrcs 覆盖并同步 `message_store_test.go` 的嵌入 fixture 锁定用例；回归 `bazel test //projects/game/fake-llm/...`（depends on T003, T004）

**验证门禁**: `bazel build //projects/game/fake-llm/... && bazel test //projects/game/fake-llm/...` 通过；既有 11 个 fixture 的加载/匹配用例保持全绿（不得回归）。

---

## Phase 3: User Story 1 - Send 瞬时失败自动恢复 (Priority: P1) 🎯 MVP

**Goal**: llm-glm 失败码对齐 dsh 共享分类学，组合中既有 llm-retry 真正生效；补齐 Retry-After、错误体分类（零回显）、空补全分类、流停滞看护、传输清理。

**Independent Test**: `bazel test //common/js/dsh-plugins/llm-glm:lib_test` 全绿——单次瞬时失败经重试恢复（fake fetchImpl 注入首败后成）、非瞬时类别零重试、看护窗口检出停滞、空补全分类 EMPTY_RESPONSE、消费方停止触发传输清理；端到端验收在 Phase 6。

**文档清单**：

- 代码规范文档：`style/javascript.md`、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、`style/javascript.md` 引用的 [TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)（nodenext ESM 规则）、[Vitest module mocking pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（测试注入面）
- 官方文档（dsh 上游仓库 0.1.1-rc.2；本地物化精确 pin 为直读源）：[dsh-timeout（util/timeout 组）](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/util/timeout/README.md)、[dsh-llm / dsh-llm-deepseek（llm 组）](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/README.md)、[cookbook adding-an-llm-adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)、[eventsource-parser（`createParser` 的 `onComment` 回调——T006 看护 pulse 的 API 面）](https://github.com/rexxars/eventsource-parser#readme)；本地物化：`node_modules/.pnpm/@deepseek-ai+dsh-timeout@0.1.1-rc.2_@deepseek-ai+cordis@4.0.1_@deepseek-ai+dsh-invarian_13401da53fdd229a45877adbff64345d/node_modules/@deepseek-ai/dsh-timeout/`（README.md + `lib/types/index.d.ts`：`idleWatchdog`/`timeoutOf`/`MAX_TIMER_DELAY_MS`）、`node_modules/.pnpm/eventsource-parser@3.1.1/node_modules/eventsource-parser/README.md`（`onComment` API 说明）、`node_modules/.pnpm/@deepseek-ai+dsh-llm@0.1.1-rc.2_@deepseek-ai+cordis@4.0.1_@deepseek-ai+dsh-attachment@0_de8559ed89b7370843bac1bad71a6196/node_modules/@deepseek-ai/dsh-llm/lib/index.js`（retry-policy 段 `:346-470`、`LlmError` options、`isQuotaExceededError`/`isContextWindowExceededError`/`RetryPolicySchema`/`resolveRetryPolicy`）、`node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_7ebcc03957095dbf1a287c95fbd2c153/node_modules/@deepseek-ai/dsh-llm-deepseek/lib/index.js`（官方先例：`:1289-1324` Retry-After+码映射、`:1386-1434` 看护、`:997-1007` 空补全、`:1422-1427` 清理）
- 技术文章/技术参考文档：`specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md`、`specs/063-llm-reliability-opencode-go/data-model.md`（§1-§2）、`specs/063-llm-reliability-opencode-go/research.md`（D1-D7）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` 引用的包级 ESM 契约）、`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`（既有契约，本 phase 终态化其 §1/§2/§3 表述）

- [ ] T006 [P] [US1] 修改 `common/js/dsh-plugins/llm-glm/src/wire.ts`：`createResponsesWire` 接受可选 `onComment` 回调（eventsource-parser 构造透传，作看护 pulse）；终局事件零内容块 → `finish{kind:'error', failure:{code:'EMPTY_RESPONSE'}}`；流结束无终局 → 码改 `STREAM_CLOSED`；载荷非法 JSON → `MALFORMED_RESPONSE`；同步更新 `common/js/dsh-plugins/llm-glm/src/wire.test.ts`（码断言从 `GLM_*` 迁移 + 空补全/onComment 新用例）
- [ ] T007 [US1] 修改 `common/js/dsh-plugins/llm-glm/src/adapter.ts`：非 2xx 处理重写——解析错误体 JSON 仅作 `isQuotaExceededError`/`isContextWindowExceededError` 分类（message 保持 `GLM endpoint returned HTTP <status>` 稳定文本、零体回显、原始体进 `cause`）；HTTP→码映射（401/403→`AUTH`、429→`RATE_LIMIT`、5xx→`SERVER`、400→`INVALID_REQUEST`/`CONTEXT_WINDOW_EXCEEDED`、其余→`HTTP_<status>`、任意+配额措辞→`QUOTA`）；`Retry-After` 头解析为 `LlmError` options `providerRetryAfterMs`；fetch/read 非 abort 抛出 → `TRANSPORT`；无 body → `EMPTY_RESPONSE`；同步更新 `common/js/dsh-plugins/llm-glm/src/adapter.test.ts`（判定表全分支用例，`specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md` §4 条目 1-2）(depends on T006)
- [ ] T008 [US1] 修改 `common/js/dsh-plugins/llm-glm/src/adapter.ts` 与 `common/js/dsh-plugins/llm-glm/src/index.ts`：`stream()` 以 `idleWatchdog`（`@deepseek-ai/dsh-timeout`）包裹 fetch+body 读取迭代（`onComment`→pulse、超时→`LlmError(...,'TIMEOUT')`）；独立 `AbortController` + 生成器 finally `abort()`+`reader.cancel()`+`releaseLock()` 传输清理；`GlmConfig` 增加 `retryPolicy?`/`streamIdleTimeoutMs?`（默认 300000）并 override `providerRetryPolicy()`（`resolveRetryPolicy` 透传）；schemastery Config 同步；测试：看护 fake timers（超时触发/comment 重置/长间隔不误判）、清理（提前 return 后 signal aborted+body cancelled）、caller abort → `finish{aborted}` 且不抛错（无重试触发面，FR-005）、policy 解析回退；修订 `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`（§1 依赖节补 `@deepseek-ai/dsh-timeout`、§2 `GlmConfig` 补 `retryPolicy?`/`streamIdleTimeoutMs?`、§3 错误码示例迁移共享码并增补 Retry-After/看护/空补全/传输清理/`providerRetryPolicy` 义务指向 `specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md`——宪章 VII 终态）（depends on T007）
- [ ] T009 [US1] 修改 `projects/game/agent_v2/cordis.yml` llm-glm 行：增加 `streamIdleTimeoutMs: !!js process.env.GLM_STREAM_IDLE_TIMEOUT_MS || 300000`；验证 `bazel build //projects/game/agent_v2/...` (depends on T008)

**Checkpoint**: US1 独立可测——`bazel test //common/js/dsh-plugins/llm-glm:lib_test` 全绿（重试经 llm-retry 的效果由 Phase 6 大型测试端到端验收）。

---

## Phase 4: User Story 2 - LLM 失败不改变团队激活成员 (Priority: P1)

**Goal**: 编排器观察成员 turn 成败（`agent/error` 订阅），失败走既有 fail 通道保持激活成员并产出结构化失败日志。**本 phase 与 Phase 3 可并行**（不同包文件）。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop:lib_test` 全绿——planner 失败 turn 后 activation 保持 planner、再 submit 重驱 planner、成功路径切换照常、cancel 不触发保持、失败日志字段完整。

**文档清单**：

- 代码规范文档：`style/javascript.md`、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、`style/javascript.md` 引用的 [TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Vitest module mocking pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
- 官方文档（dsh 上游仓库 0.1.1-rc.2；本地物化精确 pin 为直读源）：[dsh-agent-loop（core 组）](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/README.md)；本地物化 `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_094512eb081cf48bb52018a5a9cf9aa0/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（`:465-473` `agent/error` payload `{turn, step, error}` 与先于 idle 的时序、`:574-599` throw 路径）；[llm 组](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/README.md)；本地物化 `node_modules/.pnpm/@deepseek-ai+dsh-llm@0.1.1-rc.2_@deepseek-ai+cordis@4.0.1_@deepseek-ai+dsh-attachment@0_de8559ed89b7370843bac1bad71a6196/node_modules/@deepseek-ai/dsh-llm/lib/index.js`（`LlmError` 定义与 `code` 字段；`lib/types/index.d.ts` 契约类型）
- 技术文章/技术参考文档：`specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md`、`specs/063-llm-reliability-opencode-go/data-model.md`（§4 状态转移图）、`specs/063-llm-reliability-opencode-go/research.md`（D8-D10）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` 引用的包级 ESM 契约）、`specs/059-agent-v2-team-mode/data-model.md`（§5 切换锚点语义）、`specs/059-agent-v2-team-mode/spec.md:151`（失败可再次驱动语义）
- 现状代码（非文档）：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（`:614-618` 订阅先例、`:699-742` runPump/fail、`:789-794` 切换点）、`projects/game/agent_v2/src/session.ts`（`:706-727` logger 注入）

- [ ] T010 [US2] 修改 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：`createMember` 增加成员 ctx `agent/error` 订阅（写 `MemberRuntime.lastTurnFailure{code,message}`，`LlmError.code` 取码、非 LlmError 取 `UNKNOWN`）；`drive()` 返回成败结果（idle 解除后检查标记，成功清空）；`runPump` 失败结果走既有 `fail()` 通道（`current` 不变=成员保持、`lastError` 增加 `code` 字段、`OrchestratorLogger` 上下文类型扩展 `code`）；`pendingReview` 失败保留记录（既有语义确认）；同步 `projects/game/agent_v2/src/session.ts:711-718` 默认 logger 映射透传 `code` 字段（FR-012：`{session, phase, member, code, error}`）
- [ ] T011 [US2] 扩展 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`：fake member 增加 `failCurrentTurn(code)`（emit `agent/error` 后 emit idle）；用例——planner 失败保持（activation=planner、paused、lastError.code）+ 再 submit 重驱 planner + pause 期间入队消息在重驱后由原成员按 FIFO 消化 + 成功路径回归（`common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts:411-418` 既有语义）+ cancel 不触发保持 + review 失败保留 + fail 通道 logger 恰一条且字段完整（depends on T010）

**Checkpoint**: US2 独立可测——`bazel test //common/js/dsh-plugins/saolei-loop:lib_test` 全绿；与 US1 组合后的端到端（SC-002）在 Phase 6 验收。

---

## Phase 5: User Story 3 - opencode-go 模型接入与复合选择标识 (Priority: P2)

**Goal**: 新插件 `@dominion/dsh-llm-opencode-go`（Chat Completions wire）+ 选择面 `provider/model-id` 复合标识 + OPENCODE_* 部署布线。**本 phase 与 Phase 3/4 可并行**（T012 须待 T001，T016 与 Phase 4 串行，见 Dependencies）。

**Independent Test**: `bazel test //common/js/dsh-plugins/llm-opencode-go:lib_test //projects/game/agent_v2:lib_test //projects/game/web/frontend:all` 全绿——序列化/wire/分类用例、复合标识解析与联合目录、web 下拉复合值。

**文档清单**：

- 代码规范文档：`style/javascript.md`、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、`style/api.md` 及其引用 [AIP-132 List](https://google.aip.dev/132)、[AIP-134 Update](https://google.aip.dev/134)、[AIP-193 Errors](https://google.aip.dev/193)（ListModels/UpdateTeam 语义变更参照）、`style/javascript.md` 引用的 [TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Vitest module mocking pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
- 官方文档：[opencode-go 官方文档](https://opencode.ai/docs/zh-cn/go/)（endpoint/认证/会话头建议）；[swc `module` 配置](https://swc.rs/docs/configuration/modules)、[rules_swc tsconfig 锁步](https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md)、[swc #1348（swc 不读取 tsconfig——两文件需人工保持一致的原因）](https://github.com/swc/swc/issues/1348)（`style/javascript.md` 编译配置引用；T012 脚手架）；[eventsource-parser（`createParser` 的 `onComment` 回调——T014 看护 pulse 的 API 面）](https://github.com/rexxars/eventsource-parser#readme)；本地物化：`node_modules/.pnpm/eventsource-parser@3.1.1/node_modules/eventsource-parser/README.md`（`onComment` API 说明）；dsh 上游仓库 0.1.1-rc.2（本地物化精确 pin 为直读源）：[llm 组](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/README.md)、[cookbook adding-an-llm-adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)；本地物化 `node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_7ebcc03957095dbf1a287c95fbd2c153/node_modules/@deepseek-ai/dsh-llm-deepseek/lib/index.js`（Chat Completions serialize/translate 全套先例）、`node_modules/.pnpm/@deepseek-ai+dsh-llm@0.1.1-rc.2_@deepseek-ai+cordis@4.0.1_@deepseek-ai+dsh-attachment@0_de8559ed89b7370843bac1bad71a6196/node_modules/@deepseek-ai/dsh-llm/lib/types/index.d.ts`（`LlmAdapter`/`LlmRuntime` 契约）；目录快照来源 [models.dev api.json](https://models.dev/api.json)（`opencode-go` provider；值以 `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §2 表为准）
- 技术文章/技术参考文档：`specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md`、`specs/063-llm-reliability-opencode-go/contracts/model-selection.md`、`specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md`、`specs/063-llm-reliability-opencode-go/data-model.md`（§2-§3）、`specs/063-llm-reliability-opencode-go/quickstart.md`（§3 手工冒烟）、`specs/063-llm-reliability-opencode-go/research.md`（D11-D13）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新包 ESM 契约）、`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`（插件结构先例）、`common/js/dsh-plugins/llm-glm/README.md`（依赖 pin 决策——`specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §1 指向）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（新包 `vitest_test` target 的 shim 前置/退出码契约）
- 现状代码（非文档）：`common/js/dsh-plugins/llm-glm/{BUILD.bazel,package.json,tsconfig.json,.swcrc}`（T012 镜像源）、`projects/game/agent_v2/cordis.yml`、`projects/game/agent_v2/src/dsh.ts`、`projects/game/agent_v2/src/session.ts`、`projects/game/agent_v2/src/server.ts`、`projects/game/agent_v2/package.json`、`projects/game/agent_v2/BUILD.bazel`、`projects/game/agent_v2/service.yaml`、`projects/game/deploy.yaml`、`projects/game/testplan/deploy_agent_v2*.yaml`、`projects/game/testplan/system_test.yaml`、`projects/game/web/frontend/src/{api/agent.ts,components/TeamSettingsPanel.tsx,App.tsx}`

- [ ] T012 [US3] 创建新包脚手架 `common/js/dsh-plugins/llm-opencode-go/`（depends on T001：`@deepseek-ai/dsh-timeout` catalog 条目 + 新 workspace 包的 lockfile/node_modules 链接，依赖更新 `bazel run @pnpm -- --dir /mnt/code/dominion/common/js/dsh-plugins/llm-opencode-go up`，对齐 T001 流程）：`package.json`（`@dominion/dsh-llm-opencode-go`，deps `eventsource-parser`/`@deepseek-ai/schemastery`/`@deepseek-ai/dsh-timeout`，peers `@deepseek-ai/dsh-llm`/`@deepseek-ai/cordis`，devDeps `@types/node`/`typescript`/`vitest`，均 catalog:）、`tsconfig.json` + `.swcrc`（锁步，对齐 `common/js/dsh-plugins/llm-glm/` 同名文件）；**手工创建 `BUILD.bazel`**（根 gazelle 仅注册 proto/go/python 语言，不会生成 JS target；逐项镜像 `common/js/dsh-plugins/llm-glm/BUILD.bazel`：`npm_link_all_packages`、`ts_config`、`ts_project(:lib)`、`js_library(:pkg)`、`js_runtime_library(:runtime_pkg)`、`vitest_test(:lib_test)`）；验证 `bazel build //common/js/dsh-plugins/llm-opencode-go:lib //common/js/dsh-plugins/llm-opencode-go:runtime_pkg` 且 `bazel query //common/js/dsh-plugins/llm-opencode-go:lib_test` 可解析
- [ ] T013 [P] [US3] 实现 `common/js/dsh-plugins/llm-opencode-go/src/serialize.ts`：GenerateOptions → Chat Completions 请求体（system 首消息、text-only 字符串内容、reasoning 不回传、tool-call/tool-result 配对、tools 平铺非空才携带、temperature/max_tokens/stop 映射、image UNSUPPORTED_CONTENT），契约 `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §3 全表；`serialize.test.ts` 覆盖同契约 §6.1（depends on T012）
- [ ] T014 [P] [US3] 实现 `common/js/dsh-plugins/llm-opencode-go/src/wire.ts`：SSE `chat.completion.chunk` → StreamChunk（reasoning_content/content/tool_calls delta、finish_reason 缓存至 `[DONE]`、usage 尾 chunk 映射、`[DONE]` 哨兵收束、零块 stop → EMPTY_RESPONSE、无 `[DONE]` → STREAM_CLOSED、非法 JSON → MALFORMED_RESPONSE、onComment 透传），`specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §4 全表；`wire.test.ts` 覆盖同契约 §6.2（depends on T012）
- [ ] T015 [US3] 实现 `common/js/dsh-plugins/llm-opencode-go/src/adapter.ts` + `common/js/dsh-plugins/llm-opencode-go/src/index.ts`：`OpencodeGoChatAdapter`（失败分类/Retry-After/错误体零回显/providerRetryPolicy/idleWatchdog 看护/传输清理——按 `specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md` 的同套义务实现，llm-glm 改造实现（T007/T008）为可选对照、非前置；049 契约结构先例以本 feature 的 `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §1-§2 为准；条件 Authorization + `x-opencode-session`（options.sessionId）+ 自定义 User-Agent + attributionHeaders；provider route `opencode-go`；`POST {baseURL}/chat/completions`）；cordis 导出（name/inject/Config schemastery：apiKeyEnv 默认 `OPENCODE_API_KEY`、baseURL 默认 `https://opencode.ai/zen/go/v1`、models 默认目录 = `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §2 的全部 16 个官方 Chat Completions 路由模型（`glm-5.3`/`glm-5.3-flash`/`glm-5.2`/`glm-5.1`/`kimi-k3`/`kimi-k2.7-code`/`kimi-k2.6`/`longcat-2.0`/`deepseek-v4.1-flash`/`deepseek-v4-pro`/`deepseek-v4-flash`/`deepseek-v4-flash-vision-exp`/`mimo-v2.5`/`mimo-v2.5-pro`/`hy4-preview`/`hy3`；id/contextWindow 对齐 models.dev `opencode-go` 快照与官方文档表；未文档化 id 与 Responses/Anthropic 路由排除，contextWindow 为部署可调 advisory 快照）、retryPolicy/streamIdleTimeoutMs 可选）；`adapter.test.ts` 覆盖同契约 §6.3-6.4、§6.6-6.7（目录外 id advisory `resolveModel`：最小元数据、不拒绝，FR-014；默认目录 16 项完整性）（depends on T012, T013, T014）
- [ ] T016 [US3] 修改 `projects/game/agent_v2/src/session.ts`：`DEFAULT_MODEL` 改复合 `"glm-responses/" + (env.GLM_MODEL || "glm-5.3")`；新增复合标识解析（首个 `/` 切分，无斜杠/空段 → `INVALID_ARGUMENT` 含复合形态提示）；`validateModel` 按切分 provider 校验；`doMaterialize` 向 orchestrator 传切分后的 `{provider, model}`（裸值），成员运行时/成员视图的 `model` 保留**复合标识**——物化输入原文或 `${provider}/${model}` 重组（`session.ts:491-495` 的 `createMemberRuntime` 入参、`memberStateView` 输出；GetTeam/GetTeamMember `model` 回填复合标识，`specs/063-llm-reliability-opencode-go/contracts/model-selection.md` §3/§6.1）；修改 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：`TeamMemberOptions` 增加 `provider`，`agentOptions.provider` 取 `member.provider ?? deps.provider ?? TEAM_PROVIDER`；同步 `session.test.ts` 与 `orchestrator.test.ts` 用例（含 `agentOptions.model` 恒裸 id、成员视图/GetTeam `model` 为复合标识、system prompt `{{model}}` 渲染不变断言；depends on 契约冻结；与 Phase 4 同文件，须与 T010/T011 串行）
- [ ] T017 [US3] 修改 `projects/game/agent_v2/src/server.ts`：`ListModels` handler 改联合目录（`ctx.llm.listProviders()` 遍历 + 每路由 `listModels`/`resolveModelInfo`，条目 id 为复合标识，fail-loud）；`UpdateTeam` 校验路径适配复合值；同步 `server.test.ts`（mock 多 provider；联合顺序确定；裸 id 拒绝）(depends on T016)
- [ ] T018 [P] [US3] 修改 web 前端：`projects/game/web/frontend/src/api/agent.ts`（`Model`/`TeamMember` 值域注释更新为复合标识）、`projects/game/web/frontend/src/components/TeamSettingsPanel.tsx`（下拉 option 值/显示均为复合标识，"默认"空值语义不变）、`projects/game/web/frontend/src/App.tsx`（成员 chip 复合标识渲染）；同步 `TeamSettingsPanel.test.tsx`/`agent.test.ts`/`App.test.tsx` fixtures 与断言（depends on 契约冻结，可与 T016/T017 并行）
- [ ] T019 [US3] 部署布线：`projects/game/agent_v2/src/dsh.ts` 增加 `OPENCODE_LLM_TARGET`（dominion resolver）与 `OPENCODE_BASE_URL`（显式基址，兜底 `https://opencode.ai/zen/go/v1`）解析（结果写入 `env.OPENCODE_BASE_URL`，对齐 GLM 模式）与 `OPENCODE_API_KEY` 三级解析（env → `$DOMINION_SECRET_DIR/opencode-api-token` → 缺省告警）；`projects/game/agent_v2/package.json` dependencies += `"@dominion/dsh-llm-opencode-go": "workspace:*"`；`projects/game/agent_v2/BUILD.bazel` `artifact_pkg_js(server_pkg)` 的 `runtime_deps` += `//common/js/dsh-plugins/llm-opencode-go:runtime_pkg`（composed 行经 runtime_deps 入运行时闭包；npm_deps 对 workspace 包为 no-op）；`projects/game/agent_v2/cordis.yml` 增加 `llm-opencode-go` 行并注入 env（`apiKeyEnv: OPENCODE_API_KEY`、`baseURL: !!js process.env.OPENCODE_BASE_URL`、`models` 完整枚举 `specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md` §2 的 16 项（首项 id `!!js process.env.OPENCODE_MODEL || 'glm-5.3'`，其余 15 项字面量 + 各自 contextWindow；不得只注入首项——`models` 整体覆盖插件默认目录、破坏 FR-015）、`streamIdleTimeoutMs: !!js process.env.OPENCODE_STREAM_IDLE_TIMEOUT_MS || 300000`）；`projects/game/agent_v2/service.yaml` secrets += `opencode-api-token`；`projects/game/deploy.yaml` 增加 secret 绑定（`llm-secrets` 家族）；三个既有 testplan deploy YAML（`projects/game/testplan/deploy_agent_v2.yaml`/`deploy_agent_v2_drop.yaml`/`deploy_agent_v2_memory_down.yaml`）的 `agent-v2-test` env 增加 `OPENCODE_LLM_TARGET: dominion:///game/fake-llm:8080` 与合成凭据 `OPENCODE_API_KEY: test-opencode-token`（非真实 secret，用于端到端覆盖条件 Authorization 与零泄漏断言；fake-llm 忽略凭据）；新增 `projects/game/testplan/deploy_agent_v2_stall.yaml`（复用 `deploy_agent_v2.yaml` 服务清单，`agent-v2-test` env 增加 `GLM_STREAM_IDLE_TIMEOUT_MS: "2000"`；主拓扑 fixture 存在 3s/4s 正常 chunk 间隔，故看护窗口不得压在共享部署上）；`dsh.test.ts` 增 OPENCODE 解析用例 (depends on T015)

**Checkpoint**: US3 独立可测——单测全绿；手工冒烟见 `specs/063-llm-reliability-opencode-go/quickstart.md` §3。

---

## Phase 6: 大型测试验收与收尾 (Polish & Cross-Cutting)

**Purpose**: 宪章 VI 验收闭环（实际执行 testplan、全部用例通过）+ 文档终态化。

**文档清单**：

- 代码规范文档：`style/golang.md`、[Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)、`style/large_test.md`（**按模块归位、禁止按 spec/场景编号组织**；复用既有 helpers；不新建测试计划 YAML（SC-004a 独立 deploy 变体见 T019，suite/case 见 T022））
- 官方文档：无
- 技术文章/技术参考文档：`specs/063-llm-reliability-opencode-go/quickstart.md`、`specs/063-llm-reliability-opencode-go/contracts/fake-llm-fault-injection.md`（§5 场景接线表）、`specs/063-llm-reliability-opencode-go/research.md`（D15）、`specs/063-llm-reliability-opencode-go/checklists/requirements.md`（T024 终态复核）、`common/js/dsh-plugins/llm-glm/README.md`、`projects/game/agent_v2/README.md`、`projects/game/fake-llm/README.md`、`projects/game/testplan/README.md`（T024 终态化的既有读本）与 `specs/046-fake-llm-think-chunking/contracts/template-config.md`（T024 fake-llm README 基线）
- 现状代码/配置（非文档）：`projects/game/testplan/agent_v2_helpers_test.go`、`projects/game/testplan/system_test.yaml`、`projects/game/testplan/deploy_agent_v2*.yaml`

- [ ] T020 [P] 在 `projects/game/testplan/agent_v2_conversation_test.go`（会话模块）追加用例 + `projects/game/testplan/agent_v2_helpers_test.go` 常量：SC-001 单次瞬时恢复（fixture `times:1,http_status:503,retry_after:1`——覆盖 FR-003 服务端退避指示的 E2E 路径；`llm/retry` session 事件恰 1 条、turn COMPLETED）、SC-002 planner 失败保持（GetTeam activation=planner、再 Send planner 应答、`times:6` 耗尽后正常完成规划切 player）、SC-005 配额零重试（429 + `insufficient quota` 文案，零 `llm/retry`、恰一次 ERROR）、SC-005 认证零重试（401，零 `llm/retry`、恰一次 ERROR）、SC-003 会话流程（`opencode-go/<model>`：首次 Send `请开始扫雷` 由 `opencode_go.yaml` planner 开场模板确定性应答，随后 Send `start saolei` 触发 `saolei.yaml`/`saolei_tools.yaml` 工具链完成多轮+工具调用，再 Send `继续` 触发 `saolei.yaml` 的 `继续` 关键词开下一局并经工具链收束——覆盖 SC-003「多局游戏」；全部 turn 帧/历史/错误体无合成 token 值 `test-opencode-token`）；`bazel build //projects/game/testplan/...` 编译通过
- [ ] T021 [P] 在 `projects/game/testplan/agent_v2_preset_test.go`（preset/目录模块）更新断言：ListModels 联合目录（两 provider 复合标识、同名 `glm-5.3` 消歧）、UpdateTeam 复合标识/裸 id/空值三分支、GetTeam 成员 `model` 回填复合标识（FR-018，`specs/063-llm-reliability-opencode-go/contracts/model-selection.md` §3）；`bazel build //projects/game/testplan/...` 编译通过
- [ ] T022 [P] 新增 `projects/game/testplan/agent_v2_stall_test.go`（停滞看护模块）与 `projects/game/testplan/BUILD.bazel` 的 `go_largetest(name = "agent_v2_stall_test", size = "medium", ...)` target（停滞收敛含 6 次尝试 × 2s 看护 + 0.5s→10s 重试退避 ≈ 30s，另加物化/回合开销——small 60s 预算余量不足，`style/large_test.md` §测试用例 size 按实际需要；srcs/deps 对齐 `agent_v2_conversation_test` 形态：`agent_v2_stall_test.go`/`agent_v2_helpers_test.go`/`helpers_test.go`/`saolei_fixtures_test.go` + `SAOLEI_EMBEDSRCS`，按实际引用裁剪），在 `projects/game/testplan/system_test.yaml` 追加 `game-stall` suite（deploy `deploy_agent_v2_stall.yaml`；`cases` 引用 `//projects/game/testplan:agent_v2_stall_test`；`endpoint` 对齐既有 suite）：SC-004a——responses `stall` 模板 + `GLM_STREAM_IDLE_TIMEOUT_MS=2000`，断言停滞在窗口内被检出并按超时类走既有重试语义有界收敛（不无限挂起），用例在整体超时内完成；`bazel build //projects/game/testplan/...` 编译通过
- [ ] T023 大型测试验收（宪章 VI，经 testplan skill）：`guitar run projects/game/testplan/system_test.yaml` 完整部署→测试→清理闭环，**全部用例通过**（任何 failed/flaky 即验收未通过，修复后重跑直至全绿）；SC-004b：测试运行期间经 signoz 查询测试 env `game/agent-v2` error 日志存在 `{session, phase, member, code, error}` 字段记录且无合成 token 值 (depends on T020, T021, T022)
- [ ] T024 文档终态化（宪章 VII）：`common/js/dsh-plugins/llm-glm/README.md`（依赖节 + dsh-timeout、失败分类概述）、`projects/game/agent_v2/README.md`（OPENCODE_* env 说明更新）、`common/js/dsh-plugins/llm-opencode-go/README.md`（新包，结构对齐 llm-glm README）、`projects/game/fake-llm/README.md`（transient 能力/schema 契约入口：`specs/046-fake-llm-think-chunking/contracts/template-config.md` 为基线，063 契约为增量）、`projects/game/testplan/README.md`（fixture 字段/触发词与 SC-003 新模板）；复核 `specs/063-llm-reliability-opencode-go/checklists/requirements.md` 全项通过

**验证门禁**: T023 全绿为本 feature 的最终验收。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无依赖，立即开始。
- **Phase 2（Foundational）**: 与 Phase 1 无关（Go 侧独立），可与 Phase 1 并行；**阻塞 Phase 6**（大型验收的注入设施）。
- **Phase 3（US1）**: 依赖 Phase 1（dsh-timeout 依赖）。
- **Phase 4（US2）**: 与 Phase 3 完全并行（`saolei-loop` 包 vs `llm-glm` 包）。
- **Phase 5（US3）**: T012-T015（新插件）与 Phase 3/4 并行；T012 须待 T001（catalog 条目/workspace 链接）；T013/T014 须待 T012；T015 须待 T012/T013/T014（llm-glm 改造实现为可选对照、非前置）；T016 依赖契约冻结（已就绪）但与 Phase 4 同文件（T010/T011），两者串行；T017 依赖 T016；T018 可与 T016/T017 并行；T019 依赖 T015（cordis 行引用包），并含 agent_v2 运行时闭包装配（`package.json` workspace 依赖 + `BUILD.bazel` `runtime_pkg`）。
- **Phase 6（验收）**: 依赖 Phase 2-5 全部完成。

### User Story Dependencies

- **US1（P1）**: 依赖 Setup；独立可测（单测级）。
- **US2（P1）**: 无 story 间依赖（`lastError.code` 的值域由 US1 改善，但不阻塞）。
- **US3（P2）**: 失败处理义务复用 US1 的契约文档（`contracts/llm-failure-taxonomy.md`），代码上独立实现；无阻塞依赖。

### Within Each User Story

- 同文件任务串行（T007→T008 都在 `adapter.ts`；T010→T011 实现先于测试 harness；T016 与 T010/T011 同在 `saolei-loop/orchestrator.ts(+test)`，跨 story 亦须串行）。
- wire/serialize 等不同文件可并行；T013/T014 须待 T012（脚手架）完成后互为并行。

### Parallel Opportunities

- Phase 2 的 T003/T004（responses vs handler 两个 Go 文件；均须待 T002 schema 完成后）。
- Phase 3 的 T006（wire.ts）与 Phase 4 的 T010（orchestrator.ts）与 Phase 5 的 T012/T013/T014/T018（跨包/跨文件；T013/T014 在 T012 后）。
- Phase 5 内部：T013 ∥ T014（serialize.ts vs wire.ts，T012 完成后）；T018 与 T012-T015 并行；T016 与 Phase 4 同文件，不得并发。
- Phase 6 的 T020 ∥ T021 ∥ T022（三个不同测试文件）。

---

## Parallel Example: 跨 story 并行（三文件同时开工）

```bash
# 三个 P1/P2 线程互不冲突：
Task T006: "wire.ts onComment + EMPTY_RESPONSE（llm-glm 包）"   # Phase 3
Task T010: "orchestrator agent/error 订阅 + 保持（saolei-loop 包）" # Phase 4
Task T012: "opencode-go 包脚手架（新包；T013/T014 随后）"        # Phase 5
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 Setup（dsh-timeout 依赖）
2. Phase 3 US1（失败分类/重试/看护）→ 单测全绿即达成"Send 瞬时失败自动恢复"核心价值
3. **STOP and VALIDATE**：`bazel test //common/js/dsh-plugins/llm-glm:lib_test`；MVP 端到端验收可暂缓至 Phase 6

### Incremental Delivery

1. Setup → US1（MVP：重试生效）
2. + US2 → 残余失败的团队状态正确 + 可诊断日志
3. + US3 → opencode-go 模型供给与复合选择面
4. Phase 2（可与 1-3 并行推进）→ Phase 6 大型验收闭环（SC-001..005 全绿）

### Parallel Team Strategy

- 开发者 A：US1（Phase 3）；开发者 B：US2（Phase 4）；开发者 C：US3（Phase 5）；测试设施（Phase 2）随首个空档插入。

---

## Notes

- 编译+单测是每个实现任务的组成部分（任务描述内已含对应测试文件更新与 `bazel test` 验证，宪章 IV 不单列）。
- 大型测试用例按被测模块归位到既有文件（`style/large_test.md` 测试组织规则），SC 场景只是覆盖输入清单；SC-004a 因需独立看护窗口 env 使用独立 `game-stall` suite 与 deploy 变体。
- 同一文件的编辑串行进行（AGENTS.md 注释/编辑规范）。
- 中断恢复：每个 Phase 的 Checkpoint 即恢复点；Phase 6 T023 是最终验收门禁。
