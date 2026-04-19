---
name: test-plan
description: Execute large test plans in this repository by parsing a testplan markdown file, deploying the SUT, running Bazel cases, and cleaning up the environment.
compatibility: opencode
metadata:
  audience: dominion
  scope: large-test
---

# test-plan

使用这个 skill 执行仓库里的大型测试计划。

## 何时使用

- 你刚完成了 `grpc` 或 `http` 服务相关实现，需要按测试计划执行大型测试。
- 用户/开发计划要求“执行 testplan / 测试计划”。
- 你发现仓库规范要求先写 `testplan/*.md`，再按计划部署 SUT 并运行测试。

不要在下面场景使用：

- 只是运行普通单元测试：直接使用 `bazel test`。
- 还没有测试计划文件：先帮助用户补齐 `testplan` 文档。

## 必须先读的仓库约定

在执行前先读取：

1. `styles/large_test.md`
2. 对应服务目录下的 `README.md`（如果存在）
3. 测试计划文件本身

仓库约定要点：

- 大型测试计划通常放在 `testplan` 目录。
- 计划至少包含：`name`、`deploy`、`cases`。
- `deploy` 指向部署配置，使用 `deploy(//tools/deploy)` 工具链执行部署。
- `cases` 中的每个条目通常都是一个 `bazel test` 目标。
- 常用环境变量：`SUT_HOST_URL`。

## 测试计划格式

参考 `styles/large_test.md`：

```markdown
# 测试计划样例

* name：deploy 服务接口测试
* deploy：//projects/infra/deploy/test_deploy.yaml

## Test cases
* //projects/infra/deploy:integration_test
```

兼容中英文冒号（`:` / `：`）。如果计划内容和样例略有偏差，优先按语义解析，不要因为格式轻微不同就停止。

## 执行流程

### 1. 定位并解析测试计划

- 找到用户指定的 `testplan/*.md` 文件。
- 提取：
  - `name`
  - `deploy`
  - `cases`（保留顺序）
- 如果缺少 `deploy` 或没有任何 `cases`，停止执行并明确指出测试计划缺失项。

### 2. 部署被测系统（SUT）

- 在 Bazel workspace 根目录执行。
- 优先使用仓库 `tools/deploy/README.md` 中记录的命令约定。
- 先确保 `deploy` 工具可用；如未安装，优先使用 Bazel 提供的安装方式：

```bash
bazel run //:deploy_install
```

- 使用测试计划中的 `deploy` 路径部署：

```bash
deploy apply <deploy-path>
```

- 如果用户或环境已经提供 `KUBECONFIG`，沿用它；不要擅自改写 kubeconfig。

### 3. 采集测试运行所需上下文

- 阅读对应服务 README、测试代码或测试计划说明，确认如何得到 `SUT_HOST_URL`。
- 如果部署后能够从仓库文档、固定域名、网关路径或测试代码中可靠推导出 `SUT_HOST_URL`，就直接设置并继续。
- 如果无法可靠推导，不要编造地址；明确说明缺少哪一个信息。

### 4. 执行测试用例

- 对 `cases` 中每个 Bazel target 逐个执行。
- 如果同一计划包含多个 case，除非计划明确要求并行，否则按顺序执行，便于定位失败点。
- 如果遇到 `503`、`no available server`、`connection refused` 等未就绪信号，允许在**有限等待**后重跑一次对应 case；若仍失败，再按真实失败上报，最多 3 次，不要无限重试。
- 某个 case 失败时，记录失败目标与关键信息。如果是被测系统有问题，清理 SUT，尝试 fix 后再次进行测试；如果是测试代码，可以直接 fix 后再次运行（无需重新部署）。如果尝试 fix **3** 次仍无法修复，中止测试，整理失败信息返回。

### 5. 始终清理环境

- 无论测试成功还是失败，都必须清理部署环境。
- 从 `deploy` 配置中读取环境名；按 `tools/deploy/README.md` 约定执行：

```bash
deploy del <env-name>
```

- 如果删除失败，明确报告“测试结果”和“清理结果”是两个独立状态，不要把清理失败误报成测试失败。

## 输出要求

向用户汇报时，按下面结构输出：

1. 使用的测试计划路径
2. 解析出的 `deploy` 和 `cases`
3. 部署结果
4. 每个测试 case 的结果
5. 清理结果
6. 如果失败，指出失败发生在：计划解析 / 部署 / 测试执行 / 清理 中的哪一阶段

## 强约束

- 不要跳过清理。
- 不要伪造 `SUT_HOST_URL`。
- 不要把大型测试降级为“只跑单测”然后宣称完成。
- 不要修改测试计划文件，除非用户明确要求你修它。
- 不要把部署后短暂未就绪直接报成最终失败；先按仓库上下文做有限等待/重试。
- 不要无限等待或无限重试；等待策略必须有明确上限。
- 不要提交代码。

## 仓库内可直接参考的文件

- `styles/large_test.md`
- `tools/deploy/README.md`
- `projects/infra/deploy/testplan/interface_test.md`