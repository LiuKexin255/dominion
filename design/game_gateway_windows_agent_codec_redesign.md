# Game Gateway 与 Windows Agent 视频编解码 v2 协作方案

## 背景

`projects/game/gateway` 与 `projects/game/windows_agent` 之间通过 WebSocket 传输游戏控制消息和视频媒体流。当前媒体链路已经事实上约定为：

* Windows Agent 使用 ffmpeg `gdigrab + libx264` 输出 H.264 fragmented MP4。
* Windows Agent 将 `ftyp+moov` 作为 `media_init`，将 `moof+mdat` 作为 `media_segment` 发送给 gateway。
* Gateway 缓存 init 和 segment，向 web 客户端广播，并使用 `mp4ff + hi264` 从关键帧 segment 解码 JPEG snapshot。

当前问题不在于缺少 fallback，而在于双方缺少明确的编解码协作契约：

* `windows_agent/internal/media/parser.go` 将每个 segment 都标记为 `KeyFrame: true`。
* `gateway/domain/mediacache/cache.go` 将 `key_frame` 作为 late-join catch-up 的起点依据。
* `gateway/domain/mediacache/snapshot.go` 将最新 keyframe segment 作为 snapshot 解码输入。
* `media_init` 与 `media_segment` 没有显式 init 关联，encoder 重启或配置变化后 gateway 可能混用旧 init 和新 segment。
* codec/container/profile 没有协议级声明和校验，`mime_type` 只是透传字符串。

因此，本方案将媒体协议升级为完整 v2，不保留旧协议兼容路径，不通过 fallback 修复错误媒体流。

## 目标

本方案完成后应达成以下效果：

* Gateway、Windows Agent、web 调用方统一使用 v2 WebSocket 媒体协议。
* 媒体协议显式表达 `stream_id`、`init_id`、codec、sequence 和 random-access 语义。
* Windows Agent 对编码事实负责，包括 init 生命周期和 random-access 判断。
* Gateway 对协议结构、init/segment 关联、segment 顺序和支持的 codec/container 做显式校验。
* Late-join web catch-up 与 snapshot 都基于同一套 random-access segment 索引。
* 出现媒体协议错误时明确失败、断开或标记 unavailable，不做猜测修复。

## 非目标

* 不保留 v1 media protocol 兼容逻辑。
* 不支持 H.265、AV1、VP8、VP9 等其他 codec。
* 不由 gateway 根据媒体内容猜测 codec 或修复错误 `mime_type`。
* 不将 snapshot 改为 agent 生成；snapshot 仍由 gateway 解码。
* 不重新设计 session service、token、WebSocket URL、Wails UI 或 control request 协议。
* 不实现动态码率、自适应分辨率或 WebRTC。

## 已决策项

| 决策 | 结论 |
|---|---|
| 协议升级方式 | 完整 v2，所有调用方同步修改，不考虑 v1 兼容。 |
| keyframe 字段 | 删除/替换 `key_frame`，使用 `optional bool random_access`。 |
| codec 支持 | 第一版只支持 `h264-avc`。 |
| snapshot 位置 | 继续由 gateway 从 H.264 fMP4 解码 JPEG。 |
| `stream_id` | Windows Agent 每次 `StartCapture` 生成，停止后再次开始必须生成新的 stream。 |
| `init_id` | 使用 init segment 的 SHA-256 hex 作为身份。 |
| segment 顺序 | 使用 `uint64 sequence`，替代字符串 `segment_id` 作为权威顺序字段。 |
| 协议错误 | 致命协议错误断开 agent 并标记 stream unavailable；非致命媒体错误丢弃并记录。 |
| fallback | 不通过旧字段、错误 init、非关键帧或其他 codec 进行 fallback。 |

## v2 线协议模型

继续使用 `GameWebSocketEnvelope` 的 `protojson` flattened oneof 表达。`GameWebSocketEnvelope` 不新增外层 `type/payload` wrapper。

### `GameHello`

```proto
message GameHello {
  GameClientRole role = 1 [(google.api.field_behavior) = REQUIRED];
}
```

约束：

* WebSocket 建立后的第一条业务消息必须是 hello。
* 所有调用方同步迁移到 v2 media message 结构，不在 hello 中携带单独版本字段。
* 旧 media 字段在 proto 中删除，旧调用方发送的 `segment_id/key_frame` 不再构成合法 v2 media message。

### `GameMediaInit`

```proto
message GameMediaInit {
  string stream_id = 1 [(google.api.field_behavior) = REQUIRED];

  // SHA-256 hex of segment.
  string init_id = 2 [(google.api.field_behavior) = REQUIRED];

  string mime_type = 3 [(google.api.field_behavior) = REQUIRED];

  // The first v2 implementation only supports "h264-avc".
  string codec = 4 [(google.api.field_behavior) = REQUIRED];

  // Complete fMP4 init segment: ftyp + moov.
  bytes segment = 5 [(google.api.field_behavior) = REQUIRED];
}
```

约束：

* `stream_id` 非空。
* `init_id` 必须等于 `sha256(segment)` 的 hex 表达。
* `codec` 必须为 `h264-avc`。
* `mime_type` 必须与 H.264 fMP4 一致，例如 `video/mp4; codecs="avc1.64001f"`。
* `segment` 必须是完整 fMP4 init：`ftyp + moov`。
* 每次 encoder 重启、capture 重启或 codec config 变化，都必须发送新的 `stream_id` 或新的 `init_id`。

### `GameMediaSegment`

```proto
message GameMediaSegment {
  string stream_id = 1 [(google.api.field_behavior) = REQUIRED];

  string init_id = 2 [(google.api.field_behavior) = REQUIRED];

  uint64 sequence = 3 [(google.api.field_behavior) = REQUIRED];

  // Complete fMP4 media fragment: moof + mdat.
  bytes segment = 4 [(google.api.field_behavior) = REQUIRED];

  // True iff this segment starts at an independently decodable random access
  // point for the referenced init_id.
  optional bool random_access = 5 [(google.api.field_behavior) = REQUIRED];

  google.protobuf.Timestamp media_time = 6 [(google.api.field_behavior) = OUTPUT_ONLY];

  int32 duration_ms = 7 [(google.api.field_behavior) = OPTIONAL];

  bool discontinuity = 8 [(google.api.field_behavior) = OPTIONAL];
}
```

约束：

* `stream_id` 必须等于当前 active stream。
* `init_id` 必须引用已接收、已校验的 init。
* `sequence` 在同一 `stream_id` 内严格单调递增。
* `random_access` 必须显式设置。proto3 `optional bool` 用来区分“未发送”和 `false`。
* `random_access=true` 表示该 segment 的起始 sample 是可随机访问样本，并且与同一 `init_id` 的 init segment 组合后可独立解码。
* `segment` 必须是完整 `moof+mdat` fragment。
* `discontinuity=true` 表示此 segment 前存在流中断；gateway 应从该点重建 catch-up 状态。

## 职责划分

### Windows Agent

Windows Agent 是编码事实的来源，负责：

1. 配置 ffmpeg 输出 H.264 AVC-in-fMP4。
2. 每次 `StartCapture` 生成新的 `stream_id`。
3. 发送任何 media segment 前先发送 `media_init`。
4. 计算 `init_id = sha256(init_segment)`。
5. 每个 segment 绑定 `stream_id`、`init_id` 和单调递增 `sequence`。
6. 解析 fMP4 fragment，使用 MP4 sample flags 判断首个 sample 是否 random access。
7. 无法解析 sample flags、无法确认 random access 或 fragment 非法时，停止媒体流并向 gateway 报告错误。
8. 不再将每个 segment 默认标记为关键帧。

keyframe/random-access 检测应放在 `projects/game/windows_agent/internal/media`。可复用 `projects/game/gateway/testplan/fakeagent/media.go` 的思路：

* 从 init 的 `moov/mvex/trex` 获取默认 sample 信息。
* 对每个 fragment 使用 `mp4ff Fragment.GetFullSamples(trex)`。
* 读取首个 sample 的 flags。
* 使用 mp4ff 解析后的 `FullSample.IsSync()` 判断 random access；等价条件为 `sample_is_non_sync_sample == false` 且 `sample_depends_on == 2`，不要把全 0 flags 当作 random access。
* flags 缺失或解析失败时返回错误，不默认 true。

### Gateway

Gateway 是协议边界和媒体缓存负责人，负责：

1. 校验 hello role 合法。
2. 校验 `media_init` 的 `stream_id/init_id/codec/mime_type/segment`。
3. 按 `stream_id + init_id` 管理 init 和 segment。
4. 拒绝 segment-before-init、unknown init、stream mismatch、sequence 非递增和缺失 `random_access`。
5. 维护 bounded segment buffer 和 random-access index。
6. 对 late-join web 发送当前 init，然后从最新 `random_access=true` segment 开始补发。
7. Snapshot 只使用同一 `init_id` 的 `init_segment + latest random_access segment`。
8. 不从非 random-access segment、旧 init 或其他 codec 尝试 fallback 解码。

### Web 客户端

Web 客户端统一消费 v2 media messages：

* 使用 `media_init.mime_type` 初始化 MSE SourceBuffer。
* 将 `media_segment.segment` 按 sequence 顺序追加。
* 新连接时依赖 gateway catch-up，接收 init 和从 random-access 起点开始的 segment。
* 不期望 gateway 发送 v1 `segment_id/key_frame` 字段。

## 错误处理

### 致命协议错误

以下错误 gateway 应发送 `GameError{code: "protocol_error"}`，关闭 agent WebSocket，并将 runtime stream 标记为 unavailable：

* hello 缺失或 role 非法。
* unsupported codec 或 mime type。
* `media_segment` 早于 `media_init`。
* unknown `init_id`。
* `stream_id` 与当前 stream 不匹配。
* `sequence` 非严格递增。
* `random_access` 字段缺失。
* init hash 不匹配。

### 非致命媒体错误

以下错误不必断开 agent，但应丢弃该 segment、记录 `last_error`，并在必要时将 stream 标记为 degraded/unavailable：

* 单个 segment 超过大小限制。
* 单个 fragment MP4 解析失败。
* snapshot 解码失败。

Snapshot 失败时返回明确 unavailable，不尝试其他 segment fallback。

## 数据模型与缓存

Gateway media cache 应从当前简单 ring buffer 调整为 stream-aware 结构：

```go
type InitRef struct {
  StreamID string
  InitID   string
  MimeType string
  Codec    string
  Data     []byte
}

type SegmentRef struct {
  StreamID      string
  InitID        string
  Sequence      uint64
  Data          []byte
  RandomAccess  bool
  MediaTime     time.Time
  DurationMS    int32
  Discontinuity bool
}
```

缓存规则：

* 当前只维护一个 active stream。
* 收到新 `stream_id` 时清空旧 segment buffer、旧 random-access index 和 snapshot。
* 收到新 `init_id` 时替换 active init，并清空依赖旧 init 的 segment/snapshot。
* `GetSegmentsFromLastRandomAccess` 只返回同一 `stream_id/init_id` 下从最新 random-access segment 到末尾的连续 segment。
* 如果 buffer 中没有 random-access segment，新 web 只接收 init，不接收历史 segment。

## Snapshot 设计

Gateway 继续负责 snapshot：

1. 查找 active init。
2. 查找同一 `stream_id/init_id` 下最新 `random_access=true` segment。
3. 拼接 `init.segment + media.segment`。
4. 使用 `mp4ff` 提取首个样本。
5. 从 init 的 `avcC` 提取 SPS/PPS。
6. 使用 `hi264` 解码 H.264。
7. 输出 JPEG。

如果任一步失败，返回 snapshot unavailable。不得尝试：

* 非 random-access segment。
* 旧 init。
* 其他 codec。
* AVC/AnnexB 之外的猜测式修复路径。

## 代码分层

### Proto

修改 `projects/game/gateway/gateway.proto`：

* `GameMediaInit` 替换为 v2 字段。
* `GameMediaSegment` 替换为 v2 字段。
* 删除 `segment_id` 和 `key_frame`。
* 使用 `optional bool random_access`。

### Windows Agent

修改：

* `projects/game/windows_agent/internal/media/parser.go`
  * 保存 init 上下文和 `trex`。
  * 对每个 fragment 真实计算 `RandomAccess`。
  * `MediaSegment` 使用 `Sequence`、`RandomAccess`。
* `projects/game/windows_agent/internal/runtime/media_flow.go`
  * 每次 `StartCapture` 生成 `stream_id`。
  * 发送 init 时计算 `init_id`。
  * 发送 segment 时附带 `stream_id/init_id/sequence/random_access`。
* `projects/game/windows_agent/internal/transport/sender.go`
  * 更新 `SendMediaInit` 和 `SendMediaSegment` 参数。
* `projects/game/windows_agent/internal/transport/constants.go`
  * 增加 `CodecH264AVC = "h264-avc"`。

### Gateway

修改：

* `projects/game/gateway/ws_validate.go`
  * hello 必须校验 role 合法。
  * media init/segment 必须校验 v2 必填字段。
* `projects/game/gateway/domain/media.go`
  * 更新 init/segment domain model。
* `projects/game/gateway/domain/mediacache/cache.go`
  * 改为 stream/init aware cache。
  * `GetSegmentsFromLastKeyframe` 替换为 `GetSegmentsFromLastRandomAccess`。
* `projects/game/gateway/domain/mediacache/snapshot.go`
  * 只接受 random-access segment。
* `projects/game/gateway/service/gateway.go`
  * 处理 `media_init` 时设置 active init。
  * 处理 `media_segment` 时校验 init/sequence/random_access 字段。
  * 协议错误按分级策略返回。

### Fake Agent 与测试计划

修改：

* `projects/game/gateway/testplan/fakeagent`
  * 发送 v2 hello。
  * 发送 v2 media init/segment。
  * 使用真实 random-access 检测，不再发送 `key_frame`。
* `projects/game/gateway/testplan/interface_test.go`
  * 更新 MIME、catch-up、snapshot 断言。

## 测试计划

### Windows Agent 单元测试

覆盖：

* init parsing 成功后可生成 `init_id`。
* parser 对 random-access fragment 返回 `RandomAccess=true`。
* parser 对 non-sync fragment 返回 `RandomAccess=false`。
* parser 对缺失/无法解析 sample flags 的 fragment 返回错误。
* Stop/StartCapture 后 `stream_id` 改变，sequence 从 0 重新开始但不与旧 stream 混淆。
* `SendMediaInit`/`SendMediaSegment` wire payload 包含 v2 字段。

### Gateway 单元测试

覆盖：

* unsupported codec 被拒绝。
* init hash 不匹配被拒绝。
* segment before init 被拒绝。
* unknown init_id 被拒绝。
* sequence 非递增被拒绝。
* random_access 缺失被拒绝。
* non-random-access segment 不作为 catch-up 起点。
* latest random-access 之后的连续 segment 被补发。
* init 切换后旧 segment 不参与 catch-up 或 snapshot。
* snapshot 只从 matching init + random-access segment 解码。

### 集成/大型测试

更新：

* `projects/game/gateway/testplan/interface_test.go`
* `projects/game/gateway/testplan/fakeagent`
* `projects/game/testplan` 中依赖 gateway media protocol 的用例。

重点验证：

* Agent 使用 v2 media message 结构连接 gateway。
* Web late-join 收到 init 和从 random-access 起点开始的 segment。
* Snapshot 返回真实 JPEG，且无 random-access 时明确 unavailable。
* 旧 v1 media JSON 不被接受。

### Bazel 验证

实现完成后执行：

```bash
bazel run @rules_go//go -- fmt <changed-go-files>
bazel run //:gazelle projects/game/gateway projects/game/windows_agent
bazel test //projects/game/gateway/...
bazel test //projects/game/windows_agent/...
bazel test //projects/game/testplan/...
bazel build //projects/game/gateway/...
bazel build //projects/game/windows_agent/...
```

如 proto 或依赖变化导致 Bazel module 需要同步，再执行：

```bash
bazel mod tidy
```

## 迁移步骤

1. 修改 `gateway.proto` 为完整 v2 media protocol。
2. 更新 gateway 生成代码依赖和 BUILD。
3. 更新 Windows Agent transport/runtime/media parser。
4. 更新 Gateway media domain、cache、service 和 validation。
5. 更新 fakeagent、gateway tests、windows_agent tests 和大型测试。
6. 运行格式化、gazelle、bazel test/build。

## 验收标准

* v1 `segment_id/key_frame` 不再被生产代码使用。
* Windows Agent 不再硬编码所有 segment 为 random access。
* Gateway 不再把非 random-access segment 作为 catch-up 或 snapshot 起点。
* Segment 必须引用已知 `init_id`。
* Encoder restart/capture restart 后不会混用旧 init 与新 segment。
* Gateway snapshot 仍可从 H.264 fMP4 random-access segment 解码 JPEG。
* `bazel test //projects/game/gateway/...` 通过。
* `bazel test //projects/game/windows_agent/...` 通过。
* 相关大型测试通过，或明确记录非本变更导致的环境问题。

## 未来规划

* 如未来支持多 codec，可在 hello 中扩展 capabilities，并为 gateway snapshot 增加 codec-specific extractor。
* 如未来希望降低 gateway CPU，可增加 agent-side snapshot 协议，但这不属于本次 v2 media protocol。
* 如未来需要独立滚动发布，可重新引入显式 capability negotiation，但不得引入旧字段 fallback。
