# Agent 玩游戏

计划设计一个 agent 玩游戏并自己总结、更新策略的系统。Agent 使用 langchain deepagent + ws 作为有状态服务，搭配一个无状态的 proxy 进行路由。除此以外还有一个 session 服务作为控制面，管理会话信息，以及一个 gateway 服务作为整体对外的 http 网关

## 资源路径

1. session 会话：/api/v1/sessions/{session_id} -> session 服务
2. agent 实例：/api/v1/sessions/{session_id}/agent -> agent 服务
3. agent ws 连接：/api/v1/sessions/{session_id}/agent/connect -> ws 连接用于 agent input/output

## Milestone

### step 1 

1. 实现 4 个服务的框架，每个资源仅包含本阶段必要的字段。本阶段目标为完成 4 个服务的可部署，以及请求的联通，重点是 agent 服务有状态连接的路由转发。
2. agent 资源的 Create 操作为无状态请求，路由到的节点将自己作为 agent owner 进行初始化。其他请求则由根据 owner 信息进行路由
3. proxy 和 session 同样使用 mongo 数据库持久化数据，使用同一个实例但不同的数据库。本次实现要包含 mongo 数据库读写，但 deploy 配置中 mongo 可以不进行持久化。
4. 除了 ws 请求外，其他请求均使用 grpc。gateway 使用 grpc-gateway 进行 grpc <-> http。
5. 每个服务要包含接口测试，整个系统要包括系统测试，使用 testplan 进行编排
6. 系统目录为 /projects/game，域名为 game.liukexin.com

### step2 

1. desktop 将操作 UI 替换为正式的 session/agent 操作页面，以及一个新的测试功能：desktop 可以绑定一个窗口，以及截图并将截图传递给 agent，agent 可以正确接收图片。
2. desktop 增加 session 列表和针对 session 的操作页面。选定 session 后进入 play 状态，后续会开始与 agent 连接以进行游戏，但本阶段仅为绑定窗口然后可以向 agent 传递截图。
3. 要显式确定图片的编码格式，以及图片尺寸要和桌面上尺寸一致，后续 agent 通过像素反馈操作命令（例如在 (x,y) 点击鼠标左键），尺寸不一致会导致操作不准确。
4. agent ws payload 在 proto 内显式定义，不再使用 bytes，避免出现“潜规则”和漂移。
5. 所有请求的通信均使用 proto 中定义的模型，各个服务或desktop不再自己定义请求类型。
6. 其他的优化：
    1. log 可以正确换行，现在log 是类似表格，各个部分单独换行
    2. 服务请求增加 traceid，方便追踪。（仅设置 traceid，不上报 desktop 的日志）
    3. 创建 session 时 sessionid 由服务生成，不再由请求参数中指定。

### step3

1. 为 agent 服务增加 [langchain deepagent](https://docs.langchain.com/oss/javascript/deepagents/overview) 实现，支持 agent 返回操作指令到 desktop 游玩游戏。（agent 修改为 ts 服务，使用 grpc-js）
2. agent 为最简实现，仅包括一个 agent，不包括 subagent 和 long-term 记忆。agent 支持 mcp、tools 和 skills。
3. 新增 `prompt` 服务用来管理 agent 提示词、系统提示词和工具无关的 SKILLS；而 agent 服务包括 mcp、tools 和配套的 SKILLS（也就是与 runtime 有关的归属 agent，并通过 build-in 方式编译到服务内，而其他的则保存在 prompt 服务，动态加载）。prompt 服务使用 mongo 存储 profile 和 SKILLS。
4. agent 的操作通过 mcp 输出到 agent 服务，agent 服务再将指令传输给 desktop 完成操作。desktop 完成操作后，进行截图并发给 agent 服务，agent 服务将截图作为 input 输入给 agent 作为操作结果。该版本仅支持手动操作，即每次发送截图需要用户在 desktop ui 上操作。并且 agent 输出的操作指令和 desktop 发送的截图都带有序号，两边都只接受递增的操作。例如 agent 服务发出的操作序号为 m，接受到序号为 m-n 的截图则返回忽略的 warn。
5. agent 支持的操作包括鼠标在指定位置左右键单击、双击，左右双击，以及按键操作，每次一个动作。agent 输出坐标使用截图相对像素坐标，desktop 负责转换为本地窗口或屏幕坐标。
6. prompt 服务包括的资源
    1. agent profile: /api/v1/prompt/{agent_profile_name} -> agent 描述文件，包括使用的模型、系统提示词、SKILLS，以及 MCP（仅包括 mcp 名称，mcp 和 tools 实现和提示词在 agent 服务内置）。游戏玩法相关提示词通过 SKILL 输入，profile 不保存独立的 agent_prompt。
    2. skills : /api/v1/prompt/{skills_name} -> skills 文件。
7. agent 创建新增参数 {agent_profile_name}，然后根据 agent_profile 下载/更新 SKILLS、检查 mcp 合法，然后创建 agent 实例。agent 实例单次 invoke 有默认超时时间 10分钟。如果超过 30 分钟未激活（没有新的 input也没有在 invoking）则执行 delete 操作。DeleteAgent 删除空 agent 不返回错误，避免不必要的噪音。
8. desktop 重构 UI 以支持新的需求，主要包括 prompt 管理，以及 play 页面改为对话风格页面 + 侧边框展示基本信息。play 页面 desktop（role user）仅发送图片，ui 上默认折叠，点击切换展示，agent 展示 thinking 内容、文本输出以及操作内容。

### step3.a

step3.a 是 step3 的第一阶段，目标是在不接入真实 deepagent 推理的前提下，先稳定 prompt、profile、操作协议和手动游玩闭环。

1. 完成 agent 与 desktop 之间的手动游玩协议，包括截图输入、agent 文本/思考输出、agent 操作指令、desktop 操作结果回传，以及对应的递增序号约束。
2. agent 操作坐标统一使用截图相对像素坐标，desktop 负责转换为本地窗口或屏幕坐标并执行操作。
3. 新增 `prompt` 服务，使用 mongo 管理 agent profile（含系统提示词）和工具无关的 SKILLS。游戏玩法相关提示词通过 SKILL 输入，不在 profile 中单独保存 agent_prompt。
4. agent 创建新增 `{agent_profile_name}` 参数，并根据 profile 加载 SKILLS、检查 MCP 名称合法性后创建 agent 实例。
5. 本阶段 agent runtime 可以使用可预测的最小实现完成协议闭环，不要求接入真实 deepagent 推理。
6. 本阶段验收优先使用 testplan，覆盖 prompt 服务、profile 创建 agent、截图到操作指令、操作结果回传、序号校验和删除幂等；真实窗口操作保留 Windows 手动验收。

### step3.b

step3.b 是 step3 的第二阶段，目标是在 step3.a 的协议和 prompt 能力之上，将 agent runtime 切换为 TypeScript grpc-js 服务并接入最小 deepagent 能力。

1. 将 agent 服务切换为 TypeScript 服务，使用 grpc-js 实现现有 AgentService 协议，并保持 gateway、proxy、session 的对外链路不变。
2. 为 agent 服务引入 [langchain deepagent](https://docs.langchain.com/oss/javascript/deepagents/overview) 生态能力，但仅启用本阶段需要的最小单 agent 能力；不包含 subagent 和 long-term 记忆。
3. agent 支持 MCP、tools 和 runtime 相关 SKILLS；MCP、tools 及其运行时提示词由 agent 服务内置，工具无关 SKILLS 继续由 prompt 服务动态加载。
4. agent 单次 invoke 默认超时时间为 10 分钟；超过 30 分钟未激活且没有正在执行的 invoke 或待处理操作时自动删除 agent。DeleteAgent 删除空 agent 不返回错误。
5. desktop 重构 UI 以支持 prompt 管理和对话式 play 页面。play 页面中 desktop 仅发送图片，默认折叠展示；agent 展示 thinking 内容、文本输出以及操作内容。
6. 本阶段验收优先使用 testplan 覆盖 TS agent 的 gRPC/WS 链路、deepagent 最小推理闭环、profile 加载、工具无关 SKILLS 加载、超时和 idle 删除；真实窗口操作保留 Windows 手动验收。
