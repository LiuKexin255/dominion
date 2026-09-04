// 思考过程折叠行（props 契约 specs/049-agent-v2-dsh-init/contracts/
// web-frontend.md §3.2 ReasoningRow）：DisclosureRow 外壳 + IconThinkOutline14，
// 默认折叠；折叠摘要 running 时跟随最新行、完成态显示首行；展开体以
// MarkdownText 渲染完整思考文本（specs/054-agent-v2-bugfixes/contracts/
// web-ui.md §3：思考展开体与正文同能力，GFM、流式增量、不完整片段不崩溃
// ——running 即流式增长中，传给 MarkdownText 的 streaming prop）；折叠摘要
// 保持纯文本首行/最新行切片（原文切片语义，不经 markdown）；data-state=
// "running|ok" 供样式区分；无思考文本不渲染（web-frontend.md §6 测试义务 2
// 「无思考不渲染（上游组件级）」）。交互与摘要语义参照 dsh-web ReasoningRow
// 改造，剥离 locale/slot 依赖（attribution 与来源链接见包 README）：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/
// packages/client/ui-chat/src/client/chat/ReasoningRow.tsx
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import {
  DisclosureRow,
  IconThinkOutline14,
  MarkdownText,
} from '@deepseek-ai/dsh-client-ui-primitives'

export interface ReasoningRowProps {
  text: string
  running: boolean
}

function firstLine(text: string): string {
  const newline = text.indexOf('\n')
  return newline === -1 ? text : text.slice(0, newline)
}

function latestLine(text: string): string {
  const visible = text.trimEnd()
  const newline = visible.lastIndexOf('\n')
  return newline === -1 ? visible : visible.slice(newline + 1)
}

// 帧节流的视觉对齐调度（3 帧间隔合并高频流式 delta 的 DOM 对齐，卸载时取消
// 在途帧）；语义与上游 use-throttled-visual-update 一致：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/use-throttled-visual-update.ts
function useThrottledVisualUpdate(update: () => void): () => void {
  const updateRef = useRef(update)
  updateRef.current = update
  const pendingFrameRef = useRef<number | null>(null)
  useLayoutEffect(
    () => () => {
      if (pendingFrameRef.current === null) return
      cancelAnimationFrame(pendingFrameRef.current)
      pendingFrameRef.current = null
    },
    [],
  )
  return useCallback(() => {
    if (pendingFrameRef.current !== null) return
    let remainingFrames = 3
    const advance = (): void => {
      remainingFrames -= 1
      if (remainingFrames > 0) {
        pendingFrameRef.current = requestAnimationFrame(advance)
        return
      }
      pendingFrameRef.current = null
      updateRef.current()
    }
    pendingFrameRef.current = requestAnimationFrame(advance)
  }, [])
}

export function ReasoningRow({ text, running }: ReasoningRowProps) {
  const [expanded, setExpanded] = useState(false)
  const summaryRef = useRef<HTMLSpanElement>(null)
  const summary = running ? latestLine(text) : firstLine(text)
  const scheduleSummaryScroll = useThrottledVisualUpdate(() => {
    const element = summaryRef.current
    if (element === null) return
    element.scrollLeft = running ? element.scrollWidth - element.clientWidth : 0
  })
  useEffect(() => {
    scheduleSummaryScroll()
  }, [running, scheduleSummaryScroll, summary])

  if (text.trim() === '') return null

  return (
    <div
      className="reasoning-row"
      data-testid="reasoning-row"
      data-variant="think"
      data-state={running ? 'running' : 'ok'}
    >
      {running && <span className="visually-hidden">正在生成</span>}
      <DisclosureRow
        rowClassName="reasoning-row-line"
        icon={<IconThinkOutline14 size={14} />}
        title="思考过程"
        open={expanded}
        expandable
        expandOnRowClick
        onToggle={() => {
          setExpanded((value) => !value)
        }}
        collapsedContent={
          <>
            <span className="reasoning-separator" aria-hidden />
            <span
              ref={summaryRef}
              className="reasoning-summary"
              data-testid="reasoning-summary"
              data-follow-end={running || undefined}
            >
              {summary}
            </span>
          </>
        }
      >
        <div className="reasoning-body" data-testid="reasoning-body">
          <MarkdownText text={text} streaming={running} />
        </div>
      </DisclosureRow>
    </div>
  )
}
