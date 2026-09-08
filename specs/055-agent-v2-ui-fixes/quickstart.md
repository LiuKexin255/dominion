# Quickstart: 055 agent-v2 界面可用性修复 + dsh-web 交互对齐 验证指南

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`

本文档是验证/运行指南（非实现细节）。交互契约见 `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md`，行为状态与变量映射见 `specs/055-agent-v2-ui-fixes/data-model.md`。

## 1. 前置

- bazel 仓库常规环境（`AGENTS.md`）。
- 人工验证场景需一套已部署环境（`projects/game/deploy.yaml` 拓扑）与一个真实/可控的 agent 会话；desktop 场景需 wails 桌面端连接同一网关。

## 2. 自动化验证（组件级）

```bash
# web 前端组件与样式测试（含新增跟随滚动/回底入口/对比度断言）
bazel test //projects/game/web/frontend:lib_test

# desktop 前端测试（含新增返回列表刷新断言）
bazel test //projects/game/desktop/frontend:lib_test

# 全量回归（既有 049/051/054 组件与 store 用例零回归）
bazel test //...
```

覆盖对照（FR-008）：

| 测试面 | 断言 |
| --- | --- |
| 条件跟随 | 贴底时内容增长保持贴底；非贴底时位置保持；回底后跟随恢复；发新消息回底 |
| 回底入口 | 非贴底渲染、贴底不渲染、点击回底并恢复跟随（`data-testid="to-bottom-button"`） |
| ReasoningRow 对齐 | `data-expanded` 存在性、折叠态围栏样式规则、follow-end 纯 CSS 结构、无程序化滚动 |
| 状态横幅对比度 | `theme.css?raw` + token 表解析 → WCAG 对比度 ≥ 4.5:1（四类横幅规则） |
| desktop 返回刷新 | Back → `listSessions` 再次调用；失败呈现错误且不清空列表 |

## 3. 人工验证（部署环境记录，spec Clarifications 2026-09-05 Q2=A）

按序执行并留存记录（截图/说明即可作为 FR-001 布局验收证据）：

1. **窗口无滚动（FR-001）**：发起一轮包含长思考的对话；思考流式输出全程（折叠态 + 展开态各验证一次）观察浏览器窗口：不出现窗口级滚动条，页面整体不可滚动；正文流式输出同样复查。
2. **跟随不劫持（FR-002~004）**：输出进行中向上滚动到历史消息——阅读位置保持，不被拉回；此时右下出现"回到底部"按钮，点击回底后跟随恢复；直接发送新消息时视角回底。
3. **横幅可读（FR-005/006）**：制造一次回合失败（如断开模型端点）——错误横幅文字清晰可读；执行一次终止——"已终止"标识可读且与错误横幅色系可区分；preset/设置面板错误提示同查。
4. **desktop 刷新（FR-007）**：desktop 进入某 session → 在 web 侧新建/删除会话 → desktop 返回列表 → 列表反映最新集合；刷新失败场景（如断网）返回列表 → 错误呈现且旧列表不清空。
5. **对齐复查（FR-009/SC-005）**：对照 `contracts/ui-interactions.md` §5 偏离清单逐项核对代码行为与清单声明一致；清单外无未声明偏差。

## 4. 预期结果

- 全部自动化测试通过（含既有用例零回归）。
- 上述人工场景全部符合预期并留存记录。
- 054 既有 testplan suites 作为回归面保持通过（本 feature 不改服务行为，无新增大型测试面）。
