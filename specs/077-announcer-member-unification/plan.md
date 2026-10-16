# Implementation Plan: 077 成员统一性补缺（announcer 同权 + team 统一消息流 + 在途广播快路）

**Branch**: `077-announcer-member-unification` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/077-announcer-member-unification/spec.md`

## Summary

为 hook 功能做前置分层补缺（survey `survey/saolei-hook-scripts.md` K4 + 快路，D8 裁定"独立 feature 先行"）：① team 插件成为宿主消费的**成员消息流权威**——`TeamMemberSource` 增可选 live 面（agent-only 事件搬运）+ `TeamHandle.events()` team 作用域统一流，宿主三路订阅收敛为单路（行为等价）；② 宿主 announcer **统一 member 化**——`TeamEntry` 统一持有三成员，名册/读取/视图/teardown 按成员属性派生，特例代码消失（076 呈现契约回归保持）；③ **在途广播快路**——插入式产出（message 单元）经与 drain 相同的构造路径 + source 投递面（agent 成员结构性属性：在途 → 不唤醒 inject）送达当前在途成员下一 step，锚点闭包 exactly-once、发言级顺序一致、驱动零影响（hook 落地前生产行为惰性，单测承载）。

技术方向（用户裁定 2026-10-16）：team 拥有流类型与语义、loop（orchestrator）零形状传递、宿主单路订阅 team 作用域流（扩展性：未来非 saolei 场景 loop 一行传递、宿主投影层零改动）；快路工具单元级有界偏差保留并契约披露。详见 [research.md](research.md) D1–D9。

## Technical Context

**Language/Version**: TypeScript（ESM，Node 运行时；`projects/game/agent_v2` 宿主 + `common/js/dsh-plugins/` 插件）

**Primary Dependencies**: `@deepseek-ai/dsh-session`（SessionEvent/SessionSeq）、`@deepseek-ai/dsh-llm`（UserMessage/MessageSourceMap team-broadcast 扩展）、`@deepseek-ai/dsh-agent`（agent inject/steer/whenIdle、根事件 agent/assistant-stream|status|error）、cordis（`ctx.team` 服务声明合并）；无新增第三方依赖。

**Storage**: N/A（进程内存态，stateful + owner 亲和；无持久化变更）

**Testing**: bazel（`bazel build`/`bazel test`，单测随每次变更）；大型测试经 testplan skill（`guitar run projects/game/testplan/system_test.yaml`，宪法 VI 实际执行闭环）

**Target Platform**: Linux 服务（agent_v2 gRPC 宿主进程内）

**Project Type**: library（dsh 插件）+ web-service（宿主内部结构，对外 API 零变更）

**Performance Goals**: 统一流为进程内同步扇出，无新增序列化/拷贝；快路构造一次（relay 时），与单次 drain 构造等价。

**Constraints**: 对外 proto/RPC/ChatEvent 帧形状零变更（web 前端零改动）；请求侧校验口径不变（076 FR-203）；投影行为等价为验收基准（非逐字实现保持）。

**Scale/Scope**: 改动面——team 插件（source/handle/relay）、saolei-loop（orchestrator 读取面 + 投递面供给 + AgentMember 策略）、agent_v2 宿主（session/history 投影入口）、三份既有契约 + README 修订。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁 | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（原则 V） | ✅ | tasks.md 阶段按 phase 声明三分类文档清单（本 plan 已锚定全部参考文档） |
| 2. 实现门禁（原则 II/III） | ✅ | 重构式变更：分层补缺与功能基建同 feature 交付（K4 先行即宪法 II 的落地）；接口优先：[contracts/](contracts/) 三份契约（member-event-stream / inflight-broadcast-fastpath / host-member-parity）先于实现定义全部接口面 |
| 3. 编译 + 单测门禁（原则 IV） | ✅ | 每次变更 `bazel build` + `bazel test`（相关 target），不单列 task；大型测试单独验收（SC-006） |
| 4. 引用、图表与终态门禁（原则 I/VII/VIII） | ✅ | 引用全部带仓库相对路径/URL；065/059/076 契约按终态表述直接修订（不留增量痕迹，research.md D9）；全部图表为 Mermaid 代码块 |
| 5. 大型测试验收门禁（原则 VI） | ✅ | testplan skill 实际执行 system_test.yaml 全套件，全部用例通过为验收（SC-006；快路行为由单测承载——spec Clarifications 2026-10-16 用户裁定，真实端到端快路场景由后续 hook feature 携带） |

Phase 1 设计后复核：新增接口（source live/投递面、TeamHandle.events、orchestrator teamEvents 读取面、MemberRuntime 判别联合）均有契约定义与验收面——通过。

## Project Structure

### Documentation (this feature)

```text
specs/077-announcer-member-unification/
├── plan.md              # This file
├── research.md          # Phase 0 output（D1–D9 决策记录）
├── data-model.md        # Phase 1 output（类型定义/生命周期/派生规则）
├── quickstart.md        # Phase 1 output（验证指南）
├── contracts/           # Phase 1 output
│   ├── member-event-stream.md          # 统一成员事件流契约
│   ├── inflight-broadcast-fastpath.md  # 在途广播快路契约
│   └── host-member-parity.md           # 宿主成员同权契约
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/team/src/
├── team.ts        # TeamMemberSource 扩展（live/deliverInFlight）、TeamLiveEvent、
│                  # TeamMemberEvent/TeamEventStream、TeamHandle.events、relay 快路扩展
├── broadcast.ts   # 既有构造路径复用（buildBroadcastMessage——零改动预期）
└── team.test.ts   # 统一流/快路单测

common/js/dsh-plugins/saolei-loop/src/
├── orchestrator.ts         # teamEvents() 只读读取面（零形状传递）；
│                           # createMember 组合投递面（source + AgentMember）
├── agent-member.ts         # AgentMember.deliverInFlight 策略（busy → inject / 静止 → no-op；
│                           # 异常吞噬回落）
├── orchestrator.test.ts    # 读取面/供给与缺席断言（全量既有用例回归保持）
└── agent-member.test.ts    # 投递面策略单测（inject 不唤醒、计数不含快路）

projects/game/agent_v2/src/
├── session.ts      # TeamEntry 统一成员持有（MemberRuntime 判别联合 + streamOff 单路退订）；
│                   # 单路订阅 + 信封分发；名册/读取/视图/teardown 属性派生（特例代码消失）
├── session.test.ts # 驱动面切换（经统一流）、行为断言不变
├── history.ts      # MemberCollector 输入入口化（applyEvent 信封分发入口；记账机械保留）
└── history.test.ts # 驱动面切换（经 applyEvent 入口）、记账断言不变

specs/065-agent-v2-team-refine/contracts/team-member-source.md   # 终态修订（接口/传输/投递面增量）
specs/059-agent-v2-team-mode/contracts/dsh-plugins.md            # §1 服务面终态修订（统一流/投递时机/投递面归属）
specs/076-agent-v2-optimize/contracts/{flow-orchestration,announcer-member}.md  # 广播投递时机 + 宿主成员事件消费改经统一流（loop 侧 announcer 读取面保留）/ 成员读取实现侧按 kind 派生
projects/game/agent_v2/README.md                                 # 成员投影与 announcer 描述同步
```

**Structure Decision**: 全部改动落在既有包内（team 插件 / saolei-loop / agent_v2 宿主），无新包、无新依赖行；契约修订按各契约"基线契约修订"节列出的映射执行。

## Complexity Tracking

无宪法违规需论证（Complexity Check 全部通过；新增面均为 K4/快路裁定所要求的最小接口——source 两个可选面 + handle 一个方法 + orchestrator 一个只读读取面）。
