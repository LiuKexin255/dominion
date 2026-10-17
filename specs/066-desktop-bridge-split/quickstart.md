# Quickstart: DesktopBridge v2 拆分验证

> 契约：[contracts/desktop-bridge-v2.md](contracts/desktop-bridge-v2.md) · 数据模型：[data-model.md](data-model.md) · 本文档是验证/运行指南，实现细节归 tasks.md。

## 前置

- 仓库内 bazel 环境可用（`bazel`、`bazel run //:go` 等，见 `AGENTS.md`）。
- 大型测试使用 testplan skill（`tools/test/guitar`）；执行前已阅读 `style/large_test.md`。

## 1. 编译与单测（每次变更的快速门禁）

```bash
bazel build //...
bazel test //projects/game/... //common/js/dsh-plugins/...
```

预期：全绿。重点单测面（断言语义见契约 §6）：

| 包 | 覆盖 |
|---|---|
| `common/js/dsh-plugins/desktop-bridge` | attach 首帧推送/心跳启停、接管 no-op、`SendFrame` FAILED_PRECONDITION、tool_id 回执 resolve、断开 settle |
| `projects/game/desktop/internal/api` | NDJSON 行解析、首帧超时、octet-stream 编码、错误体分派解析 |
| `projects/game/proxy/handler` | Stream/SendFrame 转发、owner 策略（get-or-create vs lookup-only）、错误映射 |
| `projects/game/gateway/cmd` | body 上限 413 与归因报错、无 WS 分支后的路由 |
| `common/js/dsh-plugins/saolei-loop` | 统计口径回归（仅计已执行操作——本特性零改动，回归保护） |

## 2. 大型测试（验收门禁，原则 VI：实际执行 + 全部通过）

```bash
# 经 testplan skill 执行（部署→测试→清理闭环）
guitar run projects/game/testplan/system_test.yaml
```

关键用例与预期结果：

| 用例 | 预期 |
|---|---|
| desktop_flow：首帧 status | `GET :stream` 后 10s 内收到 ACTIVE status 帧（无上行探针） |
| desktop_flow：接管 | 第二条流打开后旧流结束；新流持续收帧 |
| desktop_flow：派发→回执→识别 | team 链路驱动操作 → desktop 收操作请求 → POST 回执 → 棋盘识别推进 |
| desktop_flow：在途断连 | 关流后 dispatch 失败 "desktop disconnected" |
| desktop_flow：空闲存活 | 无操作请求期间持续收到 ~10s 心跳 status 帧，链路不被中间层断开（SC-004） |
| desktop_flow：超限归因 | >8MiB 回执 POST 返回 413/清晰大小错误；≤5MiB body 字节数等于 proto 载荷（SC-003） |
| game（won 拓扑，fake-desktop） | 多局游戏全链路 + 终局统计播报，内容与实际派发一致 |
| game_disconnect（drop 拓扑） | 三局序列：正常局 → 断连局（"desktop disconnected" 错误入模）→ 重连恢复局 |
| conversation / stall / memory 等 | 既有回归全部通过（对话面与本重构正交） |

## 3. 手工冒烟（可选）

1. 部署 SUT 后，用 curl 验证下行流形态：`curl -N -H 'env: <env>' https://<host>/api/v2/templates/saolei/sessions/<id>:stream` —— 立即输出一行 `{"result":{...status...}}`，随后 ~10s 一行心跳。
2. 验证超限归因：向 `:sendFrame` POST >8MiB body（带 Content-Length）—— 得到 413 预检（`text/plain`）；chunked/低报 Content-Length 上传则 400 + 二进制 `google.rpc.Status`（message 含 "request body too large"）。均不静默截断（错误体线格式见契约 §1.1）。
3. 验证无流拒绝：会话未开流时 POST 合法回执 —— HTTP 400 + 二进制 `google.rpc.Status` 错误体（code=9 `FAILED_PRECONDITION`；`protoc --decode_raw` 可检视 body）。

## 4. 完成判定

- [ ] 单测与大型测试全绿（无断言弱化）
- [ ] 全仓库检索无旧框架残留：`connect` WS 端点、`WSClient`、`bind.Binder`、`WithFirstFrame`、`wsStream`、`pumpWebSocketConnect`、探针 echo（`respondProbe`/`sendFlowProbe`）、`deriveSessionName`、`disconnectDrainPause` 均无引用
- [ ] `git grep -E 'coder/websocket|gorilla/websocket' -- '*.go'` 仅剩与本链路无关的使用（第三方 vendored 代码除外）
