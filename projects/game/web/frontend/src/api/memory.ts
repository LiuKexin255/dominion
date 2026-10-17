// memory 管理 REST 客户端（preset 级 MemoryService 面，契约
// specs/071-memory-preset-scope/contracts/web-memory-view.md §2 与
// specs/071-memory-preset-scope/contracts/memory-service.md §2）。全部相对路径经
// requestJson/ApiError——同源 gateway，零 CORS（客户端模式延续
// specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2）。字段形态为
// protojson 投影（specs/071-memory-preset-scope/data-model.md §4）：camelCase
// 字段名、Timestamp 为 RFC 3339 字符串。
import { ApiError, requestJson } from './conversation.js'

// Memory is the protojson projection of projects/game/game.proto Memory: the
// template/preset/memory ids live in the name path segments
// (templates/{template}/presets/{preset}/memories/{memory}); the resource
// message carries no separate id field, the {memory} segment IS the memory id
// (specs/068-wiki-round-pause/contracts/memory-resource-identity.md §1).
// createTime/updateTime serialize as RFC 3339 strings.
export interface Memory {
  name: string
  content: string
  createTime?: string
  updateTime?: string
}

// ListMemoriesResponse per projects/game/game.proto (protojson omits absent
// fields, so both are optional).
export interface ListMemoriesResponse {
  memories?: Memory[]
  nextPageToken?: string
}

// One ListMemories page: the full resources plus the continuation token
// ('' = last page).
export interface MemoryPage {
  memories: Memory[]
  nextPageToken: string
}

// ListMemoriesOptions per AIP-158/AIP-132: undefined fields are not sent; an
// empty page token means the first page; orderBy is the order_by expression
// (whitelist memory_id/update_time, syntax rejected server-side with
// INVALID_ARGUMENT — specs/068-wiki-round-pause/contracts/
// memory-page-token-context.md).
export interface ListMemoriesOptions {
  pageSize?: number
  pageToken?: string
  orderBy?: string
}

// listQuery renders the query string for a List call: the proto pagination
// fields page_size/page_token (absent/empty values are omitted) followed by
// the order_by expression ('' renders no query string).
function listQuery(options: ListMemoriesOptions | undefined): string {
  const params = new URLSearchParams()
  if (options?.pageSize !== undefined) params.set('page_size', String(options.pageSize))
  if (options?.pageToken !== undefined && options.pageToken !== '') {
    params.set('page_token', options.pageToken)
  }
  if (options?.orderBy !== undefined && options.orderBy !== '') {
    params.set('order_by', options.orderBy)
  }
  const rendered = params.toString()
  return rendered === '' ? '' : `?${rendered}`
}

// listMemories GETs one page of a preset's memories (AIP-132; full resources,
// content included). The default order is the server's memory_id ascending —
// callers wanting recency pass orderBy="update_time desc" explicitly
// (specs/071-memory-preset-scope/contracts/web-memory-view.md §3).
export async function listMemories(
  template: string,
  preset: string,
  options?: ListMemoriesOptions,
): Promise<MemoryPage> {
  const res = await requestJson<ListMemoriesResponse>(
    `/api/v1/templates/${template}/presets/${preset}/memories${listQuery(options)}`,
  )
  return { memories: res.memories ?? [], nextPageToken: res.nextPageToken ?? '' }
}

// memoryIdForContent derives the create request's memory_id: the first 32 hex
// characters of sha256(content) over the UTF-8 bytes. This is the same rule as
// the memory tool's generateMemoryId (node createHash('sha256').digest('hex')
// .slice(0, 32) — common/js/memory/src/operations.ts), so
// web-created and agent-created entries share one deterministic id and
// same-content creates collide into the service's ALREADY_EXISTS dedupe
// (specs/071-memory-preset-scope/data-model.md §1.1/§2). The digest character
// set (hex) satisfies the service's [a-z0-9_-]+ memory_id rule. Uses Web
// Crypto, available in the browser secure context and the Node test
// environment
// (developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/digest).
//
// `content` is the exact value being stored: callers pass the trimmed text,
// mirroring the tool path's generateMemoryId(trimmed) (operations.ts applyAdd
// trims first) so the two paths stay on one id per content.
export async function memoryIdForContent(content: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(content))
  const hex = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
  return hex.slice(0, 32)
}

// createMemory creates a memories entry under a preset (AIP-133). memory_id is
// derived from the content by memoryIdForContent and travels as a query
// parameter (the path binds only parent); the body is the Memory message
// itself — the proto annotation binds body:"memory"
// (projects/game/game.proto CreateMemoryRequest), so the wire body is the flat
// {content}. Same-content creates are rejected with ALREADY_EXISTS (409).
export async function createMemory(
  template: string,
  preset: string,
  content: string,
): Promise<Memory> {
  const memoryId = await memoryIdForContent(content)
  return requestJson<Memory>(
    `/api/v1/templates/${template}/presets/${preset}/memories?memory_id=${encodeURIComponent(memoryId)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    },
  )
}

// updateMemory PATCHes the content (AIP-134). update_mask is passed as a query
// parameter (the proto binds body:"memory" to the path-bound {memory.name}, so
// the mask cannot travel in the body) and carries the sole mutable field
// content — Memory has no create-or-update, a missing target is NOT_FOUND.
// The body is the flat Memory message {name, content}; the path segment is the
// authoritative identity.
export async function updateMemory(name: string, content: string): Promise<Memory> {
  return requestJson<Memory>(`/api/v1/${name}?update_mask=content`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, content }),
  })
}

// deleteMemory DELETEs a Memory resource (AIP-135). The success response is an
// empty proto message, so the raw fetch path (not requestJson) is used — a
// JSON parse is not warranted (same shape as api/wiki.ts deleteWiki).
export async function deleteMemory(name: string): Promise<void> {
  const res = await fetch(`/api/v1/${name}`, { method: 'DELETE' })
  if (!res.ok) throw new ApiError(res.status, await res.text())
}
