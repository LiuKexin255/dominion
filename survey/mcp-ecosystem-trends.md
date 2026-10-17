# 调研：MCP 生态趋势与社区观点（2024.11–2026.09）

> **状态**：背景调研完成，无待定项。本调研为方向性输入，其结论（MCP 在本仓库拓扑下不引入）被
> `survey/dsh-grpc-runtime-client.md` 消费；亦可独立用于后续 agent 能力面设计的背景依据。
> **日期**：2026-09-30（同日增补问题二及 §5）
> **范围**：Model Context Protocol 的社区观点与生态趋势调研——验证两个问题：①"MCP 逐渐从『一级
> 公民』淡化为一种接口协议，被整合进更通用的工具（控制台 stdio/CLI、JS/Python 代码执行工具、
> 插件）"这一观察（§1–§4）；②"MCP 能否经前置查询代码转换为 tool、仅向智能体传递相关内容"——
> 即渐进发现（progressive discovery）/元工具模式的现状（§5）。
> **不含**：本仓库的方案设计（见 `survey/dsh-grpc-runtime-client.md`）。

---

## 1. 调研问题与总结论

调研问题一：MCP 当前在社区中的定位如何？"从一级公民退位为接口协议之一、被更通用的原语吸收"的
观察是否成立？（§2–§4）

调研问题二：既然 MCP 工具可以塞进 JS 脚本（Code Mode），是否就有方法把 MCP 转换为 tool——由
代码处理前置查询（目录发现/过滤），仅把与任务相关的工具内容传递给智能体？（§5）

**总结论**：观察大方向正确，但需精确化——

1. ✅ 在**编码/终端 Agent** 领域，MCP 已从 2025 年中期的"默认扩展机制、一级公民"退位：工具调用
   入口正被 shell/CLI、代码执行工具（Code Mode/PTC）、Tool Search（渐进披露）、插件体系接管，
   MCP 被包裹在这些原语之下成为"传输层/实现细节"。
2. ✅ tool 与 MCP 的边界在官方层面主动模糊化：Anthropic 自身发布 Skills、Claude Code 把 commands
   并入 skills、MCP 官方文档建议"用 Skills 构建 MCP server"；Cloudflare 直接定性"MCP tools 就是
   带文档的 RPC 接口"。
3. ⚠️ 需修正：这是"**基础设施化**"而非"死亡"。MCP 绝对使用量仍在增长（SDK 月下载 ~9700 万、
   注册 server 17000+、2025.12 捐赠 Linux Foundation/AAIF、2026.07.28 stateless 2.0 规范），在
   **无终端场景**（移动端、Electron 沙箱、桌面 Chat、企业多租户、审计合规）地位上升而非下降。
   社区主流结论："MCP 没死，它被分类了"——不需要它的场景在离开，需要它的场景在静默落地。
4. ✅ 问题二的推断成立且已官方化/工程化：即"**渐进发现（progressive discovery）/元工具模式**"。
   MCP 2026-07-28 Client Best Practices 已正式文档化（`search_tools` 元工具 + Catalog/Inspect/
   Execute 三层 + 1%–5% 切换阈值），六种转换形态在网关/server/客户端/CLI 各层落地，实测降耗
   85%–98.7% 且工具选择准确率同向提升（Opus 4 选择准确率 49%→74%）；最重要的工程坑是 prompt
   cache 失效。详见 §5。

## 2. 舆论时间线

| 时间 | 事件 | 意义 |
|---|---|---|
| 2024.11 | Anthropic 发布 MCP（"AI 的 USB-C"） | 起点 |
| 2025.03–06 | OpenAI/Google 宣布支持；Cursor/Cline 全面接入 | "一级公民"顶峰：人人 ship MCP server |
| 2025.09 | Cloudflare 发布 Code Mode（https://blog.cloudflare.com/code-mode/）："LLM 写代码调 MCP 比直接调 MCP 更好" | 首次系统性把 MCP 降级为代码 API 之后的传输线 |
| 2025.10 | Anthropic 发布 Agent Skills | 同一公司造了第二个标准 |
| 2025.11 | Anthropic 工程博客 code-execution-with-MCP（150k→2k token，98.7% 降幅，https://www.anthropic.com/engineering/code-execution-with-mcp）；Thoughtworks Technology Radar Vol.33 把"naive API-to-MCP conversion"列入 Hold 环 | 官方自己承认直接调用模式有问题 |
| 2025.12 | MCP 捐赠 Linux Foundation（AAIF；OpenAI/Google/Microsoft/AWS/Cloudflare 创始成员）；Skills 成为开放标准 | 从公司赌注变行业公共设施 |
| 2026.02.28 | Eric Holmes《MCP is dead. Long live the CLI》登顶 HN（400+ 分 ~300 评论，https://ejholmes.github.io/2026/02/28/mcp-is-dead-long-live-the-cli） | "CLI 复兴" discourse 爆发 |
| 2026.03 | Garry Tan "MCP sucks honestly" + 开源 gstack；Perplexity CTO 宣布内部弃用 MCP 转向 API/CLI（对外仍维护官方 MCP server）；OpenClaw（200k+ stars）核心不带 MCP runtime，经外部桥 mcporter 转 CLI 使用 | 舆论低谷（Gartner 幻灭期） |
| 2026.07.28 | MCP stateless 2.0：删除 initialize 握手与 Mcp-Session-Id，协议核心变为无状态请求/响应；GitHub MCP Server 借此删掉 Redis 会话 | 协议向"普通 HTTP API"收敛，实现税大降，部分开发者回流 |
| 2026.08–09 | MCP roadmap 被评"这就是一份 API roadmap"（HTTP over stdio 统一传输、ETag、OAuth/IETF token exchange，https://apievangelist.com/2026/08/24/the-mcp-roadmap-is-an-api-roadmap/） | 彻底基础设施化 |

## 3. 社区三阵营

### 3.1 CLI 原教旨派（"MCP 已死"派）

- Eric Holmes：MCP server 初始化脆弱、反复 re-auth、权限一刀切；CLI 可组合、可调试、模型训练
  数据已熟。"Ship a good API and a good CLI. The agents will figure it out."
- Peter Steinberger（OpenClaw）："I don't use MCPs or any of that crap"；给 agent 一个 shell +
  几百个 CLI + SKILL.md，"agent 第一次调用失败，读 help，15 秒后就会用"。
- Simon Willison（2025.11–2026.02 立场）："My own interest in MCPs has waned... Almost
  everything I might achieve with an MCP can be handled by a CLI tool instead."
- 数据支撑（注意 §7 可信度备注）：ScaleKit 基准 75 次对比最简任务 MCP 44,026 tokens vs CLI 1,365
  （32×）、成功率 72% vs 100%；Apideck 测得 GitHub+Slack+Sentry 三 server ~40 工具吃掉 ~55k
  tokens；Microsoft 发 Playwright CLI 取代 Playwright MCP（~27k vs ~114k tokens）。

### 3.2 传输层务实派（与本仓库结论最相关，含 Anthropic/Cloudflare 自己）

- Cloudflare Code Mode（2025.09 → 2026.02 "整个 API 只要 1000 token"）：把 MCP schema 转成
  typed SDK，模型只见一个 `code` 工具或 `search()`+`execute()` 两工具。原话："The 'tools' that
  an MCP server exposes are really just an RPC interface with attached documentation."
- Anthropic 官方（code-execution-with-MCP）：MCP server 呈现为文件系统上的代码 API，按需读取。
  即 PTC（Programmatic Tool Calling），Claude SDK、Goose 均已实现。
- 学术（CE-MCP 论文，https://arxiv.org/html/2602.15945）：代码执行模式在结构化/多工具任务上
  token/时延/轮次全面占优，但迭代式文本密集任务传统模式仍占优，且代码执行引入新攻击面——
  **两种模式共存而非替代**。
- 桥接工具生态：`mcporter`（MCP→CLI）、`clihub`（MCP server 编译成独立 CLI 二进制）、`mcp2cli`。
- 架构观点："MCP is a transport, not an architecture / not a control plane"——MCP 解决互操作，
  治理/编排/决策住在别的层。

### 3.3 企业/无终端坚守派

- Allen Hutchison（https://allen.hutchison.org/2026/03/14/mcp-isnt-dead-you-just-arent-the-target-audience/）：
  CLI 只服务"有人类权限 shell"的拓扑；Electron 沙箱、iOS/Android、无头编排器、多租户 SaaS
  没有 `$PATH` 可言。
- Simon Willison 回摆（2026.07–09，重要新信号）：
  - https://simonwillison.net/2026/jul/31/stateless-mcp/ —— stateless 规范让他一周手搓三个
    MCP 工具；**安全论证反转**："给 agent 一个带互联网访问的 shell 风险重重；MCP 工具更容易
    审计和控制，小模型也能可靠驱动。"
  - https://simonwillison.net/2026/sep/20/hn-49779718/ —— "跑全功能终端 agent 确实几乎不需要
    MCP。但只要想做得没那么 YOLO——控制可访问服务、agent 不直接持有 key、强审计——MCP 让这
    一切容易得多。"
- 采用量：SDK 月下载 9700 万+、server 17000+；Perplexity 弃用的只是内部调用机制。

## 4. 三个观察的验证

1. **"一级公民被淡化"**：✅ 编码 agent 领域成立。最有说服力的是 Anthropic 自己的行为——创造
   MCP、营销为通用标准，然后造 Claude Code（把操作系统当集成层）、再造 Skills 和 code-execution
   模式绕过直接调用。同一家公司为自己的协议做了两个"补丁标准"。Zuplo 调研
   （https://zuplo.com/blog/mcp-survey）：58% 的 MCP 构建者只是包装已有 API。
2. **"被整合进更通用的工具（stdio/CLI、JS/Python 执行工具）"**：✅ 当前最活跃的工程方向。
   代码执行工具（Cloudflare Code Mode 含 `codeMcpServer()` 把任意 MCP server 包成单 `code`
   工具）、CLI 桥（mcporter/clihub/mcp2cli/gstack）、Tool Search（Claude Code 默认开启，
   "adding more MCP servers has minimal impact on your context window"——MCP 工具与本地工具
   在成本模型上被拉平）、插件（Claude Code plugin = skills+commands+agents+hooks+MCP+LSP+
   monitors+`bin/`——`bin/` 往 Bash PATH 放可执行文件，MCP 与 shell 在同一插件清单里平起平坐）。
   该方向的机制细节与六种实现形态见 §5。
3. **"tool 与 MCP 边界模糊"**：✅ 官方主动推动。MCP 仓库 Discussion #1779 提议废弃 prompts 原语
   换 agent 可发现的 "skills" 模式；MCP 官方文档建议用 Skills 构建 MCP server；Claude Code 把
   commands 并入 skills；2026.07.28 规范裁剪原语（Tasks/Apps/Extensions、12 个月移除
   Roots/Sampling/Logging 倒计时）向 HTTP/API 收敛。

## 5. MCP → tool 的转换层：渐进发现 / 元工具模式

> 增补日期：2026-09-30。调研问题二：既然 MCP 工具可以塞进 JS 脚本（Code Mode），是否就有方法
> 把 MCP 转换为 tool——由代码处理前置查询（发现/过滤），只把与任务相关的工具内容传给智能体？
> 结论：成立，且已从"理论上的方法"变为官方推荐模式 + 一整层工程生态。

### 5.1 类比精化：MCP ≈ protobuf + gRPC，成本在语义契约而非传输

MCP 不只对应 protobuf（序列化），它同时覆盖 IDL（`tools/list` 返回的 schema+description）、
服务方法（`tools/call` 等 RPC）、能力协商与认证约定——整体更接近 gRPC 的类比。但社区实测证明
**真正的成本从来不在传输格式**（JSON-RPC 字节开销可忽略），而在语义契约必须进入模型上下文：

- 生产级 server 的工具定义平均 ~468 token/工具，相对"模型选择+调用所需的最小信息"存在稳定的
  **6.6× 冗余**（可复现基准：MCP Discussion #3176，
  https://github.com/modelcontextprotocol/modelcontextprotocol/discussions/3176）；
- 72 工具 ≈ 33.7k token ≈ 200k 窗口的 17%，每轮支付，无论是否使用。

因此解法方向是：**把工具目录从"静态注入 prompt/function schema 的配置"变成"按需检索的内容"**
（目录即数据、schema 即内容）。

### 5.2 官方标准架构：三层渐进发现

MCP 2026-07-28 Client Best Practices 已将该模式正式文档化
（https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices）：

- 宿主仍调 `tools/list` 全量拉取，但**延迟注入**模型上下文；模型只看到一个轻量 `search_tools`
  元工具；完整定义按需加载。
- **三层模式**：Layer 1 Catalog（`search_tools(query)` 返回工具名+短描述）→ Layer 2 Inspect
  （按需取单个工具的完整 schema/文档）→ Layer 3 Execute。
- **切换阈值**：工具定义占上下文窗口 1%–5% 以下时全量加载即可，超过才切渐进发现（不无脑上
  元工具）。
- **检索策略选型**：BM25 关键词（简单有效）/ embedding 向量（语义同义）/ 小模型子代理（效果
  最好但更贵）/ 混合。OpenAI 与 Anthropic API 已原生支持 tool search；需要定制检索（领域排序、
  权限过滤）时才自建。
- **服务器级渐进发现**：整个 MCP server 也可懒连接——维护 server 注册表，模型需要时才连接、
  用完断开释放上下文；skill 文件可声明其依赖的 MCP server，宿主仅在该 skill 被触发时连接
  （skill↔MCP 在协议层的官方缝合点）。

### 5.3 六种转换形态（按前置查询代码所在层分类）

| # | 形态 | 前置代码位置 | 模型看到什么 | 代表实现与数据 |
|---|---|---|---|---|
| 1 | N 工具 → 2 元工具（`get_tool`+`invoke_tool`） | 网关 | 轻量索引 + 两个元工具 | Solo.io agentgateway（`toolMode: Search`，91% 降耗，https://www.solo.io/blog/mcp-progressive-disclosure）、MCP360（`search_tools`/`execute_tool`）、Amazon Bedrock AgentCore Gateway |
| 2 | N 工具 → 1 个 `code` 工具 | server（沙箱内） | 单工具，描述内带生成的 TS 类型 | Cloudflare `codeMcpServer()`（https://developers.cloudflare.com/agents/model-context-protocol/guides/build-codemode-mcp-server/） |
| 3 | 整个 API → `search()`+`execute()` | server（沙箱内） | 两个工具、~1000 token 固定开销 | Cloudflare API MCP（数千 endpoint 全覆盖、token 足迹恒定，https://blog.cloudflare.com/code-mode-mcp/） |
| 4 | N 工具 → typed JS API | 客户端沙箱（模型写的代码内） | 一个 `code` 工具；代码内调 `codemode.search()`/`describe()` | Cloudflare Code Mode / Anthropic PTC（150k→2k，98.7%，https://www.anthropic.com/engineering/code-execution-with-mcp） |
| 5 | N 工具 → CLI 子命令 | 无（`--help` 即接口） | shell 里的普通命令 | `mcporter`、`clihub`（MCP 编译成独立二进制）、`mcp2cli`（96–99% 降耗） |
| 6 | N 工具 → 1 个自然语言端点 | server（自建 agent） | `agentic_search_content(question)` 单工具单参数 | AWS tool design 博客 V6 "agent-as-tool"（https://aws.amazon.com/blogs/machine-learning/mcp-tool-design-practical-approaches-and-tradeoffs/） |

另有无侵入的即插即用层：`mcp-lazy-proxy`（stdio 透明代理，把每个工具定义压成 ~54 token 存根、
实际调用时才展开完整 schema，实测 6.4–6.7× 降耗）——字面意义的"中间加一段代码做前置查询"。

**量级汇总**：官方文档示意全量 ~150k vs 渐进 ~2k；Anthropic Tool Search 实测 85% 降耗（5 server
58 工具 ~55k token 场景）且工具选择准确率 49%→74%（Opus 4）；#3176 基准 97%+（33.7k → ~750–880，
常驻索引仅 ~209 token）。模型在 5 个候选里挑比在 58 个里挑更准——**降耗与提质同向**。

### 5.4 关键坑：prompt cache 失效（传播最少但设计前必知）

> "Adding or removing tool definitions mid-conversation invalidates that cache, and the
> resulting miss can cost more tokens than the definitions you removed."
> —— MCP Client Best Practices

推理服务商按 prompt 前缀缓存，`tools` 数组就在前缀里；渐进发现的工作方式恰是**中途修改该数组**——
第 6 轮插入一个新 schema 会使变化点之后的缓存全部失效，长对话中可能比省下的还亏。官方缓解：

1. **append-only**：新发现的定义只追加在 cache breakpoint 之后，绝不重排序组；
2. **单稳态元工具**：所有调用经 `call_tool({name, args})` 单工具路由，**工具数组永远只有一个
   元素**——"MCP 转换为一个 tool"的极端形态，前缀绝对稳定。

2026-07-28 规范同步为"目录可缓存"铺路：`tools/list` 等结果携带 `ttlMs`/`cacheScope` 缓存提示与
确定性排序，`list_changed` 通知到达才视为失效。需区分三个不同的缓存：传输缓存（协议的
`ttlMs`）、宿主侧 memo（文档建议的定义缓存）、服务商 prompt cache（MCP 管不到）——混为一谈是
默认错误（分析见 https://development-wec.wiline.com/docs/news/mcp-progressive-discovery-prompt-cache/）。

### 5.5 协议层进展：目录内容化正在进入 spec

- **SEP-1576**（华为提出：`$ref` schema 去重、可选字段自适应、响应粒度、embedding top-k 检索）
  ——已关闭，状态 dormant；
- **SEP-2636 Progressive Tool Disclosure**（进行中，
  https://github.com/modelcontextprotocol/modelcontextprotocol/pull/2636）：`tools/catalog`
  （仅 name+summary）+ `tools/describe(names)`（批量取全量）+ 每工具 `schemaHash`（SHA-256，
  把缓存失效从全量重拉变成集合差运算）；原型 35 测试通过，实测典型轮次 34% 降耗、缓存热态 44%；
- 同族提案：SEP-1821（Dynamic Tool Discovery）、SEP-1862（Tool Resolution）、SEP-1881
  （Scope-Filtered Discovery）、SEP-2564（服务端列表过滤）——五六个 SEP 同时攻"目录即数据"
  这一个方向，拥挤本身即趋势证明。

### 5.6 边界与失败模式

1. **代码执行模式非免费午餐**（CE-MCP 论文，https://arxiv.org/html/2602.15945）：在结构化/
   数据并行任务上全面占优，但迭代式文本密集任务传统直接调用更优（局部重试 vs 整段重写）；且
   模型生成代码引入新攻击面（异常媒介代码注入、能力合成）——用架构范式换执行风险。
2. **发现自身有成本**：多一轮往返；模型必须"先搜索再调用"，eager→lazy 切换后部分模型会对
   未见过的工具幻觉调用参数（mcp-lazy-proxy 作者在征集此类边缘案例）。缓解：~200 token 级
   常驻索引 + 工具 description 写明"先 search 再调用"。
3. **审计缺口**：优化落地后难以回答"是暴露太多工具、暴露错工具、还是暴露对了仍调错"；社区在
   推 OTel GenAI 的 `tool.selection.evaluated` 事件（只记计数/哈希/策略，不记内容）补 receipt 层。
4. **小目录别用**：官方阈值 1%–5%，两工具的 server 上元工具是纯增熵。

### 5.7 统一视角

该转换层本质是在模型上下文与 MCP 目录之间插入一个**检索器**——与 RAG 之于文档、Skills 之于
知识、Tool Search 之于工具目录是同一模式的三次重演。§3/§4 的"MCP 从一级公民退位"落到工程上
就是：**MCP 管线还在，但入口被检索器接管了**。

## 6. 需要修正的三点

1. **淡化 ≠ 衰落**：Zuplo 调研 72% 受访者预期 12 个月内用量增加；竞争者不联合资助将死的标准。
   精确表述："MCP didn't die. It got sorted."（协议从聚光灯下移进基础设施层。）
2. **现象有明确作用域**："被 shell/代码执行吸收"成立的前提是 agent 有 shell 与互联网访问。
   拓扑变化（移动/沙箱/多租户）时 CLI 路线直接不可行。
3. **部分论据的数据 provenance 有问题**：见下节。

## 7. 数据可信度备注（引用社区数字前必读）

- "Perplexity 说 MCP 吃掉 72% 上下文 / 三服务 143k token"是被拼接的一句话：Apideck（卖 MCP
  替代品）自己的数字是 ~55k（~40 工具），143k 是另一份被误引的报告，72% 无法溯源。上下文开销
  问题真实（20%+ 实测很多），但引用拼接版需谨慎
  （https://qualixar.com/research/blog/mcp-2026-07-28-stateless-explained 的考证）。
- Cloudflare"81% 浪费"、ScaleKit 32× 均来自有利益立场的厂商——方向可信、量级打折。

## 8. 对本仓库的启示

- **不把 MCP 当架构，把它当传输**（若未来引入）；编排/权限/审计住在 MCP 之上的层。
- 给 agent 供能优先级：已有 CLI/API > 少量粗粒度工具 + 按需发现 > 直连大量细粒度 MCP 工具。
  通用 proto/API→工具桥无法跨越语义鸿沟（工具产品设计：脱敏、结果文本化、错误语义），Thoughtworks
  Hold 环与 AWS tool design 文章
  （https://aws.amazon.com/blogs/machine-learning/mcp-tool-design-practical-approaches-and-tradeoffs/）
  均为此结论。
- 若未来 agent 工具目录增大（超过上下文窗口 1%–5% 阈值），按 §5 渐进发现模式设计入口：常驻
  轻量索引 + 按需取 schema 的元工具，并优先采用"单稳态元工具"形态（所有调用经
  `call_tool({name, args})` 路由，工具数组不变）以规避 prompt cache 失效（§5.4）。
- tool/MCP/skill 边界融合趋势意味着"插件作为装配 agent 的基本组件"（dsh 模式）与行业方向一致：
  MCP 只是插件里的一种组件选项，不是扩展机制本身。
- 具体到 agent_v2 的 RPC 依赖工具：结论为**不引入 MCP**，改为共享 gRPC runtime client——
  详见 `survey/dsh-grpc-runtime-client.md`。
