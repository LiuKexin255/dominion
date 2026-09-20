// wiki 管理视图：template 级 wiki 的搜索、分页浏览、查看全文、创建/编辑与
// inline 二步删除（契约
// specs/069-wiki-template-web/contracts/web-wiki-view.md §3-4）。交互语汇沿用
// specs/054-agent-v2-bugfixes/contracts/web-ui.md §6：独占创建/编辑表单（编辑
// 期间列表不渲染）、inline 二步删除确认、loading/error/空态与错误呈现。视图为
// 纯前端 state（搜索词、分页 token、表单模式、删除确认），与 session/preset
// 视图互斥渲染。布局与表单样式复用 theme.css 的 presets-* 类（同一管理视图
// 形态的既有样式），标题用 Input、正文用原生 <textarea>——primitives 无专用
// Textarea，persona 编辑先例见 components/PresetsView.tsx。
import { useCallback, useEffect, useState } from 'react'
import { Button, Input } from '@deepseek-ai/dsh-client-ui-primitives'
import { ApiError } from '../api/conversation.js'
import {
  createWiki,
  deleteWiki,
  getWiki,
  listWikis,
  normalizeWikiTitle,
  searchWikis,
  updateWiki,
  wikiIdForTitle,
} from '../api/wiki.js'
import type { Wiki, WikiUpdate } from '../api/wiki.js'

interface WikiViewProps {
  template: string
}

// 每页条数：服务端默认值（projects/game/game.proto ListWikisRequest.page_size
// 缺省 20、上限 100）。
const WIKI_PAGE_SIZE = 20

// One rendered list row: ListWikis rows carry update_time, SearchWikis rows are
// the lightweight {name, title} summaries (no timestamp).
interface WikiListItem {
  name: string
  title: string
  updateTime?: string
}

type FormMode = { kind: 'closed' } | { kind: 'create' } | { kind: 'edit'; wiki: Wiki }

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// formatTime renders the protojson update_time (RFC 3339 string) in the
// browser locale; an unparseable value falls back to the raw string.
function formatTime(updateTime: string): string {
  const date = new Date(updateTime)
  return Number.isNaN(date.getTime()) ? updateTime : date.toLocaleString()
}

export function WikiView({ template }: WikiViewProps) {
  const [items, setItems] = useState<WikiListItem[]>([])
  const [loading, setLoading] = useState(true)
  // 非列表请求（getWiki/写入/删除）进行中：禁用操作入口，避免并发提交。
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // 已提交的搜索词（'' = 浏览全部，走 ListWikis）；keywordDraft 是输入框草稿，
  // 经搜索提交才生效。
  const [query, setQuery] = useState('')
  const [keywordDraft, setKeywordDraft] = useState('')
  // AIP-158 游标分页：pageTokens 是从首页到当前页的 token 链（首项 ''），
  // 末项为当前页 token；nextToken 是当前页的续页 token（'' = 末页）。
  const [pageTokens, setPageTokens] = useState<string[]>([''])
  const [nextToken, setNextToken] = useState('')
  // 列表刷新计数器：同一 query/page 上重复刷新也要重取。
  const [reloadKey, setReloadKey] = useState(0)
  const [form, setForm] = useState<FormMode>({ kind: 'closed' })
  const [titleDraft, setTitleDraft] = useState('')
  const [contentDraft, setContentDraft] = useState('')
  // 正在查看全文的条目（经 GetWiki 取完整 content）。
  const [detail, setDetail] = useState<Wiki | null>(null)
  // 待确认删除的 wiki 资源名（inline 二步提交）。
  const [deletePending, setDeletePending] = useState<string | null>(null)

  const currentToken = pageTokens[pageTokens.length - 1] ?? ''
  const onFirstPage = pageTokens.length <= 1
  const hasNextPage = nextToken !== ''

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      if (query === '') {
        const page = await listWikis(template, {
          pageSize: WIKI_PAGE_SIZE,
          pageToken: currentToken,
        })
        setItems(
          page.wikis.map((wiki) => ({
            name: wiki.name,
            title: wiki.title,
            updateTime: wiki.updateTime,
          })),
        )
        setNextToken(page.nextPageToken)
      } else {
        const page = await searchWikis(template, query, {
          pageSize: WIKI_PAGE_SIZE,
          pageToken: currentToken,
        })
        setItems(page.wikis.map((wiki) => ({ name: wiki.name, title: wiki.title })))
        setNextToken(page.nextPageToken)
      }
      setError(null)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }, [template, query, currentToken, reloadKey])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const submitSearch = useCallback(() => {
    setQuery(keywordDraft.trim())
    setPageTokens([''])
    setDeletePending(null)
    setError(null)
    setReloadKey((key) => key + 1)
  }, [keywordDraft])

  const goNext = useCallback(() => {
    if (nextToken === '') return
    setPageTokens((prev) => [...prev, nextToken])
    setDeletePending(null)
  }, [nextToken])

  const goPrev = useCallback(() => {
    setPageTokens((prev) => (prev.length > 1 ? prev.slice(0, -1) : prev))
    setDeletePending(null)
  }, [])

  const closeForm = useCallback(() => {
    setForm({ kind: 'closed' })
    setError(null)
  }, [])

  const openCreate = useCallback(() => {
    setForm({ kind: 'create' })
    setTitleDraft('')
    setContentDraft('')
    setDetail(null)
    setDeletePending(null)
    setError(null)
  }, [])

  const openEdit = useCallback((wiki: Wiki) => {
    setForm({ kind: 'edit', wiki })
    setTitleDraft(wiki.title)
    setContentDraft(wiki.content)
    setDetail(null)
    setDeletePending(null)
    setError(null)
  }, [])

  // 列表的「编辑」入口只有摘要（SearchWikis 不返回 content）：经 GetWiki 取
  // 完整资源后进入编辑表单。
  const startEdit = useCallback(
    async (name: string) => {
      setBusy(true)
      try {
        openEdit(await getWiki(name))
      } catch (err) {
        setError(errorMessage(err))
      } finally {
        setBusy(false)
      }
    },
    [openEdit],
  )

  // 查看：统一经 GetWiki 取完整 content（列表条目与搜索命中同路径——列表虽
  // 含 content，也以服务端最新值为准）。
  const openDetail = useCallback(async (name: string) => {
    setBusy(true)
    try {
      setDetail(await getWiki(name))
      setDeletePending(null)
      setError(null)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }, [])

  const save = useCallback(async () => {
    const title = titleDraft.trim()
    if (title === '') return
    setBusy(true)
    try {
      if (form.kind === 'create') {
        const created = await createWiki(
          template,
          await wikiIdForTitle(title),
          title,
          contentDraft,
        )
        closeForm()
        // 新建后确保条目可见：当前搜索词不匹配新标题时回到浏览列表（匹配为
        // 规范化子串，与服务端同规则——contracts/web-wiki-view.md §3.1/§3.2）。
        if (
          query !== '' &&
          !normalizeWikiTitle(created.title).includes(normalizeWikiTitle(query))
        ) {
          setQuery('')
          setKeywordDraft('')
          setPageTokens([''])
        } else {
          await refresh()
        }
      } else if (form.kind === 'edit') {
        // update_mask 只含实际改动字段（contracts/web-wiki-view.md §3.3）。
        const update: WikiUpdate = {}
        if (title !== form.wiki.title) update.title = title
        if (contentDraft !== form.wiki.content) update.content = contentDraft
        if (Object.keys(update).length === 0) {
          closeForm()
          return
        }
        await updateWiki(form.wiki.name, update)
        closeForm()
        await refresh()
      }
    } catch (err) {
      // 编辑目标被外部并发删除：呈现 404 并返回列表，不吞错误（契约 §4）。
      if (form.kind === 'edit' && err instanceof ApiError && err.status === 404) {
        setForm({ kind: 'closed' })
        await refresh()
        setError(errorMessage(err))
      } else {
        setError(errorMessage(err))
      }
    } finally {
      setBusy(false)
    }
  }, [form, titleDraft, contentDraft, query, template, refresh, closeForm])

  const confirmDelete = useCallback(
    async (name: string) => {
      setBusy(true)
      let failure: unknown = null
      try {
        await deleteWiki(name)
      } catch (err) {
        failure = err
      }
      setDeletePending(null)
      if (detail?.name === name) setDetail(null)
      setBusy(false)
      // 成功或并发删除（404）都以服务端列表为准重取；错误在刷新后呈现，
      // 避免被 refresh 的成功路径清除。
      await refresh()
      if (failure !== null) setError(errorMessage(failure))
    },
    [detail, refresh],
  )

  return (
    <div className="presets-view" data-testid="wiki-view">
      <div className="presets-header">
        <h2 className="presets-title">Wiki</h2>
        <div className="presets-actions">
          <Button
            data-testid="refresh-wikis"
            disabled={loading || busy}
            onClick={() => setReloadKey((key) => key + 1)}
          >
            刷新
          </Button>
          <Button
            variant="primary"
            data-testid="create-wiki"
            disabled={loading || busy || form.kind !== 'closed'}
            onClick={openCreate}
          >
            新建 wiki
          </Button>
        </div>
      </div>
      <p className="presets-hint">
        template 级知识库：同一 template 的所有 session 共享；标题按规范化子串匹配（大小写/全半角不敏感）。
      </p>
      {error !== null && (
        <div className="presets-error" data-testid="wiki-error" role="alert">
          {error}
        </div>
      )}

      {form.kind !== 'closed' && (
        <form
          className="preset-form"
          data-testid="wiki-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label className="preset-form-field" htmlFor="wiki-title-input">
            <span>标题（≤ 128 字符）</span>
            <Input
              id="wiki-title-input"
              data-testid="wiki-title-input"
              aria-label="wiki 标题"
              value={titleDraft}
              onChange={(e) => setTitleDraft(e.target.value)}
              placeholder="如 开局-左上角"
            />
          </label>
          <label className="preset-form-field" htmlFor="wiki-content-input">
            <span>正文（≤ 64KB）</span>
            <textarea
              id="wiki-content-input"
              className="preset-prompt-input"
              data-testid="wiki-content-input"
              aria-label="wiki 正文"
              rows={10}
              value={contentDraft}
              onChange={(e) => setContentDraft(e.target.value)}
            />
          </label>
          <div className="preset-form-actions">
            <Button
              variant="primary"
              data-testid="wiki-save"
              disabled={busy || titleDraft.trim() === ''}
              onClick={() => void save()}
            >
              保存
            </Button>
            <Button data-testid="wiki-cancel" disabled={busy} onClick={closeForm}>
              取消
            </Button>
          </div>
        </form>
      )}

      {form.kind === 'closed' && detail !== null && (
        <div className="preset-form" data-testid="wiki-detail">
          <div className="preset-item-title">
            <span className="preset-item-name" data-testid="wiki-detail-title">
              {detail.title}
            </span>
            {detail.updateTime !== undefined && (
              <span className="preset-item-time" data-testid="wiki-detail-time">
                {formatTime(detail.updateTime)}
              </span>
            )}
          </div>
          <pre className="preset-prompt-input" data-testid="wiki-detail-content">
            {detail.content}
          </pre>
          <div className="preset-form-actions">
            <Button
              variant="primary"
              data-testid="wiki-detail-edit"
              disabled={busy}
              onClick={() => openEdit(detail)}
            >
              编辑
            </Button>
            <Button
              data-testid="wiki-detail-close"
              disabled={busy}
              onClick={() => setDetail(null)}
            >
              返回列表
            </Button>
          </div>
        </div>
      )}

      {form.kind === 'closed' && detail === null && (
        <>
          <form
            className="presets-filter"
            data-testid="wiki-search-form"
            onSubmit={(e) => {
              e.preventDefault()
              submitSearch()
            }}
          >
            <label htmlFor="wiki-search-input">搜索标题</label>
            <Input
              id="wiki-search-input"
              data-testid="wiki-search-input"
              aria-label="搜索 wiki 标题"
              value={keywordDraft}
              onChange={(e) => setKeywordDraft(e.target.value)}
              placeholder="输入关键词（留空 = 浏览全部）"
            />
            <Button data-testid="wiki-search-submit" disabled={loading || busy} onClick={submitSearch}>
              搜索
            </Button>
          </form>

          {!loading && items.length === 0 && error === null ? (
            <div className="presets-empty" data-testid="wiki-empty">
              {query === ''
                ? '还没有 wiki 条目。新建一条把知识沉淀下来，planner 也能检索到。'
                : `没有匹配「${query}」的条目，换个关键词或浏览全部。`}
              <div className="presets-empty-actions">
                <Button
                  variant="primary"
                  data-testid="wiki-empty-create"
                  onClick={openCreate}
                >
                  新建 wiki
                </Button>
              </div>
            </div>
          ) : (
            <ul className="preset-items">
              {items.map((item) => (
                <li key={item.name} className="preset-item" data-testid="wiki-item">
                  <div className="preset-item-main">
                    <div className="preset-item-title">
                      <button
                        type="button"
                        className="preset-item-name"
                        data-testid="wiki-title"
                        onClick={() => void openDetail(item.name)}
                      >
                        {item.title}
                      </button>
                    </div>
                    {item.updateTime !== undefined && (
                      <span className="preset-item-time" data-testid="wiki-time">
                        {formatTime(item.updateTime)}
                      </span>
                    )}
                  </div>
                  <div className="preset-item-actions">
                    {deletePending === item.name ? (
                      <>
                        <span className="preset-delete-confirm">确认删除？</span>
                        <Button
                          variant="primary"
                          data-testid="wiki-delete-confirm"
                          disabled={busy}
                          onClick={() => void confirmDelete(item.name)}
                        >
                          确认
                        </Button>
                        <Button
                          data-testid="wiki-delete-cancel"
                          disabled={busy}
                          onClick={() => setDeletePending(null)}
                        >
                          取消
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button
                          data-testid="wiki-open"
                          disabled={busy}
                          onClick={() => void openDetail(item.name)}
                        >
                          查看
                        </Button>
                        <Button
                          data-testid="wiki-edit"
                          disabled={busy}
                          onClick={() => void startEdit(item.name)}
                        >
                          编辑
                        </Button>
                        <Button
                          data-testid="wiki-delete"
                          disabled={busy}
                          onClick={() => setDeletePending(item.name)}
                        >
                          删除
                        </Button>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}

          {!loading && (!onFirstPage || hasNextPage) && (
            <div className="presets-actions" data-testid="wiki-pager">
              <span className="presets-note" data-testid="wiki-page">
                第 {pageTokens.length} 页
              </span>
              <Button data-testid="wiki-prev" disabled={loading || busy || onFirstPage} onClick={goPrev}>
                上一页
              </Button>
              <Button data-testid="wiki-next" disabled={loading || busy || !hasNextPage} onClick={goNext}>
                下一页
              </Button>
            </div>
          )}
        </>
      )}
      {loading && <div className="presets-note">加载中…</div>}
    </div>
  )
}
