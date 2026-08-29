// 工具调用卡片（props 契约 specs/049-agent-v2-dsh-init/contracts/
// web-frontend.md §3.2 ToolCard）：名称 / 参数（JsonBlock 默认折叠）/ 状态
// （StateDot：RUNNING→ongoing、SUCCEEDED→done、FAILED→error）/ result 关联
// 展示于同一卡片。单卡形态参照 dsh-web ToolCallTree（剥离 renderSlot/
// 子调用树，attribution 与来源链接见包 README）：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx
// 卡内布局另参照同上游的单卡形态 GenericToolCard：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/toolviews/GenericToolCard.tsx
import { JsonBlock, StateDot } from '@deepseek-ai/dsh-client-ui-primitives'
import type { StateDotState } from '@deepseek-ai/dsh-client-ui-primitives'

export type ToolCardStatus = 'RUNNING' | 'SUCCEEDED' | 'FAILED'

export interface ToolCardProps {
  toolId: string
  name: string
  argsJson: string
  status: ToolCardStatus
  result?: string
}

// StateDot 仅有的四态中本卡取三态（StateDot aria-hidden，状态语义由
// STATUS_TEXT 可见文本承载）。
const DOT_STATE: Record<ToolCardStatus, StateDotState> = {
  RUNNING: 'ongoing',
  SUCCEEDED: 'done',
  FAILED: 'error',
}

const STATUS_TEXT: Record<ToolCardStatus, string> = {
  RUNNING: '运行中',
  SUCCEEDED: '已完成',
  FAILED: '失败',
}

// parseJson tolerates payloads that are not complete JSON (流式 delta 拼接中
// 的不完整参数): valid JSON pretty-prints as its value, other text falls
// through verbatim as a JSON string literal.
function parseJson(text: string): unknown {
  try {
    return JSON.parse(text) as unknown
  } catch {
    return text
  }
}

export function ToolCard({ toolId, name, argsJson, status, result }: ToolCardProps) {
  return (
    <div
      className="tool-card"
      data-testid="tool-card"
      data-tool-id={toolId}
      data-status={status}
    >
      <div className="tool-card-header">
        <StateDot state={DOT_STATE[status]} />
        <span className="tool-card-name" data-testid="tool-card-name">
          {name}
        </span>
        <span className="tool-card-state" data-testid="tool-card-state">
          {STATUS_TEXT[status]}
        </span>
        <span className="tool-card-id">{toolId}</span>
      </div>
      <div data-testid="tool-card-args">
        <JsonBlock label="参数" payload={parseJson(argsJson)} />
      </div>
      {result !== undefined && (
        <div data-testid="tool-card-result">
          <JsonBlock label="结果" payload={parseJson(result)} />
        </div>
      )}
    </div>
  )
}
