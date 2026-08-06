<script lang="ts">
  import { tick } from 'svelte'
  import ChatMessage from './ChatMessage.svelte'
  import { MessageRole, messagePartKind, classifyToolResultStatus } from '../api'
  import type { MessagePart, ImagePart, ToolResultPart } from '../api'
  import { renderMarkdown } from '../markdown'

  type ChatEntry = {
    messageId: string
    role: MessageRole
    timestamp: string
    // D12: team agent name (replaces the former agentProfileName). Each tab
    // shows one agent's bucket, so this labels the agent run (FR-025).
    agent?: string
    parts?: MessagePart[]
    warnMessage?: string
  }

  // A flattened render item. Tool call + result collapse into ONE 'tool' item
  // keyed by tool_id (spec 023 FR-007 / data-model.md §5): a tool_call creates
  // the bubble; a later tool_result with the same tool_id merges into it in
  // place (no new entry). text/thinking/image keep their own items. A pending
  // (queued, not-yet-consumed) user message is flagged via the `pending`
  // field on its 'part' items so it can be visually marked
  // (specs/030-queued-chat-input/spec.md FR-008). NOTE: per the user decision,
  // specs/030-queued-chat-input/spec.md FR-010 (removing a queued message)
  // is NOT implemented in this feature —
  // once a message enters the queue it cannot be deleted: the queue lives in
  // the backend TurnLoop buffer and the inbound desktop→agent channel defines
  // no remove semantics
  // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §1).
  type ToolItem = {
    kind: 'tool'
    key: string
    messageId: string
    role: MessageRole
    timestamp: string
    agent?: string
    toolId?: string
    name?: string
    argsJson?: string
    result?: ToolResultPart
  }
  type RenderItem =
    | { kind: 'warn'; key: string; messageId: string; timestamp: string; message: string }
    | { kind: 'profile'; key: string; profile: string }
    | { kind: 'part'; key: string; messageId: string; role: MessageRole; timestamp: string; part: MessagePart; pending?: boolean }
    | ToolItem

  let {
    messages,
    processing = false,
    queueCount = 0,
    pendingMessageIds = [],
    loadingMessages = false,
    messagesError = null,
    onSend,
    onZoom = () => {},
    pendingScreenshot = null,
    onRemoveScreenshot = () => {},
    inputEnabled = true,
  }: {
    messages: ChatEntry[]
    processing?: boolean
    queueCount?: number
    pendingMessageIds?: string[]
    loadingMessages?: boolean
    messagesError?: string | null
    onSend: (text: string) => void
    onZoom?: (url: string) => void
    pendingScreenshot?: { dataUrl: string; widthPx: number; heightPx: number } | null
    onRemoveScreenshot?: () => void
    // FR-032: false for agents that do not accept user input (saolei:
    // planner) — the tab becomes an observe-only view.
    inputEnabled?: boolean
  } = $props()

  let inputText = $state('')
  let scrollContainer: HTMLDivElement | undefined = $state()

  // TOLERANCE for the at-bottom test (px); absorbs sub-pixel float jitter so
  // "at the bottom" is stable across browsers. Mirrors ChatMessage.svelte's
  // thinking-bubble follow (specs/027-chat-bubble-game-state/data-model.md §6;
  // contracts/desktop-bubble-render-contract.md §2).
  const TOLERANCE = 8

  // stickToBottom is the sticky-scroll gate: TRUE = follow new content down to
  // the bottom; FALSE (the operator scrolled up to read history) = pause — the
  // thread must not be yanked back down by streaming output. Set by
  // handleThreadScroll on every scroll; consulted by the auto-scroll
  // $effect.pre below. Starts TRUE so a session entry with history lands on
  // the latest messages.
  let stickToBottom = $state(true)

  // handleThreadScroll keeps stickToBottom in sync with the operator's
  // position: scrolling to/near the bottom re-arms following; scrolling up
  // (to read history) disarms it until the operator returns to the bottom.
  function handleThreadScroll() {
    const el = scrollContainer
    if (!el) return
    const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - TOLERANCE
    stickToBottom = atBottom
  }

  // pendingIdSet is the O(1) lookup over the frontend-tracked pending message
  // ids (App.svelte owns the FIFO list; the backend QueueSignal drives the
  // count — specs/030-queued-chat-input/contracts/queue-channel-contract.md §5).
  const pendingIdSet = $derived(new Set(pendingMessageIds))

  // renderItems flattens the chat entries into an ordered render list, merging
  // a tool_call and its later tool_result (same tool_id) into one evolving
  // bubble. The merge is recomputed reactively as messages arrive
  // (data-model.md §5).
  const renderItems = $derived.by<RenderItem[]>(() => {
    const items: RenderItem[] = []
    const toolByKey = new Map<string, ToolItem>()
    let lastAgent: string | undefined = undefined
    for (const msg of messages) {
      if (msg.warnMessage != null) {
        items.push({ kind: 'warn', key: msg.messageId, messageId: msg.messageId, timestamp: msg.timestamp, message: msg.warnMessage })
        continue
      }
      const parts = msg.parts ?? []
      if (parts.length === 0) continue
      // Agent label: show once per consecutive agent run when the agent
      // changes (D12 — the entry's agent name).
      if (msg.role === MessageRole.AGENT && msg.agent && msg.agent !== lastAgent) {
        items.push({ kind: 'profile', key: msg.messageId + '-profile', profile: msg.agent })
        lastAgent = msg.agent
      } else if (msg.role !== MessageRole.AGENT) {
        lastAgent = undefined
      }
      const isPending = msg.role === MessageRole.USER && pendingIdSet.has(msg.messageId)
      for (const part of parts) {
        const k = messagePartKind(part)
        if (k === 'toolCall') {
          const tc = part.toolCall!
          const key = 'tool-' + (tc.toolId ?? crypto.randomUUID())
          const existing = tc.toolId ? toolByKey.get(tc.toolId) : undefined
          if (existing) {
            existing.name = tc.name
            existing.argsJson = tc.argsJson
          } else {
            const item: ToolItem = {
              kind: 'tool',
              key,
              messageId: msg.messageId,
              role: msg.role,
              timestamp: msg.timestamp,
              agent: msg.agent,
              toolId: tc.toolId,
              name: tc.name,
              argsJson: tc.argsJson,
            }
            items.push(item)
            if (tc.toolId) toolByKey.set(tc.toolId, item)
          }
        } else if (k === 'toolResult') {
          const tr = part.toolResult!
          const target = tr.toolId ? toolByKey.get(tr.toolId) : undefined
          if (target) {
            target.result = tr
          } else {
            // Result with no preceding call: create a result-only bubble.
            const key = 'tool-' + (tr.toolId ?? crypto.randomUUID())
            const item: ToolItem = {
              kind: 'tool',
              key,
              messageId: msg.messageId,
              role: msg.role,
              timestamp: msg.timestamp,
              agent: msg.agent,
              toolId: tr.toolId,
              result: tr,
            }
            items.push(item)
            if (tr.toolId) toolByKey.set(tr.toolId, item)
          }
        } else {
          items.push({ kind: 'part', key: msg.messageId + '-' + items.length, messageId: msg.messageId, role: msg.role, timestamp: msg.timestamp, part, pending: isPending || undefined })
        }
      }
    }
    return items
  })

  function handleSend() {
    if (!inputEnabled) return
    const text = inputText.trim()
    if (!text && !pendingScreenshot) return
    onSend(text)
    inputText = ''
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!inputEnabled) return
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  function isAgentRole(role: MessageRole): boolean {
    return role === MessageRole.AGENT
  }

  function formatTime(t: string): string {
    try {
      return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  // imageUrlForPart builds a display data URL from an ImagePart. protojson
  // delivers bytes as base64 (image.data) and the encoding as the proto enum
  // name (e.g. "IMAGE_ENCODING_PNG"); the proto currently defines PNG only.
  function imageUrlForPart(image: ImagePart): string {
    const raw = typeof image.encoding === 'string' ? image.encoding : ''
    const enc = raw.replace(/^IMAGE_ENCODING_/, '').toLowerCase() || 'png'
    return `data:image/${enc};base64,${image.data}`
  }

  // Compact a tool_call's argsJson for INLINE display
  // (specs/027-chat-bubble-game-state/spec.md FR-005;
  // specs/027-chat-bubble-game-state/data-model.md §7;
  // specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §3).
  // Invalid JSON falls back to the raw string (no throw) —
  // specs/027-chat-bubble-game-state/spec.md FR-005 edge case.
  function compactArgs(argsJson?: string): string {
    if (!argsJson) return ''
    try {
      return JSON.stringify(JSON.parse(argsJson))
    } catch {
      return argsJson
    }
  }

  // Follow-or-pause auto-scroll for the thread. Runs as $effect.pre (BEFORE
  // the DOM update — the scrollHeight read below reflects the height the
  // operator currently sees, so the follow decision is stable across content
  // growth; tick() then lands the scroll after the update). Follows the
  // stream down only while the operator is at the bottom (stickToBottom): if
  // they've scrolled up to read history, no scroll happens, so streaming
  // output never locks the whole dialog. This is the same pattern as
  // ChatMessage.svelte's thinking-bubble follow (research.md D2).
  $effect.pre(() => {
    renderItems
    const el = scrollContainer
    if (!el) return
    if (!stickToBottom) return
    tick().then(() => {
      el.scrollTop = el.scrollHeight
    })
  })
</script>

<div class="chat-view">
  {#if messagesError}
    <div class="chat-warning" data-testid="chat-warning">{messagesError}</div>
  {/if}

  <!-- Message Thread -->
  <div class="chat-thread" bind:this={scrollContainer} onscroll={handleThreadScroll}>
    {#if loadingMessages}
      <div class="chat-loading" data-testid="messages-loading">Loading messages...</div>
    {:else if renderItems.length === 0}
      <div class="chat-empty" data-testid="chat-empty">No messages yet. Start a conversation below.</div>
    {:else}
      {#each renderItems as item (item.key)}
        {#if item.kind === 'warn'}
          <div data-testid="chat-message">
            <div class="msg-row msg-warn">
              <div class="msg-bubble warn-bubble">
                <span class="warn-icon">&#9888;</span>
                <span class="msg-content">{item.message}</span>
              </div>
            </div>
          </div>
        {:else if item.kind === 'profile'}
          <div class="msg-profile-label" data-testid="agent-profile-label">{item.profile}</div>
        {:else if item.kind === 'part'}
          {@const kind = messagePartKind(item.part)}
          {#if kind === 'text' && isAgentRole(item.role)}
            {@const sanitizedHtml = renderMarkdown(item.part.text?.content ?? '')}
            <div class="msg-row msg-agent">
              <div class="msg-bubble agent-bubble">
                <div class="msg-sender">Agent</div>
                <div class="msg-content markdown-content">{@html sanitizedHtml}</div>
                <div class="msg-time">{formatTime(item.timestamp)}</div>
              </div>
            </div>
          {:else if kind === 'image'}
            {@const url = imageUrlForPart(item.part.image!)}
            <div class="msg-row msg-image" class:msg-image-user={item.role === MessageRole.USER} class:msg-pending={item.pending}>
              <details class="image-details">
                <summary class="image-summary" data-testid="image-entry-summary">Screenshot</summary>
                <img class="screenshot-img clickable" src={url} alt="Screenshot" data-testid="image-entry-img" onclick={() => onZoom(url)} />
              </details>
            </div>
          {:else if kind === 'text' || kind === 'thinking'}
            <!-- Non-agent text (user / system) and thinking parts render via the
                 ChatMessage bubble component. A pending (queued) user message
                 is visually marked via .msg-pending
                 (specs/030-queued-chat-input/spec.md FR-008). -->
            <div class="msg-pending-wrapper" class:msg-pending={item.pending}>
              <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
            </div>
          {/if}
        {:else if item.kind === 'tool'}
          {@const resolved = item.result != null}
          {@const statusClass = resolved ? classifyToolResultStatus(item.result!.status) : 'neutral'}
          {@const statusIcon = statusClass === 'succeeded' ? '✓' : statusClass === 'failed' ? '✗' : '›'}
          {@const statusLabel = statusClass === 'succeeded' ? 'succeeded' : statusClass === 'failed' ? 'failed' : 'done'}
          <div class="msg-row msg-operation">
            <div
              class="tool-bubble"
              class:tool-resolved-success={resolved && statusClass === 'succeeded'}
              class:tool-resolved-failure={resolved && statusClass === 'failed'}
              class:tool-resolved-neutral={resolved && statusClass === 'neutral'}
              data-testid="tool-bubble"
            >
              <div class="tool-head">
                <span class="tool-name" data-testid="tool-name">{item.name ?? 'tool'}</span>
                {#if item.argsJson}
                  <!-- Inline compact args next to the tool name
                       (specs/027-chat-bubble-game-state/spec.md FR-005;
                       specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §3;
                       specs/027-chat-bubble-game-state/data-model.md §7). -->
                  <code class="tool-args-inline" data-testid="tool-args">{compactArgs(item.argsJson)}</code>
                {/if}
              </div>
              {#if resolved}
                <!-- Collapsible result body
                     (specs/027-chat-bubble-game-state/spec.md FR-007/008;
                     specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §5;
                     specs/027-chat-bubble-game-state/data-model.md §7).
                     The outer <details> has NO `open`
                     attribute → the <summary> (status icon + label) is always
                     visible; the formatted message and the screenshot
                     sub-toggle are hidden until expanded. The screenshot keeps
                     its own nested <details> so its open/closed state is
                     independent (specs/027-chat-bubble-game-state/spec.md FR-008).
                     The pending "running…" branch below
                     stays outside the <details> (nothing to collapse before
                     resolution). -->
                <details class="tool-result-details">
                  <summary>
                    <span class="op-result-icon">{statusIcon}</span>
                    <span class="op-result-status">{statusLabel}</span>
                  </summary>
                  {#if item.result!.message}
                    <!-- pre-wrap preserves the multi-line text board
                         (specs/027-chat-bubble-game-state/spec.md FR-006;
                         specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §4;
                         specs/027-chat-bubble-game-state/data-model.md §7). -->
                    <pre class="op-result-message">{item.result!.message}</pre>
                  {/if}
                  {#if item.result!.screenshot?.data}
                    {@const screenshotUrl = imageUrlForPart(item.result!.screenshot)}
                    <details class="op-result-screenshot-details">
                      <summary class="op-result-summary">Result screenshot</summary>
                      <img
                        class="screenshot-img clickable"
                        src={screenshotUrl}
                        alt="Tool result screenshot"
                        data-testid="operation-result-screenshot"
                        onclick={() => onZoom(screenshotUrl)}
                      />
                    </details>
                  {/if}
                </details>
              {:else}
                <div class="tool-pending">running…</div>
              {/if}
            </div>
          </div>
        {/if}
      {/each}
    {/if}

    {#if processing}
      <div class="typing-indicator">
        <span class="typing-dots">
          <span class="dot"></span>
          <span class="dot"></span>
          <span class="dot"></span>
        </span>
        <span class="typing-text">Agent is typing…</span>
      </div>
    {/if}

    {#if queueCount > 0}
      <!-- Phase 5 (T011): queueCount is backend-driven by QueueSignal
           (specs/030-queued-chat-input/spec.md FR-008). -->
      <div class="queue-indicator" data-testid="queue-indicator">
        {queueCount} message{queueCount !== 1 ? 's' : ''} queued
      </div>
    {/if}
  </div>

  <!-- Input Area. FR-032: blocked (observe-only) for agents that do not
       accept user input (saolei: planner). -->
  <div class="chat-input-area">
    {#if !inputEnabled}
      <div class="observe-hint" data-testid="observe-only-hint">
        Observe only — this agent does not accept user input (FR-032).
      </div>
    {/if}
    {#if pendingScreenshot}
      <div class="pending-attachment" data-testid="pending-attachment">
        <img class="attachment-thumb clickable" src={pendingScreenshot.dataUrl} alt="Screenshot attachment" onclick={() => onZoom(pendingScreenshot.dataUrl)} />
        <span class="attachment-info">{pendingScreenshot.widthPx}×{pendingScreenshot.heightPx}</span>
        <button class="attachment-remove" onclick={onRemoveScreenshot} data-testid="remove-attachment">✕</button>
      </div>
    {/if}
    <!-- Input stays editable/submittable while an agent turn is in progress
         (specs/030-queued-chat-input/spec.md FR-001). A submit during a run is
         buffered by the backend TurnLoop (specs/030-queued-chat-input/spec.md
         FR-002) and rendered as pending via queueCount; the empty-input guard
         below is unrelated to the processing state. -->
    <textarea
      class="chat-input"
      data-testid="chat-input"
      placeholder="Type a message…"
      bind:value={inputText}
      onkeydown={handleKeydown}
      rows={1}
      disabled={!inputEnabled}
    ></textarea>
    <button
      class="send-btn"
      data-testid="chat-send-btn"
      onclick={handleSend}
      disabled={!inputEnabled || (!inputText.trim() && !pendingScreenshot)}
    >
      Send
    </button>
  </div>
</div>

<style>
  .chat-view {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    overflow: hidden;
  }

  /* ── Thread ── */
  .chat-thread {
    flex: 1;
    overflow-y: auto;
    padding: 8px 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .chat-empty {
    padding: 40px 16px;
    text-align: center;
    color: #606080;
    font-size: 12px;
  }

  .msg-profile-label {
    padding: 2px 12px 0;
    font-size: 10px;
    font-weight: 600;
    color: #50fa7b;
    opacity: 0.7;
  }

  .chat-loading {
    padding: 40px 16px;
    text-align: center;
    color: #a0a0b0;
    font-size: 12px;
    font-style: italic;
  }

  .chat-warning {
    padding: 8px 12px;
    font-size: 12px;
    color: #ffb86c;
    background: rgba(255, 184, 108, 0.08);
    border-bottom: 1px solid rgba(255, 184, 108, 0.2);
  }

  /* ── Typing Indicator ── */
  .typing-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 16px;
    color: #8888aa;
    font-size: 12px;
    font-style: italic;
  }

  .typing-dots {
    display: inline-flex;
    gap: 3px;
    align-items: center;
  }

  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: #8888aa;
    animation: dotPulse 1.4s ease-in-out infinite;
  }

  .dot:nth-child(2) {
    animation-delay: 0.2s;
  }

  .dot:nth-child(3) {
    animation-delay: 0.4s;
  }

  @keyframes dotPulse {
    0%, 80%, 100% {
      opacity: 0.3;
      transform: scale(0.8);
    }
    40% {
      opacity: 1;
      transform: scale(1);
    }
  }

  .typing-text {
    user-select: none;
  }

  /* ── Queue Indicator ── */
  .queue-indicator {
    padding: 4px 16px;
    font-size: 11px;
    color: #ffb86c;
    user-select: none;
  }

  /* ── Pending (queued) message visual mark
       (specs/030-queued-chat-input/spec.md FR-008). Per the user decision
       specs/030-queued-chat-input/spec.md FR-010 (removing a queued message)
       is NOT implemented: a message that
       has entered the backend queue cannot be deleted
       (specs/030-queued-chat-input/contracts/queue-channel-contract.md §1). ── */
  .msg-pending {
    opacity: 0.65;
  }

  /* ── Input Area ── */
  .chat-input-area {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px;
    border-top: 1px solid #0f3460;
    background: #1a1a2e;
  }

  /* FR-032 observe-only hint (planner tab) */
  .observe-hint {
    width: 100%;
    font-size: 11px;
    color: #ffb86c;
    font-style: italic;
    user-select: none;
  }

  /* ── Pending Attachment Preview ── */
  .pending-attachment {
    display: flex;
    flex-direction: row;
    gap: 8px;
    align-items: center;
    padding: 6px 8px;
    background: #0f3460;
    border: 1px solid #1a4a80;
    border-radius: 4px;
    margin-bottom: 4px;
    width: 100%;
  }

  .attachment-thumb {
    max-height: 48px;
    max-width: 80px;
    object-fit: contain;
    border-radius: 2px;
  }

  .attachment-info {
    font-size: 11px;
    color: #a0a0b0;
  }

  .attachment-remove {
    background: transparent;
    border: none;
    color: #ff6b6b;
    cursor: pointer;
    font-size: 14px;
    padding: 2px 6px;
  }

  .chat-input {
    flex: 1;
    padding: 8px 10px;
    font-size: 12px;
    font-family: inherit;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
    resize: none;
    min-height: 34px;
    max-height: 120px;
    line-height: 1.4;
  }

  .chat-input:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .chat-input::placeholder {
    color: #606080;
  }

  .chat-input:disabled {
    opacity: 0.5;
  }

  .send-btn {
    padding: 8px 16px;
    font-size: 12px;
    font-weight: 600;
    background: #0f3460;
    border: 1px solid #1a4a80;
    border-radius: 4px;
    color: #ffffff;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    white-space: nowrap;
    align-self: flex-end;
  }

  .send-btn:hover:not(:disabled) {
    background: #1a4a80;
    border-color: #4a9eff;
  }

  .send-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  /* ── Message Row + Bubble (agent text with markdown) ── */
  .msg-row {
    display: flex;
    padding: 2px 12px;
  }

  /* Non-flex wrapper for ChatMessage bubbles: no display: flex, so the
     component's own .msg-row.msg-user (justify-content: flex-end) controls
     user-bubble alignment
     (specs/036-team-mode-bugfix/contracts/desktop-alignment-fix.md §2). */
  .msg-pending-wrapper {
    padding: 2px 12px;
  }

  .msg-row.msg-agent {
    justify-content: flex-start;
  }

  .msg-bubble {
    max-width: 80%;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 12px;
    line-height: 1.5;
  }

  .agent-bubble {
    background: #1a1a3e;
    border: 1px solid #2a2a5e;
    color: #e0e0e0;
    border-bottom-left-radius: 2px;
  }

  .agent-bubble .msg-sender {
    font-size: 10px;
    font-weight: 600;
    color: #50fa7b;
    margin-bottom: 2px;
  }

  .agent-bubble .msg-time {
    font-size: 10px;
    color: rgba(80, 250, 123, 0.4);
    margin-top: 4px;
  }

  /* Markdown content rendered via {@html} */
  .markdown-content :global(p) {
    margin: 0 0 6px;
  }

  .markdown-content :global(p:last-child) {
    margin-bottom: 0;
  }

  .markdown-content :global(pre) {
    margin: 4px 0;
    padding: 6px 8px;
    background: #0d0d2b;
    border-radius: 4px;
    font-size: 11px;
    overflow-x: auto;
  }

  .markdown-content :global(code) {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
  }

  .markdown-content :global(pre code) {
    background: none;
    padding: 0;
  }

  .markdown-content :global(:not(pre) > code) {
    background: rgba(80, 250, 123, 0.1);
    padding: 1px 4px;
    border-radius: 3px;
  }

  .markdown-content :global(a) {
    color: #4a9eff;
  }

  .markdown-content :global(blockquote) {
    margin: 4px 0;
    padding: 2px 10px;
    border-left: 2px solid #333355;
    color: #8888aa;
  }

  .markdown-content :global(ul),
  .markdown-content :global(ol) {
    margin: 4px 0;
    padding-left: 18px;
  }

  .markdown-content :global(li) {
    margin: 2px 0;
  }

  /* ── Image Entry ── */
  .msg-image {
    justify-content: flex-start;
  }

  .msg-image.msg-image-user {
    justify-content: flex-end;
  }

  .image-details {
    max-width: 80%;
    background: #1a1a3e;
    border: 1px solid #2a2a5e;
    border-radius: 6px;
    padding: 4px 8px;
  }

  .image-summary {
    font-size: 11px;
    color: #8888aa;
    cursor: pointer;
    user-select: none;
  }

  .screenshot-img {
    max-width: 100%;
    margin-top: 6px;
    border-radius: 4px;
    display: block;
  }

  /* ── Operation Entry ── */
  .msg-operation,
  .msg-operation-result {
    justify-content: flex-start;
  }

  .op-card {
    max-width: 80%;
    padding: 6px 10px;
    background: rgba(74, 158, 255, 0.06);
    border: 1px solid rgba(74, 158, 255, 0.2);
    border-radius: 6px;
    font-size: 11px;
    color: #a0c8ff;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .op-label {
    font-weight: 600;
    color: #4a9eff;
  }

  .op-action {
    color: #e0e0e0;
  }

  .op-coords {
    color: #50fa7b;
  }

  /* ── Operation Result Entry ── */
  .op-result-card {
    max-width: 80%;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 11px;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }

  .op-result-success {
    background: rgba(80, 250, 123, 0.08);
    border: 1px solid rgba(80, 250, 123, 0.3);
    color: #50fa7b;
  }

  .op-result-failure {
    background: rgba(255, 107, 107, 0.08);
    border: 1px solid rgba(255, 107, 107, 0.3);
    color: #ff6b6b;
  }

  .op-result-icon {
    font-weight: 700;
  }

  .op-result-status {
    font-weight: 600;
  }

  .op-result-message {
    /* specs/027-chat-bubble-game-state/spec.md FR-006;
       specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §4;
       specs/027-chat-bubble-game-state/data-model.md §7:
       preserve newlines and wrap.
       MDN white-space: https://developer.mozilla.org/en-US/docs/Web/CSS/white-space
       — `pre-wrap` preserves newlines and wraps overlong lines (unlike `pre`,
       which would horizontally scroll). */
    white-space: pre-wrap;
    word-break: break-word;
    margin: 6px 0 0;
    color: #a0a0b0;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .op-result-screenshot-details {
    margin-top: 4px;
  }

  .op-result-summary {
    font-size: 11px;
    color: #8888aa;
    cursor: pointer;
    user-select: none;
  }

  /* ── Tool Bubble (tool call + result; specs/024-tool-render-coord-fix
       data-model.md §2; research.md D5) ──
     One evolving bubble per tool_call.id (specs/023-saolei-mcp-refine/spec.md FR-007). These rules reuse the
     pre-refactor .op-card / .op-result-* visual language: a bordered monospace
     box whose border/background tint reflects the resolved status
     (success/failed/neutral — data-model.md §1). The legacy .op-* rules are
     retained (research.md D5: removal optional). */
  .tool-bubble {
    max-width: 80%;
    padding: 6px 10px;
    background: rgba(74, 158, 255, 0.06);
    border: 1px solid rgba(74, 158, 255, 0.2);
    border-radius: 6px;
    font-size: 11px;
    color: #a0c8ff;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .tool-head {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }

  .tool-name {
    font-weight: 600;
    color: #4a9eff;
  }

  .tool-args-inline {
    /* Inline compact args (specs/027-chat-bubble-game-state/spec.md FR-005;
       specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §3;
       specs/027-chat-bubble-game-state/data-model.md §7).
       `<code>` gives monospace inline without forcing a block. */
    background: #0d0d2b;
    padding: 1px 4px;
    border-radius: 3px;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    word-break: break-word;
  }

  .tool-result-details {
    /* Collapsible result body (specs/027-chat-bubble-game-state/spec.md FR-007/008;
       specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §5;
       specs/027-chat-bubble-game-state/data-model.md §7). */
    margin-top: 4px;
  }

  .tool-result-details > summary {
    cursor: pointer;
    user-select: none;
  }

  .tool-pending {
    margin-top: 4px;
    color: #8888aa;
    font-style: italic;
  }

  /* Resolved-state tints applied to .tool-bubble: success/failed reuse the
     .op-result-success/.op-result-failure palette; neutral is the muted
     palette for an absent/UNSPECIFIED status (protojson omits the zero-value
     enum — https://protobuf.dev/programming-guides/json/#presence — so a
     saolei/MCP result arrives with status absent and MUST read neutral, never
     failed; data-model.md §1). */
  .tool-resolved-success {
    background: rgba(80, 250, 123, 0.08);
    border-color: rgba(80, 250, 123, 0.3);
    color: #50fa7b;
  }

  .tool-resolved-failure {
    background: rgba(255, 107, 107, 0.08);
    border-color: rgba(255, 107, 107, 0.3);
    color: #ff6b6b;
  }

  .tool-resolved-neutral {
    background: rgba(136, 136, 170, 0.08);
    border-color: rgba(136, 136, 170, 0.3);
    color: #8888aa;
  }

  /* ── Warn Bubble (control-signal payload) ── */
  .msg-row.msg-warn {
    justify-content: flex-start;
  }

  .warn-bubble {
    background: rgba(255, 184, 108, 0.08);
    border: 1px solid rgba(255, 184, 108, 0.3);
    color: #ffb86c;
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: 12px;
  }

  .warn-icon {
    font-size: 14px;
    flex-shrink: 0;
    line-height: 1.5;
  }

  .warn-bubble .msg-content {
    white-space: pre-wrap;
    word-break: break-word;
  }

  .clickable {
    cursor: pointer;
  }
</style>
