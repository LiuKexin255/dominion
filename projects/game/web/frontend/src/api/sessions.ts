// v1 session 管理 REST 客户端（既有 /api/v1 路由，零后端改动）。
// 字段形态为 protojson（grpc-gateway v2 默认 marshaler：camelCase 字段名、
// 枚举输出名字符串），对照 projects/game/game.proto 的 Session /
// ListSessionsResponse / CreateSessionRequest。全部相对路径调用——页面与
// API 经 game.liukexin.com 同主机名路径分流，零 CORS
// （specs/049-agent-v2-dsh-init/contracts/conversation-api.md §2）。
import { ApiError, requestJson } from './conversation.js'

// Session is the /api/v1 session resource (protojson projection of
// projects/game/game.proto Session): the template and session id live in the
// name path segments (AIP-124), create_time serializes as an RFC 3339 string.
export interface Session {
  name: string
  createTime?: string
}

// ListSessionsResponse per projects/game/game.proto (protojson omits absent
// fields, so both fields are optional).
export interface ListSessionsResponse {
  sessions?: Session[]
  nextPageToken?: string
}

export async function listSessions(template: string): Promise<Session[]> {
  const res = await requestJson<ListSessionsResponse>(
    `/api/v1/templates/${template}/sessions`,
  )
  return res.sessions ?? []
}

// createSession defers the session id to the server (empty session_id in
// CreateSessionRequest, projects/game/game.proto) and returns the created
// resource carrying its server-assigned name.
export async function createSession(template: string): Promise<Session> {
  return requestJson<Session>(`/api/v1/templates/${template}/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    // grpc-gateway binds the body to CreateSessionRequest.session (body:
    // "session"); an empty Session object is the whole request body.
    body: '{}',
  })
}

export async function deleteSession(name: string): Promise<void> {
  const res = await fetch(`/api/v1/${name}`, { method: 'DELETE' })
  if (!res.ok) throw new ApiError(res.status, await res.text())
}
