// 侧栏 session 列表：图标按钮（新建=加号/刷新=圆环箭头，FR-002）、每条目
// 右侧 `···` 操作菜单（删除带确认，不依赖选中态，FR-003）、长名渐隐 +
// 悬停滚动复位（FR-004）——契约
// specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §1。删除的
// /api/v1 编排在 App 层执行，本组件仅回调选中资源名并等待其 Promise 完成
// 以驱动"该条目删除进行中"的条目级禁用。
import { useState } from 'react'
import {
  Button,
  IconEllipsisOutline16,
  IconPlusOutline16,
  IconRefreshOutline14,
  Menu,
} from '@deepseek-ai/dsh-client-ui-primitives'
import type { MenuEntry } from '@deepseek-ai/dsh-client-ui-primitives'
import type { Session } from '../api/sessions.js'

export interface SessionListProps {
  sessions: Session[]
  selected: string | null
  loading: boolean
  error: string | null
  onSelect: (name: string) => void
  onRefresh: () => void
  onCreate: () => void
  onDelete: (name: string) => void | Promise<void>
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

// 图标按钮的 tooltip 用原生 title 而非 primitives 的 Tooltip：后者经
// cloneElement 向锚点注入 ref（node_modules/.pnpm/
// @deepseek-ai+dsh-client-ui-primitives@0.1.1-rc.2_*/
// node_modules/@deepseek-ai/dsh-client-ui-primitives/lib/types/Tooltip.d.ts），
// 而 0.1.1-rc.2 的 Button 是不转发 ref 的函数组件（React 18 函数组件不接受
// ref prop），锚点 ref 恒为 null、气泡无法定位；title + aria-label 以单元素
// 承载两个语义。
function IconButton(props: {
  icon: 'plus' | 'refresh' | 'ellipsis'
  label: string
  testId: string
  disabled?: boolean
  onClick: () => void
}) {
  const glyph =
    props.icon === 'plus' ? (
      <IconPlusOutline16 />
    ) : props.icon === 'refresh' ? (
      <IconRefreshOutline14 />
    ) : (
      <IconEllipsisOutline16 />
    )
  return (
    <Button
      size="sm"
      icon={glyph}
      aria-label={props.label}
      title={props.label}
      data-testid={props.testId}
      disabled={props.disabled}
      onClick={props.onClick}
    />
  )
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
  // `···` 菜单当前打开的条目（资源名；null = 全部关闭，同侧栏至多一个菜单）。
  const [menuFor, setMenuFor] = useState<string | null>(null)
  // 已进入确认步的条目：菜单内"删除"点击一次仅进入确认，再次确认才回调。
  const [armedFor, setArmedFor] = useState<string | null>(null)
  // 删除进行中的条目：其菜单删除项禁用（FR-003），其余条目不受影响。
  const [deleting, setDeleting] = useState<string | null>(null)
  // 悬停中的条目：名称容器切换 scrollable 类（theme.css .session-name
  // 由渐隐遮罩切换为可横向滚动；类驱动使悬停态在 jsdom 下可断言）。
  const [hovered, setHovered] = useState<string | null>(null)

  // 执行删除：deleting 置位后无论 onDelete 同步抛错、返回 rejected
  // promise 还是正常完成，都必然清理（async 包裹使同步抛错与 rejection
  // 同路被吞，失败呈现由 App 层负责——本组件仅承载条目级禁用态，
  // FR-003 specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §1）。
  const runDelete = (name: string) => {
    setMenuFor(null)
    setArmedFor(null)
    setDeleting(name)
    void (async () => {
      try {
        await onDelete(name)
      } catch {
        // 失败路径仅清理状态；错误呈现与重试由 App 层错误区承载。
      } finally {
        setDeleting((current) => (current === name ? null : current))
      }
    })()
  }

  // 菜单项：确认步未进入时仅"删除"；确认步呈现"确认删除/取消"。
  const menuEntries = (name: string): MenuEntry[] =>
    armedFor === name
      ? [
          {
            id: 'confirm-delete',
            label: '确认删除',
            danger: true,
            disabled: deleting === name,
          },
          { id: 'cancel-delete', label: '取消' },
        ]
      : [{ id: 'delete', label: '删除', danger: true, disabled: deleting === name }]

  const onMenuSelect = (name: string, id: string) => {
    if (id === 'delete') setArmedFor(name)
    else if (id === 'confirm-delete') runDelete(name)
    else if (id === 'cancel-delete') setArmedFor(null)
  }

  return (
    <>
      <div className="sidebar-header">
        <h2 className="sidebar-title" data-testid="sidebar-title">
          Sessions ({sessions.length})
        </h2>
        <div className="sidebar-actions">
          <IconButton
            icon="refresh"
            label="刷新"
            testId="refresh-sessions"
            disabled={loading}
            onClick={onRefresh}
          />
          <IconButton
            icon="plus"
            label="新建会话"
            testId="create-session"
            disabled={loading}
            onClick={onCreate}
          />
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
          <li key={s.name} className="session-entry">
            {/* 名称与时间纵向堆叠（desktop 基线为横排两端布局）：侧栏为
                260px 窄列，横排会截断较长的格式化时间。 */}
            <button
              type="button"
              data-testid="session-item"
              className={
                s.name === selected ? 'session-item selected' : 'session-item'
              }
              onClick={() => onSelect(s.name)}
              onMouseEnter={() => setHovered(s.name)}
              onMouseLeave={(e) => {
                setHovered(null)
                // FR-004 复位面：指针移出条目时名称容器 scrollLeft 归零
                // （悬停可滚动态见 theme.css .session-name.scrollable）。
                const name = e.currentTarget.querySelector<HTMLElement>(
                  '.session-name',
                )
                if (name !== null) name.scrollLeft = 0
              }}
            >
              <span
                className={
                  hovered === s.name
                    ? 'session-name scrollable'
                    : 'session-name'
                }
                data-testid="session-name"
              >
                {sessionTitle(s.name)}
              </span>
              {s.createTime !== undefined && (
                <span className="session-time" data-testid="session-time">
                  {formatTime(s.createTime)}
                </span>
              )}
            </button>
            <Menu
              open={menuFor === s.name}
              align="end"
              // portal 模式（契约 specs/051-agent-v2-dsh-migration/
              // contracts/web-frontend.md §1 `···` 菜单行）：列表挂
              // document.body 定位，规避 .session-items overflow-y:auto
              // 滚动容器对 in-place 列表的裁剪——底部条目的菜单整体超出
              // ul 底边被裁掉、"删除"入口不可用；primitives Menu.d.ts
              // portal 属性文档原文 "Use when an ancestor's overflow
              // clipping would crop the in-place list"。
              portal
              anchor={
                <IconButton
                  icon="ellipsis"
                  label="会话操作"
                  testId="session-actions"
                  onClick={() =>
                    setMenuFor((current) => (current === s.name ? null : s.name))
                  }
                />
              }
              items={menuEntries(s.name)}
              onSelect={(id) => onMenuSelect(s.name, id)}
              onClose={() => {
                setMenuFor(null)
                setArmedFor(null)
              }}
            />
          </li>
        ))}
      </ul>
    </>
  )
}
