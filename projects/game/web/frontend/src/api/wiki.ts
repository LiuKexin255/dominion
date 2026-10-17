// wiki 管理 REST 客户端（template 级 WikiService 面，契约
// specs/069-wiki-template-web/contracts/web-wiki-view.md §2 与
// specs/069-wiki-template-web/contracts/wiki-service.md §2）。全部相对路径经
// requestJson/ApiError——同源 gateway，零 CORS（客户端模式延续
// specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2）。字段形态为
// protojson 投影（specs/069-wiki-template-web/data-model.md §4）：camelCase
// 字段名、Timestamp 为 RFC 3339 字符串。
import { ApiError, requestJson } from './conversation.js'

// Wiki is the protojson projection of projects/game/game.proto Wiki: the
// template and wiki id live in the name path segments
// (templates/{template}/wikis/{wiki}), create_time/update_time serialize as
// RFC 3339 strings.
export interface Wiki {
  name: string
  title: string
  content: string
  createTime?: string
  updateTime?: string
}

// WikiSummary is one SearchWikis hit: the lightweight {name, title} pair;
// search never returns content
// (specs/069-wiki-template-web/contracts/wiki-service.md §2/§4).
export interface WikiSummary {
  name: string
  title: string
}

// ListWikisResponse / SearchWikisResponse per projects/game/game.proto
// (protojson omits absent fields, so both are optional).
export interface ListWikisResponse {
  wikis?: Wiki[]
  nextPageToken?: string
}

export interface SearchWikisResponse {
  wikis?: WikiSummary[]
  nextPageToken?: string
}

// One ListWikis page: the full resources plus the continuation token
// ('' = last page).
export interface ListWikisPage {
  wikis: Wiki[]
  nextPageToken: string
}

// One SearchWikis page: the lightweight hits plus the continuation token
// ('' = last page).
export interface SearchWikisPage {
  wikis: WikiSummary[]
  nextPageToken: string
}

// ListWikisOptions / SearchWikisOptions per AIP-158 (undefined fields are not
// sent; an empty page token means the first page).
export interface ListWikisOptions {
  pageSize?: number
  pageToken?: string
}

export interface SearchWikisOptions {
  pageSize?: number
  pageToken?: string
}

// WikiUpdate carries the mutable fields for UpdateWiki; the caller-provided
// fields become the update_mask paths
// (specs/069-wiki-template-web/contracts/wiki-service.md §2).
export interface WikiUpdate {
  title?: string
  content?: string
}

// pageQuery renders the query string for a List/Search call: an optional
// `query` first, then the proto pagination fields page_size/page_token
// (absent/empty values are omitted; '' renders no query string).
function pageQuery(
  options: ListWikisOptions | SearchWikisOptions | undefined,
  query?: string,
): string {
  const params = new URLSearchParams()
  if (query !== undefined) params.set('query', query)
  if (options?.pageSize !== undefined) params.set('page_size', String(options.pageSize))
  if (options?.pageToken !== undefined && options.pageToken !== '') {
    params.set('page_token', options.pageToken)
  }
  const rendered = params.toString()
  return rendered === '' ? '' : `?${rendered}`
}

// listWikis GETs one page of the template's wikis (AIP-132; full resources,
// content included). There is no order_by: the service returns wiki_id
// ascending (specs/069-wiki-template-web/contracts/wiki-service.md §2).
export async function listWikis(
  template: string,
  options?: ListWikisOptions,
): Promise<ListWikisPage> {
  const res = await requestJson<ListWikisResponse>(
    `/api/v1/templates/${template}/wikis${pageQuery(options)}`,
  )
  return { wikis: res.wikis ?? [], nextPageToken: res.nextPageToken ?? '' }
}

// searchWikis GETs one page of normalized-title substring hits (AIP-136
// custom method); the query is required and must be non-empty
// (specs/069-wiki-template-web/contracts/wiki-service.md §4).
export async function searchWikis(
  template: string,
  query: string,
  options?: SearchWikisOptions,
): Promise<SearchWikisPage> {
  const res = await requestJson<SearchWikisResponse>(
    `/api/v1/templates/${template}/wikis:search${pageQuery(options, query)}`,
  )
  return { wikis: res.wikis ?? [], nextPageToken: res.nextPageToken ?? '' }
}

// createWiki creates a wikis entry under a template (AIP-133). wiki_id is
// derived from the normalized title by wikiIdForTitle and travels as a query
// parameter (the path binds only parent); the body is the Wiki resource
// (projects/game/game.proto body: "wiki"). A title that normalizes to an
// existing one is rejected with ALREADY_EXISTS
// (specs/069-wiki-template-web/contracts/wiki-service.md §2/§3).
export async function createWiki(
  template: string,
  wikiId: string,
  title: string,
  content: string,
): Promise<Wiki> {
  return requestJson<Wiki>(
    `/api/v1/templates/${template}/wikis?wiki_id=${encodeURIComponent(wikiId)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title, content }),
    },
  )
}

// getWiki GETs one wiki by resource name (AIP-131) — the read path that
// returns the full content.
export async function getWiki(name: string): Promise<Wiki> {
  return requestJson<Wiki>(`/api/v1/${name}`)
}

// updateWiki PATCHes the mutable fields (AIP-134). update_mask is passed as a
// query parameter (projects/game/game.proto binds the body to wiki.name via
// {wiki.name=...}, so the mask cannot travel in the body) and is built from
// exactly the fields the caller supplies.
export async function updateWiki(name: string, update: WikiUpdate): Promise<Wiki> {
  const body: WikiUpdate = {}
  const mask: string[] = []
  if (update.title !== undefined) {
    body.title = update.title
    mask.push('title')
  }
  if (update.content !== undefined) {
    body.content = update.content
    mask.push('content')
  }
  return requestJson<Wiki>(`/api/v1/${name}?update_mask=${mask.join(',')}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function deleteWiki(name: string): Promise<void> {
  const res = await fetch(`/api/v1/${name}`, { method: 'DELETE' })
  if (!res.ok) throw new ApiError(res.status, await res.text())
}

// normalizeWikiTitle is the JS twin of the service's NormalizeTitle: NFKC
// Unicode normalization then lower-casing
// (specs/069-wiki-template-web/data-model.md §4; same rule as
// common/js/memory/src/wiki-operations.ts).
export function normalizeWikiTitle(title: string): string {
  return title.normalize('NFKC').toLowerCase()
}

// wikiIdForTitle derives the create request's wiki_id: the first 32 hex
// characters of sha256(title_norm) — the same rule the service and the wiki
// tools use (specs/069-wiki-template-web/data-model.md §4), so web-created and
// agent-created entries share one deterministic id. Uses Web Crypto, available
// in the browser secure context and the Node test environment
// (developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/digest).
export async function wikiIdForTitle(title: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(normalizeWikiTitle(title)),
  )
  const hex = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
  return hex.slice(0, 32)
}
