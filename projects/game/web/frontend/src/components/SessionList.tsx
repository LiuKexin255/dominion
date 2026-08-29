// 侧栏 session 列表：列表（名称+创建时间）、Refresh/Create/Delete、选中态与
// 切换回调（specs/049-agent-v2-dsh-init/contracts/web-frontend.md §2/§3.2，
// 行为基线 projects/game/desktop/frontend/src/components/SessionList.svelte）。
// Delete 的 /api/v1→/api/v2 编排在 App 层执行，本组件仅回调选中资源名。
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import type { Session } from '../api/sessions.js'

export interface SessionListProps {
  sessions: Session[]
  selected: string | null
  loading: boolean
  error: string | null
  onSelect: (name: string) => void
  onRefresh: () => void
  onCreate: () => void
  onDelete: (name: string) => void
}

// sessionTitle projects the resource name to its display id segment
// (templates/{template}/sessions/{id} → {id}).
function sessionTitle(name: string): string {
  return name.split('/').pop() ?? name
}

// formatTime renders the protojson create_time (RFC 3339 string) in the
// browser locale, matching the desktop SessionList 呈现形态; an unparseable
// value falls back to the raw string instead of "Invalid Date".
function formatTime(createTime: string): string {
  const date = new Date(createTime)
  return Number.isNaN(date.getTime()) ? createTime : date.toLocaleString()
}

export function SessionList({
  sessions,
  selected,
  loading,
  error,
  onSelect,
  onRefresh,
  onCreate,
  onDelete,
}: SessionListProps) {
  return (
    <>
      <div className="sidebar-header">
        <h2 className="sidebar-title">Sessions ({sessions.length})</h2>
        <div className="sidebar-actions">
          <Button
            data-testid="refresh-sessions"
            disabled={loading}
            onClick={onRefresh}
          >
            刷新
          </Button>
          <Button
            data-testid="create-session"
            disabled={loading}
            onClick={onCreate}
          >
            新建
          </Button>
          <Button
            data-testid="delete-session"
            disabled={loading || selected === null}
            onClick={() => {
              if (selected !== null) onDelete(selected)
            }}
          >
            删除
          </Button>
        </div>
      </div>
      {loading && <div className="sidebar-note">加载中…</div>}
      {/* 错误与列表并存（desktop 基线为错误替换整个列表）：刷新/删除失败时
          不隐藏既有 session，保留列表可继续选择与操作。 */}
      {error !== null && (
        <div className="sidebar-error" data-testid="session-error">
          {error}
        </div>
      )}
      {!loading && sessions.length === 0 && error === null && (
        <div className="sidebar-note">暂无 session，点击新建创建一个</div>
      )}
      <ul className="session-items">
        {sessions.map((s) => (
          <li key={s.name}>
            {/* 名称与时间纵向堆叠（desktop 基线为横排两端布局）：侧栏为
                260px 窄列，横排会截断较长的格式化时间。 */}
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
                <span className="session-time" data-testid="session-time">
                  {formatTime(s.createTime)}
                </span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </>
  )
}
