# Research: Mouse Move Action & Post-Operation Screenshot Feedback

**Feature**: 014-mouse-move-screenshot
**Date**: 2026-06-25

## R-001: LangChain Tool Image Return Format

**Decision**: Return a content-block array `[{type:"text",...}, {type:"image_url",...}, {type:"text",...}]` from the mouse tool callback.

**Rationale**: The `tool()` function from `langchain@1.5.0` calls `_formatToolOutput()` in `@langchain/core@1.2.0`. When the callback returns an array where every element has a `.type` property, the array is passed through verbatim as `ToolMessage.content`. Both `@langchain/anthropic` and `@langchain/openai` providers serialize `image_url` blocks in tool results — Anthropic via `_formatContentBlocks` → `_formatImage` (converts `data:` URI to `{type:"base64", media_type, data}`), OpenAI natively accepts `{type:"image_url", image_url:{url}}`.

**Evidence**:
- [_formatToolOutput source](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — checks `Array.isArray(content) && content.every(_isMessageContentBlockShaped)`, passes through as `ToolMessage({content: array})`
- [Anthropic _formatContentBlocks](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/providers/langchain-anthropic/src/utils/message_inputs.ts#L148-L162) — handles `image_url` blocks in tool results

**Version pins**: `langchain@1.5.0`, `@langchain/core@1.2.0`, `@langchain/anthropic@^1.5.0`, `@langchain/openai@^1.5.1` (from `pnpm-lock.yaml`).

**Alternatives considered**:
- `responseFormat: "content_and_artifact"` (two-tuple return `[content, artifact]`): more complex, artifact is not sent to model. Rejected because we want the image sent to the model.
- String-only return with base64 inline: rejected because the model needs structured `image_url` blocks to process images, not a raw base64 string.

---

## R-002: Go Screenshot Overlay Marker (stdlib-only)

**Decision**: Use Go stdlib `image`, `image/color`, `image/draw`, `image/png` to draw a high-contrast ring marker on the captured screenshot at the executed coordinate.

**Rationale**: No external dependencies are needed. The screenshot is returned as PNG bytes from `capture.CaptureWindow`. We decode the PNG with `png.Decode`, draw a circle outline (radius ~12px) at `(xPx, yPx)` using a simple midpoint-circle algorithm on the `image.RGBA`, and re-encode with `png.Encode`. A ring (rather than a filled dot or crosshair) is chosen because it marks the exact target without obscuring the pixel underneath.

**Algorithm**: Midpoint circle algorithm — iterate octants to compute ring pixel positions. Color: `color.RGBA{R:255, G:0, B:0, A:255}` (pure red) for maximum contrast against arbitrary UI backgrounds. The marker is drawn directly on the decoded image's pixel buffer (after asserting or converting to `*image.RGBA` or `*image.NRGBA`).

**No new dependencies**: All packages are in Go stdlib. No `golang.org/x/image` needed.

**Alternatives considered**:
- `golang.org/x/image/font` for text labels: adds an external dependency for minimal benefit (the coordinate is already in the text annotation). Rejected per §III minimal-dependency principle.
- Filled dot: obscures the target pixel, making it harder for the agent to see what's underneath. Rejected.
- Crosshair: visually cleaner but requires drawing two line segments; the ring is simpler and equally effective.

---

## R-003: Proto Backward Compatibility for AgentOperationResultFrame Extension

**Decision**: Add `AgentImageFrame screenshot = 4;` as an optional field to `AgentOperationResultFrame`. This is wire-compatible with existing consumers.

**Rationale**: Protocol Buffers v3 treats all fields as optional by default. Adding a new field with a previously unused tag number (4) is a backward-compatible change: existing consumers that do not read field 4 simply ignore it. The proto3 `optional` keyword ensures the field is tracked for presence (`hasScreenshot`), distinguishing "no screenshot" from "screenshot with zero values".

**Evidence**: [Protocol Buffers Language Guide — Updating Message Types](https://protobuf.dev/programming-guides/proto3/#updating) — "Adding new fields: ... as long as the field number is not reused, the change is forward and backward compatible."

**Alternatives considered**:
- New message type wrapping the old: rejected, adds indirection without benefit.
- Sidecar image delivery (separate frame): rejected, breaks the one-round-trip dispatch/result contract of OperationBridge.

---

## R-004: MOVE Action Execution (Empty Event Sequence)

**Decision**: The MOVE action is handled by the existing `ExecuteMouseAction` two-phase flow. `validateMouseAction` adds MOVE to valid actions; `actionEventSequence` returns an empty slice `[]uint32{}` for MOVE, so `ExecuteMouseAction` performs Phase 1 (`SetCursorPos`) and skips Phase 2 (no button events to dispatch).

**Rationale**: The current `ExecuteMouseAction` already calls `setCursorPos` before the event loop. For MOVE, the loop body never executes (zero iterations), which is exactly the desired behavior — cursor moves, no buttons. This is a natural extension of the existing design, not a new code path.

**Design verdict (§V)**: The existing `ExecuteMouseAction` design (validate → bounds → SetCursorPos → event sequence) already serves MOVE without structural change. `validateMouseAction` gains one enum case; `actionEventSequence` gains one case returning an empty slice. No new function, no new branch in `ExecuteMouseAction` itself.

---

## R-005: Post-Action Screenshot Flow in executeAgentOperation

**Decision**: Refactor `executeAgentOperation` to attempt screenshot capture after the mouse action regardless of success/failure (FR-007), and include it on the result frame when capture succeeds and the image is within the size limit.

**Rationale**: The current function returns early on any failure (`failed(msg)`). The refactor restructures it so that:
1. Mouse action is attempted first.
2. If a window is bound, screenshot is attempted next (even if action failed).
3. The result frame's status reflects the action outcome; the message may include screenshot failure info; the screenshot field is set only when capture succeeds and size ≤ 5 MiB.

**Design verdict (§V)**: The `failed()` helper currently returns immediately with FAILED status. This is refactored so the function always flows through to the screenshot attempt. The helper is preserved but restructured to not return early — instead, it records the error and continues to the screenshot phase. This is a refactor of the function's control flow, not stacked logic.

---

## R-006: Image Size Annotation for Tool Results (FR-014)

**Decision**: The mouse tool callback appends a text block with pixel dimensions after the screenshot image_url block, mirroring the annotation pattern already used for user-turn images in `streamFromAgent`.

**Rationale**: The user clarified that the size-annotation text block should apply universally to all images the agent sees. User-turn images are already annotated in `llm.ts`. Tool-result images are annotated in the mouse tool callback. The annotation format is identical: `[图片像素尺寸：${w}×${h}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`. No shared utility function is needed because the two call sites use different data shapes (TurnContent fields vs. OperationScreenshot fields) and the annotation is a single-line template literal.

## References

### Official Documentation

- [Protocol Buffares Language Guide — Updating Message Types](https://protobuf.dev/programming-guides/proto3/#updating) — proto3 backward-compatible field addition

### Repositories

- [langchain-ai/langchainjs — _formatToolOutput](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — tool content-block array passthrough (langchain v1.5.0 / @langchain/core v1.2.0)
- [langchain-ai/langchainjs — Anthropic _formatContentBlocks](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/providers/langchain-anthropic/src/utils/message_inputs.ts#L148-L162) — image_url serialization in tool results

### Articles & RFCs

- No external articles or RFCs referenced.
