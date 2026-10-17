# Data Model: 078 saolei JS hook

**Spec**: [spec.md](spec.md) | **决策依据**: [research.md](research.md) D1–D9
**实现锚点**: `common/js/dsh-plugins/saolei/src/game/runtime.ts`、`common/js/dsh-plugins/saolei-hooks/src/`（新）、`common/js/dsh-plugins/saolei-loop/src/{orchestrator,index}.ts`、`projects/game/agent_v2/cordis.yml`

全部状态为 team 物化生命周期的进程内存态（stateful + owner 亲和），无持久化存储变更（spec FR-005）。

## 1. 游戏基础面（`@dominion/dsh-saolei`）

### 1.1 GameRuntimeDeps（扩展；既有字段语义不变）

```ts
export interface GameRuntimeDeps {
  readonly sessionName: string;    // 既有
  readonly dispatch: ...;          // 既有
  readonly boardApi: ...;          // 既有

  /** 新增（research D5）：棋盘变动事件 seam——可选回调，operate 批量落账且
   * 触发条件满足时同步调用一次；载荷 = 该次落账的 GameLogEntry。
   * 缺省（undefined）时行为与现状完全一致（seam 无副作用）。 */
  readonly onBoardChange?: (entry: GameLogEntry) => void;
}
```

`createGameRuntime(sessionName, desktopBridge, deps?)` 构造签名随之扩展（可选第三参承载扩展 deps；既有两参调用不变）。

### 1.2 触发条件（机械定义；spec FR-001）

`operate` 内、`saolei_operate` 条目 push 后同步求值：

```text
onBoardChange 触发 ⇔ (deps.onBoardChange 存在)
                  ∧ (executed > 0)                // 实际变动：全 skip 批量不触发
                  ∧ (gameStatus(finalState) === "playing")  // 终局批量不触发
```

不触发路径（零回调）：空批量/结构拒绝/识别失败/派发失败的提前返回（无落账）、全 skip 批量、终局批量（`won`/`lost`）、`saolei_init` 落账、`(game-end)` 追加条目。

**回调异常边界**：回调抛错由订阅方（hooks 引擎的 handler，§3.3）内部吞噬并转为播报/日志，**不得**改变 `operate` 返回值（spec US4 场景 5）；游戏基础侧不做 try/catch（订阅方边界责任，[contracts/board-change-event.md](contracts/board-change-event.md) §3）。

## 2. hook 注册表与脚本（`@dominion/dsh-saolei-hooks`）

### 2.1 注册表

```ts
/** 注册项：名称（身份）+ 预编译脚本 + 注册序（首个 set 的位置）。 */
interface HookEntry {
  readonly name: string;
  readonly script: vm.Script;        // 注册期 new vm.Script(调用形态包装源码)——语法错误在 set 时反馈，执行期跨 context 复用
  readonly source: string;           // 原始函数体源码（list 回显）
  readonly order: number;            // 注册序单调递增；同名 set 覆盖时保留原 order（research D3）
}

class HookRegistry {
  set(name, source): { ok: true } | { ok: false; rejection: string };  // 上限/banned-pattern/编译校验
  remove(name): { ok: true } | { ok: false; rejection: string };
  list(): readonly { name: string; order: number; length: number }[];  // 按注册序
  entriesInOrder(): readonly HookEntry[];                              // 事件处理器消费
}
```

**状态迁移**：team 物化时随 hooks unit 构造创建 → 存续期内 set/remove/list 读写 → 刷新（UpdateTeam 重建）/dispose 时随 unit 丢弃（无显式清理面——纯内存引用）。

### 2.2 脚本形态与执行结果

脚本 = 函数体源码，包装为**调用形态** `(function(board) { <source> })(board)`（`board` 由宿主以该条目 `state` 的 JSON 快照注入上下文；调用表达式内联于被编译源码，故 `runInContext` 的 timeout 覆盖函数体）：

```ts
/** 一次 hook 执行的终态（事件处理器据此决定播报内容）。 */
type HookRunResult =
  | { kind: "output"; text: string }            // return 字符串，或可 JSON.stringify 的对象（确定性转字符串）
  | { kind: "empty" }                            // return 空字符串 → 不播报（空输出仅指空字符串，spec FR-008）
  | { kind: "error"; message: string }           // 运行期抛错 / undefined（无 return）/ 输出不可序列化
  | { kind: "timeout" }                          // 超出 EXECUTION_TIMEOUT_MS 被中断
  | { kind: "truncated"; text: string };         // 输出超 MAX_OUTPUT_CHARS 截断
```

执行语义（research D1/D7）：注册期编译**调用形态** `(function(board) { <source> })(board)`；执行期每次 `vm.createContext({ board: <entry.state 的 JSON 快照> }, { codeGeneration: { strings: false, wasm: false } })`（上下文只有数据参数 `board` + fresh context 自带的 ECMAScript 内建，无宿主 API）+ `script.runInContext(context, { timeout: EXECUTION_TIMEOUT_MS })`——函数体在 `runInContext` 内执行，timeout 覆盖函数体；`board` 为快照，脚本改写不触达领域状态（spec FR-012 / US3 场景 6）；每次执行新 context（无跨执行状态）。

### 2.3 上限常量（research D4；`saolei-hooks/src/limits.ts`）

```ts
export const MAX_HOOKS = 8;              // 注册数量上限（超出 → set 文本拒绝）
export const MAX_SCRIPT_CHARS = 16384;   // 脚本源码长度上限（超出 → set 文本拒绝）
export const MAX_OUTPUT_CHARS = 4096;    // 播报输出上限（超出 → 截断 + 标识）
export const EXECUTION_TIMEOUT_MS = 500; // 逐 hook 执行预算（超出 → 中断播报）
```

### 2.4 banned-pattern（注册期 best-effort 校验，research D7）

源码含以下任一子串 → set 文本拒绝：`require(`、`import `、`process.`、`globalThis.`、`global.`、`fetch(`、`child_process`、`eval(`、`Function(`。

## 3. 能力面与装配

### 3.1 hooks unit（team 级工厂；research D8）

```ts
/** 插件 provide 面（plugin load 时 ctx.provide("saoleiHooks", ...)）。 */
interface SaoleiHooksMount {
  /** 物化早期（player 成员构造前）由编排调用：announce = 编排持有的 announcer 播报 seam
   *  （惰性闭包——调用时读 announcer，见 contracts/saolei-hooks-plugin.md §5）。 */
  buildHooksUnit(deps: { announce: (text: string) => void }): SaoleiHooksUnit;
}

interface SaoleiHooksUnit {
  /** planner setup 注册的工具面（单一 saolei_hook 工具，闭包持有同一注册表）。 */
  readonly tools: readonly ToolDefinition[];
  /** 恒为空数组（零 prompt section，spec FR-006）——与 SaoleiToolsUnit 同形，
   *  使编排得以按统一模式注册（tools → sections）。 */
  readonly sections: readonly PromptSection[];
  /** player 侧事件处理器（闭包持有同一注册表 + announce seam）。 */
  readonly onBoardChange: (entry: GameLogEntry) => void;
}
```

一个 unit = 一个 team 物化：registry/executor/handler 三面共享同一闭包状态；`sections` 恒为空数组（spec FR-006：零 prompt section）。

### 3.2 工具参数（schema 根字段全部 optional；XOR 在 execute 内校验，memory 同型）

```ts
interface SaoleiHookToolArgs {
  action?: "set" | "remove" | "list";
  name?: string;      // set/remove 必需（execute 内校验）
  script?: string;    // set 必需（execute 内校验）
}
```

工具输出恒为普通结果文本（`{result: string}` 形态）；失败路径（动作缺失/未知、非适用动作的多余字段、name/script 缺失、上限超限、banned-pattern、语法错误、remove 不存在）均为文本拒绝，无 error outcome（spec FR-003/004）。

### 3.3 事件处理器（onBoardChange）

```text
onBoardChange(entry):
  hooks = registry.entriesInOrder()
  for hook of hooks:                       // 注册序；前序失败不中断（spec US1 场景 7）
    result = execute(hook, entry.state)   // §2.2 语义：内部转 JSON 快照传入 vm，函数体受 timeout 约束；内部 try/catch
    // 播报文本按 contracts/hook-announcement.md §2 格式表分派（output/truncated/error/timeout）；
    // empty（空字符串）不播报——格式表的唯一权威来源是该契约，此处不重述。
    announce(文本按 §2 格式表渲染)
  （registry 为空 → 零执行零播报；任何内部异常 → warn 日志，不上抛、不影响 operate）
```

播报文本格式全集见 [contracts/hook-announcement.md](contracts/hook-announcement.md) §2。

## 4. 播报回流（复用 077 交付，零新机制）

hook 播报 = announcer 的插入式产出（发送方非激活成员），三面投影与恰好一次语义完全由既有机制承载：

- **单次生产**：unit 持有的 `announce` seam 即 `AnnouncerMember.announce(text)`（编排物化时绑定）。
- **web 展示链**：077 统一成员事件流 → 宿主单路投影（`team_message{member="announcer"}` 帧 + 归并条目）。
- **静止成员**：team relay → pending → 下次驱动装配 drain（`<announcer-message>` 广播）。
- **在途成员（player）**：077 在途广播快路（`deliverInFlight` 投递面）→ 不唤醒注入 next-step → 下一 step 边界认领 → 下一 step 模型请求携带；锚点闭包恰好一次。
- **顺序**：发言级 = 生产序（跨成员一致）；工具单元级有界偏差维持 077 裁定（披露不消除）。

时序图见 [spec.md](spec.md) 背景节。

## 5. 组合清单增量

`projects/game/agent_v2/cordis.yml` saolei-loop 行 `config.plugins` 追加：

```yaml
- name: '@dominion/dsh-saolei-hooks'
```

`projects/game/agent_v2/package.json` 依赖闭包追加 `@dominion/dsh-saolei-hooks`（workspace 协议）。saolei-loop `apply` 增加条件 fail-loud（[contracts/saolei-hooks-plugin.md](contracts/saolei-hooks-plugin.md) §5）。
