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

* `windows agent`：运行在游戏所在主机上，负责窗口绑定、截图/视频采集和操作执行。
* 控制面：使用 `grpc-go`，负责 `session` 的 `CRUD`、agent 管理、状态汇总和命令转发。
* web：`react` 页面，只作为控制台和验证界面，通过控制面查看截图/视频并发起调试操作。
* `session`：表示某个游戏进程（窗口）与控制面的关联，不是 `llm` 对话会话。
* 本阶段目标：打通 `windows agent -> 控制面 -> web` 链路，完成远程窗口的查看、基础操作和结果验证。
