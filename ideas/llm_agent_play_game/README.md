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