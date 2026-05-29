# Agent 玩游戏 step2 评审收敛方案

本文档记录 `design/game/game-agent-step2.md` 实现评审后的收敛结论。本文档作为 step2 的补充设计，不修改原方案文件。

## 结论摘要

1. session id 生成暂不调整，保留当前会返回 error 的生成接口。
2. `ListSessions` 默认 page size 定义为常量，最大 page size 为 `1000`。
3. `ListSessions` 分页排序改为 `create_time` 倒序，次要排序 `session_id` 倒序。
4. `ListSessions` page token 使用 base64 编码的 JSON 结构，包含 `create_time` 与 `session_id`。
5. 列表分页通过多查询一条记录判断是否存在下一页。
6. 空对象返回策略收敛为：无语义的空 domain envelope 可返回 `nil`，协议必须返回空消息的场景仍返回对应 empty message。
7. 从 screenshot 协议中移除 `client_x_px` 与 `client_y_px`，agent 操作只表达相对 client area / screenshot 坐标，由 desktop 转换为屏幕绝对坐标。

## session id 生成

当前实现保留：

```go
type IDGenerator interface {
    NewID(ctx context.Context) (string, error)
}
```

原因：step2 暂不把 id generator 的 error 语义作为本轮改动范围。后续如需收敛为无 error 接口，可单独评估使用 `uuid.NewString()`、`ulid.Make()` 或 must-style crypto random 生成器。

## ListSessions page size

`ListSessions` 应定义统一常量，避免 handler 与 repository 各自硬编码默认值。

建议常量：

```go
const (
    DefaultListSessionsPageSize = 50
    MaxListSessionsPageSize     = 1000
)
```

处理规则：

1. `page_size <= 0` 时使用 `DefaultListSessionsPageSize`。
2. `page_size > MaxListSessionsPageSize` 时截断为 `MaxListSessionsPageSize`。
3. handler 与 repository 使用同一组语义，避免默认值或上限漂移。

## ListSessions 排序与分页

排序规则：

```text
create_time DESC, session_id DESC
```

选择该排序的原因：

1. session 列表默认展示最新创建的 session 更符合 UI 使用习惯。
2. `create_time` 可能相同，`session_id` 作为次要排序键保证顺序稳定。
3. 两个字段均倒序时，下一页 cursor 条件直观且可稳定复现。

MongoDB 建议索引：

```text
{ create_time: -1, session_id: -1 }
```

### page token 结构

`page_token` 使用 base64url 编码的 JSON 结构，不直接拼接字符串。

JSON 结构建议：

```json
{
  "create_time": "2026-05-29T12:34:56.789Z",
  "session_id": "abc123"
}
```

编码规则：

1. JSON 字段名固定为 `create_time` 与 `session_id`。
2. `create_time` 使用 RFC3339Nano UTC 字符串。
3. 外层使用 base64 URL encoding，建议不带 padding。
4. 非法 token 应返回 `InvalidArgument`，不能包装为 `Internal`。

### 下一页查询条件

当请求携带 token `{create_time, session_id}` 时，下一页 filter 为：

```text
create_time < token.create_time
OR (
  create_time == token.create_time
  AND session_id < token.session_id
)
```

查询时应多取一条：

```text
limit = pageSize + 1
```

返回规则：

1. 若查询结果数量为 `pageSize + 1`，表示存在下一页。
2. 返回给客户端前截断为前 `pageSize` 条。
3. `next_page_token` 使用截断后最后一条 session 的 `create_time` 与 `session_id` 生成。
4. 若查询结果数量小于等于 `pageSize`，`next_page_token` 为空。

该规则修复“当前页最后一个对象也是全局最后一个对象时仍返回 next token”的问题。

## 空对象返回策略

空对象处理按语义区分：

1. 单个 entity 不存在时，repository 继续返回业务错误，例如 `ErrNotFound`。
2. 转换函数遇到 nil 输入时返回 nil，避免制造无语义空对象。
3. `ListSessions` 无结果时，domain 层可以返回 `nil, nil` 或带 nil/empty sessions 的 result，但实现应统一风格。
4. handler 层负责把 domain 空结果转换为协议空响应，例如空 `ListSessionsResponse`。
5. `DeleteSession` 等协议要求返回 `google.protobuf.Empty` 的场景，继续返回 `new(emptypb.Empty)`。

推荐实现倾向：`ListSessions` repository 在无结果时返回 `nil, nil`，handler 将其转换为 `&game.ListSessionsResponse{}`。

## 移除 screenshot 中的 client 坐标

从 `AgentScreenshotFrame` 中移除：

```proto
int32 client_x_px = 6;
int32 client_y_px = 7;
```

移除后在 proto 中保留字段号与字段名：

```proto
reserved 6, 7;
reserved "client_x_px", "client_y_px";
```

理由：

1. step2 截取完整窗口 client area，截图左上角天然对应 client area 原点。
2. agent 不需要感知 desktop 所在屏幕的绝对坐标。
3. 后续 agent 输出鼠标/键盘操作时，只提供相对 client area 或 screenshot 的像素坐标。
4. desktop 持有窗口绑定关系，应由 desktop 将相对坐标转换为目标窗口 client area 的屏幕绝对坐标并执行操作。

同步修改范围：

1. `projects/game/game.proto` 的 `AgentScreenshotFrame`。
2. agent domain `ScreenshotInput` 中的 `ClientXPx` / `ClientYPx`。
3. agent handler 的 screenshot proto-to-domain 转换。
4. desktop frontend/API 类型定义。
5. protojson、gateway、agent、desktop WebSocket 相关测试数据。

## 测试补充要求

分页至少补充以下用例：

1. `page_size <= 0` 使用默认值。
2. `page_size > 1000` 截断为 `1000`。
3. 总数刚好等于 page size 时 `next_page_token` 为空。
4. 总数大于 page size 时返回 `next_page_token`，且响应 sessions 已截断。
5. 多条 session 拥有相同 `create_time` 时，按 `session_id DESC` 稳定分页。
6. 携带合法 base64 JSON token 能返回下一页。
7. 非法 base64 token、非法 JSON token、缺字段 token 返回 `InvalidArgument`。

协议变更至少补充以下用例：

1. `AgentScreenshotFrame` protojson 不再包含 `client_x_px` / `client_y_px`。
2. agent 接收 screenshot 后仍能返回 ack。
3. desktop 发送 screenshot 时只包含截图尺寸、编码、数据、scale factor、window title 与 capture time。

## 待定项

无阻塞待定项。

后续实现时只需在代码中落实上述常量、cursor 编码、分页查询和协议字段移除规则。
