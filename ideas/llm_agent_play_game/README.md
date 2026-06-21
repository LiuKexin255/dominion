# Agent 玩游戏

计划设计一个 agent 玩游戏并自己总结、更新策略的系统。Agent 使用 langchain deepagent + ws 作为有状态服务，搭配一个无状态的 proxy 进行路由。除此以外还有一个 session 服务作为控制面，管理会话信息，以及一个 gateway 服务作为整体对外的 http 网关

## 资源路径

1. session 会话：/api/v1/sessions/{session_id} -> session 服务
2. agent 实例：/api/v1/sessions/{session_id}/agent -> agent 服务
3. agent ws 连接：/api/v1/sessions/{session_id}/agent/connect -> ws 连接用于 agent input/output

## Milestone

### step.1 

1. 实现 4 个服务的框架，每个资源仅包含本阶段必要的字段。本阶段目标为完成 4 个服务的可部署，以及请求的联通，重点是 agent 服务有状态连接的路由转发。
2. agent 资源的 Create 操作为无状态请求，路由到的节点将自己作为 agent owner 进行初始化。其他请求则由根据 owner 信息进行路由
3. proxy 和 session 同样使用 mongo 数据库持久化数据，使用同一个实例但不同的数据库。本次实现要包含 mongo 数据库读写，但 deploy 配置中 mongo 可以不进行持久化。
4. 除了 ws 请求外，其他请求均使用 grpc。gateway 使用 grpc-gateway 进行 grpc <-> http。
5. 每个服务要包含接口测试，整个系统要包括系统测试，使用 testplan 进行编排
6. 系统目录为 /projects/game，域名为 game.liukexin.com

### step.2 

1. desktop 将操作 UI 替换为正式的 session/agent 操作页面，以及一个新的测试功能：desktop 可以绑定一个窗口，以及截图并将截图传递给 agent，agent 可以正确接收图片。
2. desktop 增加 session 列表和针对 session 的操作页面。选定 session 后进入 play 状态，后续会开始与 agent 连接以进行游戏，但本阶段仅为绑定窗口然后可以向 agent 传递截图。
3. 要显式确定图片的编码格式，以及图片尺寸要和桌面上尺寸一致，后续 agent 通过像素反馈操作命令（例如在 (x,y) 点击鼠标左键），尺寸不一致会导致操作不准确。
4. agent ws payload 在 proto 内显式定义，不再使用 bytes，避免出现“潜规则”和漂移。
5. 所有请求的通信均使用 proto 中定义的模型，各个服务或desktop不再自己定义请求类型。
6. 其他的优化：
    1. log 可以正确换行，现在log 是类似表格，各个部分单独换行
    2. 服务请求增加 traceid，方便追踪。（仅设置 traceid，不上报 desktop 的日志）
    3. 创建 session 时 sessionid 由服务生成，不再由请求参数中指定。

### step.3

目标：为 agent 服务增加对话 agent 实现，使用 [langchain deepagent](https://docs.langchain.com/oss/javascript/deepagents/overview) 框架。

1. 将 agent 改为 grpc-js 服务，使用 deepagent 实现一个对话 agent。创建 Agent 模板由 prompt 服务定义，创建 agent 实例时传入 agent_profile 名称，agent 服务读取 agent 描述文件创建 agent。
2. agent 将历史对话放入 deepagent contextPrompt 上下文中。本版本不使用 tools、mcp 和 skill。
3. agent 实例超过 15 分钟无活动（未收到消息或者未在使用中）则清理实例。
4. session-agent 的架构模型不变，只是本阶段 agent 只实现一个最简单的对话 agent，后续逐渐完善使其可以玩游戏。
5. desktop 本次目标包括两大模块，原有的 play 模式和截图功能，改为连接 agent 后进入一个对话框 + 侧边栏显示 agent 信息的 UI。对话框展示用户的输入，agent 的 think 和输出（都是文字）。
6. 除此以外，desktop 增加一个 prompt 管理模块，目前包括对于 agent profile 的增删改查。
7. agent provide 本次只有一个 [opencode-go](（https://opencode.ai/docs/zh-cn/go/）) （注意是 go 不是 zen）。provide secret 通过 deploy 工具配置 secret 文件挂载，后续其他的 provide 的密钥也通过配置 secret 文件提供。

> 已经将 deepagent 更换为 [langchain](https://docs.langchain.com/oss/javascript/langchain/overview)

### step.4

目标：为 agent 服务增加鼠标操作的 tools 和收发图片功能，使 agent 可以在用户操作下完成游戏。

1. 为 agent 新增鼠标tools 支持，并新增鼠标 tool，参数：x，y（截图相对坐标，由 desktop 负责转换绝对坐标）、action（左、右键单击/双击，左右同时按下）。tool 负责接收 llm 的操作指令并转换成 Frame 传递给 desktop，由 desktop 完成操作后再将结果传回 llm。
2. agent 增加接收和向 llm 发送图片的功能，并且支持图片和文字一起打包发送（对于 desktop 操作来说）。
3. desktop 在 play 页面支持绑定窗口功能，绑定后可以使用“截图”按钮将窗口截图，并且将截图作为附件添加到输入框（可删除）。可随消息（可选）一起发送。
4. prompt 在 agent profile 中增加 tools 列表（只含 tools 名称，声明使用的工具，工具实现是在 agent 上面）
5. desktop 增加对于 tools 和 图片消息的展示，图片消息正常为折叠，点击可展开；鼠标工具则展示操作信息。
6. desktop 增加对于鼠标操作 Frame 处理与回报。
7. desktop 对话框文本消息展示支持 markdown 格式，可正确处理 markdown 内容。
8. 当前阶段不追求完全 agent 自动化，agent 每次执行单步，可由用户进行推进。