// 侧栏 session 列表（最小集：Create + 选择；列表时间格式化与 Delete/Refresh
// 编排属 US4，specs/049-agent-v2-dsh-init/contracts/web-frontend.md §2/§3.2）。
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import type { Session } from '../api/sessions.js'

export interface SessionListProps {
  sessions: Session[]
  selected: string | null
  loading: boolean
  error: string | null
  onSelect: (name: string) => void
  onCreate: () => void
}

// sessionTitle projects the resource name to its display id segment
// (templates/{template}/sessions/{id} → {id}).
function sessionTitle(name: string): string {
  return name.split('/').pop() ?? name
}

export function SessionList({
  sessions,
  selected,
  loading,
  error,
  onSelect,
  onCreate,
}: SessionListProps) {
  return (
    <>
      <div className="sidebar-header">
        <h2 className="sidebar-title">Sessions</h2>
        <Button data-testid="create-session" onClick={onCreate}>
          新建
        </Button>
      </div>
      {loading && <div className="sidebar-note">加载中…</div>}
      {error !== null && <div className="sidebar-error">{error}</div>}
      <ul className="session-items">
        {sessions.map((s) => (
          <li key={s.name}>
            <button
              type="button"
              data-testid="session-item"
              className={
                s.name === selected ? 'session-item selected' : 'session-item'
              }
              onClick={() => onSelect(s.name)}
            >
              <span>{sessionTitle(s.name)}</span>
              {s.createTime !== undefined && (
                <span className="session-time">{s.createTime}</span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </>
  )
}
