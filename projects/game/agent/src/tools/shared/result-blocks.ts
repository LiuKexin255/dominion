/**
 * result-blocks.ts — LangChain content-block builder shared by the mouse tools.
 *
 * Emits the status text always, and — when the desktop captured a screenshot—
 * the image plus a pixel-dimension annotation so the model can re-estimate
 * coordinates against the correct pixel space.
 *
 * Relocated verbatim from `src/mouse-tool.ts` (Feature 020 — per-tool-name
 * directory layout, see
 * `specs/020-agent-resources-layout/contracts/directory-layout.md`).
 */

import type { OperationResult } from "../../operation-bridge";

/**
 * A content block returned to LangChain. Mirrors the subset of the LangChain
 * multimodal content shape consumed by `_formatToolOutput`: an array whose
 * elements each carry a `type` discriminator is passed through verbatim as the
 * `ToolMessage.content`, so an `image_url` block reaches the model as a real
 * image rather than a stringified blob.
 */
export type MouseContentBlock =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

/**
 * Build the LangChain content-block array for a tool result.
 *
 * Emits the status text always, and — when the desktop captured a screenshot—
 * the image plus a pixel-dimension annotation so the model can re-estimate
 * coordinates against the correct pixel space.
 */
export function buildResultBlocks(
  result: OperationResult,
): MouseContentBlock[] {
  const blocks: MouseContentBlock[] = [
    { type: "text", text: result.message },
  ];
  if (result.screenshot) {
    blocks.push({
      type: "image_url",
      image_url: {
        url: `data:image/png;base64,${result.screenshot.data}`,
      },
    });
    blocks.push({
      type: "text",
      text: `[图片像素尺寸：${result.screenshot.widthPx}×${result.screenshot.heightPx}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`,
    });
  }
  return blocks;
}
