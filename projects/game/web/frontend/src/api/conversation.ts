// /api/v2 对话 API 客户端（team 面，契约
// specs/059-agent-v2-team-mode/contracts/team-api.md）。sendStream 以 fetch +
// ReadableStream 消费 team 流 NDJSON（EventSource 不适用——POST body）；流
// 从发起持续至 team 静止，承载成员事件帧与 team_message/member_view 帧
// （team-api.md §3.1/§3.2；member_view 增量修订见
// specs/060-agent-v2-team-optimize/contracts/team-api.md §2）。全部类型为
// protojson 投影：camelCase 字段名、枚举输出名字符串、oneof 展平为可选
// 字段——未知 oneof 分支被消费端忽略（proto3 forward-compat）。

// ─── 流事件（ChatEvent，team-api.md §3.2 protojson 投影） ───────────────────

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
  // 用户经 :cancel 终止（agent_v2.proto TurnStatus；specs/054-agent-v2-
  // bugfixes/data-model.md §1.2）。
  | 'TURN_STATUS_CANCELED'

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

// USER_MEMBER is the reserved member value naming user input in the merged
// team sequence and in member-view sender annotations (team-api.md §3.2/§5:
// member/sender are plain wire strings — "user" for user input, the member
// role string for member output; no enum prefix normalization).
export const USER_MEMBER = 'user'

// TeamMessage is one merged-sequence element: the ListTeamMessages response
// item and the team_message frame payload are the same shape with the same
// seq source (team-api.md §3.2, data-model.md §2). member is the producer
// string (USER_MEMBER for user input, the member role string otherwise).
// seq (int64) is emitted by protojson as a JSON string — normalize with seqOf
// before ordering/comparing.
export interface TeamMessage {
  member?: string
  message: HistoryMessage
  seq: number | string
}

// seqOf normalizes a protojson int64 (JSON string) to a number for ordering
// and dedup anchors.
export function seqOf(seq: number | string | undefined): number {
  return typeof seq === 'number' ? seq : Number(seq ?? 0)
}

export interface ChatEvent {
  session?: string
  turnId?: string
  queued?: { position: number }
  // Member annotation of a member event frame: the producing member's role
  // string (scenario vocabulary, saolei: "player"/"planner"; absent/empty on
  // team-level frames). Member event frames group by (member, turnId), block
  // index/step scoped to the member turn (team-api.md §3.2).
  member?: string
  // Team-level merged-sequence frame (team-api.md §3.2): {member, message,
  // seq} identical to a ListTeamMessages element; no outer member field.
  teamMessage?: TeamMessage
  // Team-level member-view frame (specs/060-agent-v2-team-optimize/contracts/
  // team-api.md §2): {member, sender, message} — a member consumed an input
  // (user input or a broadcast relay) and it entered that member's view; no
  // outer member field. message is the same projection ListMemberMessages
  // serves for the entry (ROLE_USER, sender annotated by the sender field);
  // messageId is the client-side idempotency anchor.
  memberView?: { member?: string; sender?: string; message: HistoryMessage }
  turnStart?: Record<string, never>
  blockStart?: {
    index: number
    type: BlockType
    toolId?: string
    name?: string
    // 块所属的模型输出步骤序号（turn 内从 1 单调递增，服务端 step 循环
    // 序号；0 为缺失哨兵，agent_v2.proto BlockStartEvent；分组维度，
    // index 仍为块序维度）。
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

// ─── 历史（HistoryMessage，team-api.md §5） ──────────────────────────────────

export type Role = 'ROLE_UNSPECIFIED' | 'ROLE_USER' | 'ROLE_AGENT'

export interface HistoryMessage {
  messageId?: string
  role: Role
  // protojson Timestamp: RFC 3339 string.
  createTime?: string
  blocks: ContentBlock[]
  // True when this assistant step's content is an interrupted prefix (the
  // LLM stream failed or was cancelled before the step settled): the step
  // carries no final answer, so the web folding check excludes the message
  // (specs/054-agent-v2-bugfixes/data-model.md §1.5). Absent for settled
  // steps and user messages (proto3 default-omitted).
  interrupted?: boolean
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

// sendStream posts one user message and opens the team stream: the NDJSON
// stream carries every member event frame and team_message frame from the
// subscription point until the team goes quiescent (team-api.md §3.1) —
// framing per the grpc-gateway v2 streaming envelope, one {"result": …} JSON
// object per line (runtime/handler.go handleForwardResponseServerStream at
// the repo-pinned v2.27.6).
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
    yield (JSON.parse(line) as { result: ChatEvent }).result
  }
}
