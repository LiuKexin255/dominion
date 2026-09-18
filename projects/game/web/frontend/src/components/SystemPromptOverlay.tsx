// system prompt 只读浮层（specs/059-agent-v2-team-mode/contracts/web-views.md
// §5）：对话页工具条成员清单与设置面板成员清单共用的取数与呈现逻辑。入口调用
// open(role)，经 GetTeamMember 读取该实例当前生效的完整装配结果（persona +
// team section + 工具守则 + [planner] 记忆快照；服务端从装配面取实际内容而
// 非另行拼装）。请求竞态以序号守卫；刷新 team（新物化快照 updateTime 变化）
// 或切换会话时收起——全文必须与当前实例一致，重新打开入口即取新值。
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import { getTeamMember } from '../api/agent.js'

// 一个控制器可被多处入口共享（App 对话页把同一实例传给工具条成员清单与设置
// 面板成员清单）：任一时刻至多一个浮层，后打开的入口替换先打开的内容。
export interface SystemPromptController {
  /** 已打开入口的成员 role；null = 浮层关闭。 */
  member: string | null
  /** 该成员的 system prompt 全文；null = 尚未取到。 */
  text: string | null
  /** 取数失败文案；null = 无错误。 */
  error: string | null
  loading: boolean
  open: (role: string) => void
  close: () => void
}

// useSystemPrompt 管理一个 session 的浮层状态；refreshKey 承载"当前物化
// 快照"标识（如 Team.updateTime）——变化即收起已打开的全文。
export function useSystemPrompt(
  session: string,
  refreshKey?: string,
): SystemPromptController {
  const [member, setMember] = useState<string | null>(null)
  const [text, setText] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const request = useRef(0)

  useEffect(() => {
    request.current += 1
    setMember(null)
    setText(null)
    setError(null)
    setLoading(false)
  }, [session, refreshKey])

  const open = useCallback(
    async (role: string) => {
      const id = ++request.current
      setMember(role)
      setText(null)
      setError(null)
      setLoading(true)
      try {
        const entry = await getTeamMember(session, role)
        if (request.current !== id) return
        setText(entry.systemPrompt ?? '')
      } catch (err) {
        if (request.current !== id) return
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (request.current === id) setLoading(false)
      }
    },
    [session],
  )

  const close = useCallback(() => {
    request.current += 1
    setMember(null)
    setText(null)
    setError(null)
    setLoading(false)
  }, [])

  return { member, text, error, loading, open, close }
}

// SystemPromptOverlay 呈现打开的浮层；className 供调用方追加定位上下文
// （对话页工具条下方 vs 设置面板内联）。
export function SystemPromptOverlay({
  controller,
  className,
}: {
  controller: SystemPromptController
  className?: string
}) {
  if (controller.member === null) return null
  return (
    <div
      className={className === undefined ? 'system-prompt' : `system-prompt ${className}`}
      data-testid="system-prompt"
      data-role={controller.member}
    >
      <div className="system-prompt-header">
        <span data-testid="system-prompt-title">
          {controller.member} 的 system prompt（只读）
        </span>
        <Button data-testid="system-prompt-close" onClick={controller.close}>
          关闭
        </Button>
      </div>
      {controller.loading && (
        <div className="system-prompt-note" data-testid="system-prompt-loading">
          加载中…
        </div>
      )}
      {controller.error !== null && (
        <div className="system-prompt-error" data-testid="system-prompt-error" role="alert">
          {controller.error}
        </div>
      )}
      {controller.text !== null && (
        // 只读全文（等宽/原文呈现，web-views.md §5）：<pre> 保留换行与
        // 空白，不做 markdown 解析。
        <pre className="system-prompt-text" data-testid="system-prompt-text">
          {controller.text}
        </pre>
      )}
    </div>
  )
}
