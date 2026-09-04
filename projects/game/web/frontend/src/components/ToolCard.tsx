// 工具调用卡片（props 契约 specs/049-agent-v2-dsh-init/contracts/
// web-frontend.md §3.2 ToolCard）：名称 / 参数（JsonBlock 默认折叠）/ 状态
// （StateDot：RUNNING→ongoing、SUCCEEDED→done、FAILED→error）/ result 关联
// 展示于同一卡片。result 为预格式化等宽文本直接呈现（specs/
// 054-agent-v2-bugfixes/contracts/web-ui.md §3：坐标标尺对齐、不 markdown
// 化、不以 JSON 字符串字面量形态呈现——文本棋盘逐字符原样，<pre> 保持
// 空白与换行）；参数区仍是 JSON（流式拼接中的不完整参数经 parseJson 容错）。
// 单卡形态参照 dsh-web ToolCallTree（剥离 renderSlot/
// 子调用树，attribution 与来源链接见包 README）：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx
// 卡内布局另参照同上游的单卡形态 GenericToolCard：
// https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/toolviews/GenericToolCard.tsx
import { JsonBlock, StateDot } from '@deepseek-ai/dsh-client-ui-primitives'
import type { StateDotState } from '@deepseek-ai/dsh-client-ui-primitives'

export type ToolCardStatus = 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'INTERRUPTED'

export interface ToolCardProps {
  toolId: string
  name: string
  argsJson: string
  status: ToolCardStatus
  result?: string
}

// StateDot 仅有的四态中本卡取四态（StateDot aria-hidden，状态语义由
// STATUS_TEXT 可见文本承载）；INTERRUPTED 取警示点——中断非失败（工具未
// 执行完，specs/054-agent-v2-bugfixes/data-model.md §2）。
const DOT_STATE: Record<ToolCardStatus, StateDotState> = {
  RUNNING: 'ongoing',
  SUCCEEDED: 'done',
  FAILED: 'error',
  INTERRUPTED: 'warning',
}

const STATUS_TEXT: Record<ToolCardStatus, string> = {
  RUNNING: '运行中',
  SUCCEEDED: '已完成',
  FAILED: '失败',
  INTERRUPTED: '已中断',
}

// parseJson tolerates payloads that are not complete JSON (流式 delta 拼接中
// 的不完整参数): valid JSON pretty-prints as its value, other text falls
// through verbatim as a JSON string literal. 仅参数区消费（结果区为
// 预格式化原文呈现，不经 JSON 处理）。
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
          <pre className="tool-card-result-pre" data-testid="tool-card-result-pre">
            {result}
          </pre>
        </div>
      )}
    </div>
  )
}
