// /api/v2 对话 API 客户端（AgentService，契约
// specs/051-agent-v2-dsh-migration/contracts/agent-api.md）。sendStream 以
// fetch + ReadableStream 消费 NDJSON 事件流（EventSource 不适用——POST body）；
// listHistory 为 agent 单例（AIP-156）消息子资源的标准 List（AIP-132）。
// 全部类型为 protojson 投影：camelCase 字段名、枚举输出名字符串、oneof 展平
// 为可选字段——未知 oneof 分支被消费端忽略（proto3 forward-compat）。

// ─── 流事件（ChatEvent，conversation-api.md §1 protojson 投影） ──────────────

export type BlockType =
  | 'BLOCK_TYPE_UNSPECIFIED'
  | 'BLOCK_TYPE_TEXT'
  | 'BLOCK_TYPE_THINK'
  | 'BLOCK_TYPE_TOOL_CALL'

export type TurnStatus =
  | 'TURN_STATUS_UNSPECIFIED'
  | 'TURN_STATUS_COMPLETED'
  | 'TURN_STATUS_ERROR'
  | 'TURN_STATUS_ABORTED'

// ContentBlock is the terminal content block carried by block_end and
// history messages (oneof kind flattened to optional protojson fields).
export interface ContentBlock {
  text?: { content: string }
  think?: { content: string }
  toolCall?: {
    toolId: string
    name: string
    argsJson: string
    status: string
    result?: string
  }
}

export interface ChatEvent {
  session?: string
  turnId?: string
  queued?: { position: number }
  turnStart?: Record<string, never>
  blockStart?: {
    index: number
    type: BlockType
    toolId?: string
    name?: string
    // 块所属的模型输出步骤序号（turn 内从 0 单调递增，agent_v2.proto
    // BlockStartEvent；分组维度，index 仍为块序维度）。
    step?: number
  }
  delta?: { index: number; text: string; step?: number }
  blockEnd?: { index: number; block: ContentBlock; step?: number }
  turnEnd?: {
    status: TurnStatus
    error?: { code: string; message: string }
    usage?: { inputTokens?: string; outputTokens?: string; reasoningTokens?: string }
  }
  // One tool execution's terminal outcome (agent_v2.proto ToolResultEvent);
  // toolId joins back to the originating tool-call block (research D10).
  toolResult?: { toolId: string; status: string; result: string }
}

// ─── 历史（HistoryMessage，agent-api.md §1 ListAgentMessages） ───────────────

export type Role = 'ROLE_UNSPECIFIED' | 'ROLE_USER' | 'ROLE_AGENT'

export interface HistoryMessage {
  messageId?: string
  role: Role
  // protojson Timestamp: RFC 3339 string.
  createTime?: string
  blocks: ContentBlock[]
}

export interface ListAgentMessagesResponse {
  messages?: HistoryMessage[]
  nextPageToken?: string
}

// ─── 错误与请求设施 ──────────────────────────────────────────────────────────

// ApiError carries a request-level failure (stream never opened). The HTTP
// status follows grpc-gateway's gRPC code mapping (InvalidArgument→400,
// Unavailable→503, … — github.com/grpc-ecosystem/grpc-gateway
// runtime/errors.go @ v2.27.6 HTTPStatusFromCode).
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: string,
  ) {
    super(`api request failed: ${status} ${body}`)
    this.name = 'ApiError'
  }
}

export async function requestJson<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  if (!res.ok) throw new ApiError(res.status, await res.text())
  return (await res.json()) as T
}

// ndjsonLines reassembles one JSON value per line from a byte stream:
// buffered across chunk boundaries (half lines) and coalesced within a chunk
// (glued lines); decoder stream mode keeps multi-byte UTF-8 sequences intact
// across chunks (developer.mozilla.org/en-US/docs/Web/API/ReadableStream).
export async function* ndjsonLines(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<string> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    let nl = buf.indexOf('\n')
    while (nl >= 0) {
      yield buf.slice(0, nl)
      buf = buf.slice(nl + 1)
      nl = buf.indexOf('\n')
    }
  }
}

// sendStream posts one user message and yields the turn's ChatEvents until
// its turn ends (including queued waiting, FR-012). Framing per
// specs/049-agent-v2-dsh-init/contracts/conversation-api.md §6.
export async function* sendStream(
  session: string,
  text: string,
): AsyncGenerator<ChatEvent> {
  const res = await fetch(`/api/v2/${session}:send`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  })
  if (!res.ok || !res.body) throw new ApiError(res.status, await res.text())
  for await (const line of ndjsonLines(res.body)) {
    // grpc-gateway v2's default streaming marshaler wraps every message in a
    // "result" key — one {"result": <ChatEvent>} JSON object per line
    // (conversation-api.md §2; runtime/handler.go
    // handleForwardResponseServerStream at the repo-pinned v2.27.6).
    yield (JSON.parse(line) as { result: ChatEvent }).result
  }
}

// listHistory backfills the agent singleton's messages via the standard
// List method (AIP-132): parent is the agent resource name
// templates/{template}/sessions/{session}/agent.
export async function listHistory(session: string): Promise<HistoryMessage[]> {
  const res = await requestJson<ListAgentMessagesResponse>(
    `/api/v2/${session}/agent/messages`,
  )
  return res.messages ?? []
}
