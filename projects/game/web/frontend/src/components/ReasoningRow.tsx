// 思考过程折叠行（props 契约 specs/049-agent-v2-dsh-init/contracts/
// web-frontend.md §3.2 ReasoningRow）：DisclosureRow 外壳 + IconThinkOutline14，
// 默认折叠；折叠摘要 running 时以纯 CSS follow-end 右对齐露出最新行末尾
// （specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §2，对齐上游
// 双层 summary > summaryText 结构，无程序化滚动），完成态显示首行 + ellipsis；
// 根节点 data-expanded 是折叠态布局围栏（theme.css
// `.reasoning-row:not([data-expanded])` contain: size layout）的样式钩子；
// 展开体以 MarkdownText 渲染完整思考文本（specs/054-agent-v2-bugfixes/
// contracts/web-ui.md §3：思考展开体与正文同能力，GFM、流式增量、不完整
// 片段不崩溃——running 即流式增长中，传给 MarkdownText 的 streaming prop）；
// 折叠摘要保持纯文本首行/最新行切片（原文切片语义，不经 markdown）；
// data-state="running|ok" 供样式区分；无思考文本不渲染（web-frontend.md §6
// 测试义务 2「无思考不渲染（上游组件级）」）。交互与摘要语义参照 dsh-web
// ReasoningRow 改造，剥离 locale/slot 依赖（attribution 与来源链接见包
// README）：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/
// packages/client/ui-chat/src/client/chat/ReasoningRow.tsx
import { useState } from 'react'
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

export function ReasoningRow({ text, running }: ReasoningRowProps) {
  const [expanded, setExpanded] = useState(false)
  const summary = running ? latestLine(text) : firstLine(text)

  if (text.trim() === '') return null

  return (
    <div
      className="reasoning-row"
      data-testid="reasoning-row"
      data-variant="think"
      data-state={running ? 'running' : 'ok'}
      data-expanded={expanded || undefined}
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
            <span className="reasoning-summary" data-testid="reasoning-summary" data-follow-end={running || undefined}>
              <span className="reasoning-summary-text">{summary}</span>
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
