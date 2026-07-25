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

/** Matches the pixel-dimension annotation emitted by buildResultBlocks. */
const PIXEL_SIZE_PATTERN = /图片像素尺寸[：:]?\s*(\d+)\s*[×xX*]\s*(\d+)/;

/** Parsed tool-result display fields extracted from content blocks. */
export interface ParsedToolResult {
  message: string;
  screenshot?: { data: string; widthPx: number; heightPx: number };
}

/**
 * Parse a tool's content-block array (the LangChain ToolMessage.content shape)
 * into display fields: the first non-annotation text block → message; an
 * image_url block → screenshot.data (base64, data-url prefix stripped); the
 * pixel-size annotation text → screenshot widthPx/heightPx. A plain-string
 * content is used as the message directly.
 *
 * Used by BOTH the live emission (llm.ts reads `stream.toolCalls` output) and
 * history reconstruction (handler.ts `ListMessages` reads the checkpointed
 * ToolMessage), so live and history render identically (spec 023 FR-009).
 */
export function parseToolResultFields(content: unknown): ParsedToolResult {
  const blocks = Array.isArray(content)
    ? (content as { type?: string; text?: string; image_url?: { url?: string } }[])
    : [];
  let message = "";
  let data = "";
  let widthPx = 0;
  let heightPx = 0;
  for (const block of blocks) {
    if (block.type === "text" && typeof block.text === "string") {
      const dims = block.text.match(PIXEL_SIZE_PATTERN);
      if (dims) {
        widthPx = Number.parseInt(dims[1], 10) || 0;
        heightPx = Number.parseInt(dims[2], 10) || 0;
      } else if (!message) {
        message = block.text;
      }
    } else if (block.type === "image_url" && block.image_url?.url) {
      data = block.image_url.url.replace(/^data:image\/[^;]+;base64,/, "");
    }
  }
  if (!message && typeof content === "string") {
    message = content;
  }
  const result: ParsedToolResult = { message };
  if (data) result.screenshot = { data, widthPx, heightPx };
  return result;
}
