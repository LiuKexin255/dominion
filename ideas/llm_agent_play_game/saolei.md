# LLM Agent 玩扫雷

## 目标

* 使用 LLM Agent 游玩扫雷，Agent 自主探索和策略迭代，prompt 无外部经验&策略输入。
* 游戏运行 `windows` 系统上，agent 部署在 `k8s` 集群。计划通过 `web + grpc` 服务从 `windows` 获取窗口视频流和操控游戏。然后再封装为 `MCP` 接入 Agent。
* 提供多种存储能力，让 agent 存储和更新游戏过程中总结的策略和经验。**特别的，存储能力不感知游戏内容**。
* Agent 方面设计：待定（我还没想好）

## 选型

* 控制面、以及各种能力服务：`grpc`(`go` 优先，其次 `node.js`)
* Agent 系统：`LangChain/LangGraph`，暂定通过 `Agent Server` 作为 API 接口。
* Web：`React` + `node.js`，使用 `bazel` 进行构建。

## 里程碑

### milestone 1: web + 控制面

* web：`react` 页面，控制面：`grpc-go`。前后端分离框架
* 资源模型：`session`（这个不是 `llm` 的会话，是这个控制面的概念）对应与某个游戏进程（窗口）的联系。web + 控制面实现 `session` 的 `CURD`。
* web 实现窗口的捕获 + 界面视频流到控制面服务，然后再从控制面获取视频的截图展示在 web 页面上（具体实现可以是一个按钮，按一下显示视频流当时的截图）
* 本阶段目标为可以通过 web 获取和操作运行在另一台主机上的窗口，视频流传给后台，后台发送操作指令再由前端执行。