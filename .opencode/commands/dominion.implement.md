---
description: Execute the SDD implementation plan by orchestrating per-phase development and review via developer and reviewer subagents
---

## User Input

```text
$ARGUMENTS
```

You **MUST** consider the user input before proceeding (if not empty).

## Role

按照 SDD 要求执行 tasks.md 计划，per-phase 任务分发与结果验证，确保代码变更按照计划执行。你将与 `developer` 和 `reviewer` 子代理（subagent）合作完成 task：`developer` 负责代码编写和开发，`reviewer` 负责代码审查。**禁止**与 `developer` 和 `reviewer` 做重复工作——不要自己写代码，不要自己 review。

## Pre-Execution Checks

1. Run `.specify/scripts/bash/check-prerequisites.sh --json --require-tasks --include-tasks` from repo root and parse FEATURE_DIR and AVAILABLE_DOCS list. All paths must be absolute. For single quotes in args like "I'm Groot", use escape syntax: e.g 'I'\''m Groot' (or double-quote if possible: "I'm Groot").

2. 完整阅读 spec 文档以建立执行上下文：
   - **REQUIRED**: Read tasks.md 获取完整的 phase/task 列表与执行计划
   - **REQUIRED**: Read plan.md 获取技术栈、架构与文件结构
   - **IF EXISTS**: Read data-model.md 获取实体与关系
   - **IF EXISTS**: Read contracts/ 获取 API 规约与测试要求
   - **IF EXISTS**: Read research.md 获取技术决策与约束
   - **IF EXISTS**: Read .specify/memory/constitution.md 获取治理约束

## Outline

对 tasks.md 中的**每一个 phase**，按以下步骤循环执行，直至所有 phase 完成：

### Step 1 — 分发 developer（per-phase 开发）

通过 Task 工具启动子代理 `developer` 执行该 phase。委派时必须：

- 明确说明负责的 phase（例如 `specs/[xxx-xxx]/tasks.md` 当中 Phase x）
- 要求 developer 在 coding 前按 SDD 规范阅读 spec 文档和其他要求的文档
- 补充你认为需要的其他上下文

委派示例：

```
你负责开发 `specs/[xxx-xxx]/tasks.md` 当中 Phase x。按照 SDD 规范阅读 spec 文档和其他要求的文档之后，再进行代码开发。完成后向我提供你的工作内容概要。

（其他你认为需要的补充）
```

### Step 2 — 回收并校验 developer 结果

回收 developer 的反馈后，进行检查：

1. **一致性校验**：developer 的反馈与代码仓库是否一致（例如 developer 声明修改的文件是否被更改、新增的文件是否存在）。
2. **符合性校验**：developer 的工作内容是否与 spec/plan/task 要求一致。

任一校验不通过，回到 Step 1 resume developer 子代理并反馈问题让其修改，重复至通过。

### Step 3 — 分发 reviewer（per-phase 代码审查）

通过 Task 工具启动子代理 `reviewer` 对该 phase 的代码变更进行审查。委派时**必须显式**说明：

- 参考文档列表（该 phase 的 Required Reading，同样**禁止删减/总结**）
- 需要 review 的代码文件列表

委派示例：

```
你负责 review `specs/[xxx-xxx]/tasks.md` 当中 Phase x 的代码变更。有以下参考文件：`style/api.md`, [AIP-126](URL), ...。请在阅读完参考文件后，对以下代码文件进行评审，并告诉我你的意见。

`xxx/xxx.go`
`xxx/xxx_test.go`
...
```

### Step 4 — 修复回路

对于 Step 2 或 Step 3 出现的任何问题，resume developer 子代理并反馈问题让其修改。修改后再次执行 Step 2 校验与 Step 3 review，重复该环节直到全部通过。

### Step 5 — 提交

为该 phase 的代码变更进行 git commit 提交。提交前确认仅包含本 phase 的预期变更。

### Step 6 — 推进

标记该 phase 完成，进入下一个 phase 重复 Step 1–Step 5，直到所有 phase 的所有 task 全部完成。

## Completion Report

所有 phase 完成后，向用户汇报最终状态与本命令完成的工作概要。

## Done When

- [ ] tasks.md 中所有 phase 的所有 task 已完成并标记 `[X]`
- [ ] 每个 phase 均经过 developer 开发 → executor 校验 → reviewer 审查 → 修复回路的闭环
- [ ] 每个 phase 的代码变更已分别 commit
- [ ] 全程未执行方案和计划之外的工作；遇到不符合方案的情况已中止并向用户反馈
