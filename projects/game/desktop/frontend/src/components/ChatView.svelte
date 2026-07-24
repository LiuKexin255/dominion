<script lang="ts">
  import ChatMessage from './ChatMessage.svelte'
  import { FrameSender, MouseClickAction, ToolResultStatus, partKind } from '../api'
  import type { Part, ImagePart } from '../api'
  import { renderMarkdown } from '../markdown'

  type ChatEntry = {
    messageId: string
    sender: FrameSender
    timestamp: string
    agentProfileName?: string
    parts?: Part[]
    warnMessage?: string
  }

  // Lookup tables for enum → display-name conversions. protojson serializes
  // enums as their proto names (e.g. "MOUSE_CLICK_ACTION_LEFT_CLICK"), while
  // the TypeScript types in api.ts declare them as numeric enums. To stay
  // robust against both forms, each table maps both the numeric enum value
  // (coerced to string by the Record lookup) and the proto name string to the
  // same display name.
  const MOUSE_CLICK_DISPLAY: Record<string, string> = {
    [String(MouseClickAction.LEFT_CLICK)]: 'LEFT_CLICK',
    [String(MouseClickAction.LEFT_DOUBLE_CLICK)]: 'LEFT_DOUBLE_CLICK',
    [String(MouseClickAction.RIGHT_CLICK)]: 'RIGHT_CLICK',
    [String(MouseClickAction.RIGHT_DOUBLE_CLICK)]: 'RIGHT_DOUBLE_CLICK',
    [String(MouseClickAction.LEFT_RIGHT_PRESS)]: 'LEFT_RIGHT_PRESS',
    MOUSE_CLICK_ACTION_LEFT_CLICK: 'LEFT_CLICK',
    MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK: 'LEFT_DOUBLE_CLICK',
    MOUSE_CLICK_ACTION_RIGHT_CLICK: 'RIGHT_CLICK',
    MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK: 'RIGHT_DOUBLE_CLICK',
    MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS: 'LEFT_RIGHT_PRESS',
  }

  const TOOL_RESULT_SUCCESS_VALUES: ReadonlySet<string> = new Set<string>([
    String(ToolResultStatus.SUCCEEDED),
    'TOOL_RESULT_STATUS_SUCCEEDED',
  ])

  let {
    messages,
    processing = false,
    queueCount = 0,
    loadingMessages = false,
    messagesError = null,
    onSend,
    onZoom = () => {},
    pendingScreenshot = null,
    onRemoveScreenshot = () => {},
    heldToolIds = new Set<string>(),
    onConfirm = (_toolID: string) => {},
  }: {
    messages: ChatEntry[]
    processing?: boolean
    queueCount?: number
    loadingMessages?: boolean
    messagesError?: string | null
    onSend: (text: string) => void
    onZoom?: (url: string) => void
    pendingScreenshot?: { dataUrl: string; widthPx: number; heightPx: number } | null
    onRemoveScreenshot?: () => void
    heldToolIds?: Set<string>
    onConfirm?: (toolID: string) => void
  } = $props()

  let inputText = $state('')
  let scrollContainer: HTMLDivElement | undefined = $state()

  function handleSend() {
    const text = inputText.trim()
    if (!text && !pendingScreenshot) return
    onSend(text)
    inputText = ''
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  function isAgentEntry(msg: ChatEntry): boolean {
    return msg.sender === FrameSender.AGENT
  }

  function formatTime(t: string): string {
    try {
      return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  function describeMouseClick(click: MouseClickAction | string | undefined): string {
    // protojson serializes enums as their proto names (e.g.
    // "MOUSE_CLICK_ACTION_LEFT_CLICK"), not their numeric values, so the
    // lookup table accepts both the numeric enum (coerced to string) and the
    // proto name form. See game.proto / protojson defaults.
    if (click == null) return 'UNSPECIFIED'
    return MOUSE_CLICK_DISPLAY[String(click)] ?? 'UNSPECIFIED'
  }

  function isToolResultSucceeded(status: number | string | undefined): boolean {
    // protojson emits the proto name ("TOOL_RESULT_STATUS_SUCCEEDED"); accept
    // both that and the numeric enum form.
    if (status == null) return false
    return TOOL_RESULT_SUCCESS_VALUES.has(String(status))
  }

  // imageUrlForPart builds a display data URL from an ImagePart. protojson
  // delivers bytes as base64 (image.data) and the encoding as the proto enum
  // name (e.g. "IMAGE_ENCODING_PNG"); the proto currently defines PNG only.
  function imageUrlForPart(image: ImagePart): string {
    const raw = typeof image.encoding === 'string' ? image.encoding : ''
    const enc = raw.replace(/^IMAGE_ENCODING_/, '').toLowerCase() || 'png'
    return `data:image/${enc};base64,${image.data}`
  }

  $effect(() => {
    // reactively scroll when messages change
    messages
    const el = scrollContainer
    if (el) {
      requestAnimationFrame(() => {
        el.scrollTop = el.scrollHeight
      })
    }
  })
</script>

<div class="chat-view">
  {#if messagesError}
    <div class="chat-warning" data-testid="chat-warning">{messagesError}</div>
  {/if}

  <!-- Message Thread -->
  <div class="chat-thread" bind:this={scrollContainer}>
    {#if loadingMessages}
      <div class="chat-loading" data-testid="messages-loading">Loading messages...</div>
    {:else if messages.length === 0}
      <div class="chat-empty" data-testid="chat-empty">No messages yet. Start a conversation below.</div>
    {:else}
      {#each messages as msg (msg.messageId)}
        {#if msg.warnMessage != null}
          <!-- Warn control-signal payload: a warning bubble. -->
          <div data-testid="chat-message">
            <div class="msg-row msg-warn">
              <div class="msg-bubble warn-bubble">
                <span class="warn-icon">&#9888;</span>
                <span class="msg-content">{msg.warnMessage}</span>
              </div>
            </div>
          </div>
        {:else if msg.parts && msg.parts.length > 0}
          {#if isAgentEntry(msg) && msg.agentProfileName}
            <div class="msg-profile-label" data-testid="agent-profile-label">{msg.agentProfileName}</div>
          {/if}
          <div data-testid="chat-message">
            <!-- Render each Part by its kind. The part kind — not a separate
                 `type` field — is the sole discriminator. Unknown / empty
                 parts are skipped (graceful degradation). -->
            {#each msg.parts as part, partIndex (partIndex)}
              {@const kind = partKind(part)}
              {#if kind === 'text' && isAgentEntry(msg)}
                {@const sanitizedHtml = renderMarkdown(part.text?.content ?? '')}
                <div class="msg-row msg-agent">
                  <div class="msg-bubble agent-bubble">
                    <div class="msg-sender">Agent</div>
                    <div class="msg-content markdown-content">{@html sanitizedHtml}</div>
                    {#if partIndex === msg.parts.length - 1}
                      <div class="msg-time">{formatTime(msg.timestamp)}</div>
                    {/if}
                  </div>
                </div>
              {:else if kind === 'image'}
                {@const url = imageUrlForPart(part.image!)}
                <div class="msg-row msg-image" class:msg-image-user={msg.sender === FrameSender.USER}>
                  <details class="image-details">
                    <summary class="image-summary" data-testid="image-entry-summary">Screenshot</summary>
                    <img class="screenshot-img clickable" src={url} alt="Screenshot" data-testid="image-entry-img" onclick={() => onZoom(url)} />
                  </details>
                </div>
              {:else if kind === 'mouseMove'}
                {@const mv = part.mouseMove!}
                <div class="msg-row msg-operation">
                  <div class="op-card" data-testid="operation-entry">
                    <span class="op-label">MouseMove</span>
                    <span class="op-action">MOVE</span>
                    <span class="op-coords">(@ {mv.xPx}, {mv.yPx})</span>
                  </div>
                </div>
              {:else if kind === 'mouseClick'}
                {@const mc = part.mouseClick!}
                <div class="msg-row msg-operation">
                  <div class="op-card" data-testid="operation-entry">
                    <span class="op-label">MouseClick</span>
                    <span class="op-action">{describeMouseClick(mc.click)}</span>
                  </div>
                </div>
              {:else if kind === 'toolResult'}
                {@const result = part.toolResult!}
                {@const succeeded = isToolResultSucceeded(result.status)}
                {@const isHeld = result.toolId != null && heldToolIds.has(result.toolId)}
                <div class="msg-row msg-operation-result">
                  <div class="op-result-card" class:op-result-success={succeeded} class:op-result-failure={!succeeded} data-testid="operation-result-entry">
                    <span class="op-result-icon">{succeeded ? '✓' : '✗'}</span>
                    <span class="op-result-status">{succeeded ? 'succeeded' : 'failed'}</span>
                    {#if result.message}
                      <span class="op-result-message">{result.message}</span>
                    {/if}
                    {#if result.screenshot?.data}
                      {@const screenshotUrl = imageUrlForPart(result.screenshot)}
                      <details class="op-result-details">
                        <summary class="op-result-summary">Result screenshot</summary>
                        <img
                          class="screenshot-img clickable"
                          src={screenshotUrl}
                          alt="Operation result screenshot"
                          data-testid="operation-result-screenshot"
                          onclick={() => onZoom(screenshotUrl)}
                        />
                      </details>
                    {/if}
                    {#if isHeld}
                      <button
                        class="btn btn-small confirm-btn"
                        data-testid="confirm-tool-result"
                        onclick={() => onConfirm(result.toolId!)}
                      >Confirm</button>
                    {/if}
                  </div>
                </div>
              {:else if kind === 'text' || kind === 'thinking'}
                <!-- Non-agent text (user / system) and thinking parts render
                     via the ChatMessage bubble component. -->
                <ChatMessage {part} sender={msg.sender} timestamp={msg.timestamp} />
              {/if}
            {/each}
          </div>
        {/if}
        <!-- A content entry with zero parts renders nothing (graceful
             degradation for empty PartBlocks). -->
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
      <div class="queue-indicator">
        {queueCount} message{queueCount !== 1 ? 's' : ''} queued
      </div>
    {/if}
  </div>

  <!-- Input Area -->
  <div class="chat-input-area">
    {#if pendingScreenshot}
      <div class="pending-attachment" data-testid="pending-attachment">
        <img class="attachment-thumb clickable" src={pendingScreenshot.dataUrl} alt="Screenshot attachment" onclick={() => onZoom(pendingScreenshot.dataUrl)} />
        <span class="attachment-info">{pendingScreenshot.widthPx}×{pendingScreenshot.heightPx}</span>
        <button class="attachment-remove" onclick={onRemoveScreenshot} data-testid="remove-attachment">✕</button>
      </div>
    {/if}
    <textarea
      class="chat-input"
      data-testid="chat-input"
      placeholder="Type a message…"
      bind:value={inputText}
      onkeydown={handleKeydown}
      rows={1}
      disabled={processing}
    ></textarea>
    <button
      class="send-btn"
      data-testid="chat-send-btn"
      onclick={handleSend}
      disabled={processing || (!inputText.trim() && !pendingScreenshot)}
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

  /* ── Input Area ── */
  .chat-input-area {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px;
    border-top: 1px solid #0f3460;
    background: #1a1a2e;
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
    color: #a0a0b0;
  }

  .op-result-details {
    width: 100%;
    margin-top: 4px;
  }

  .op-result-summary {
    font-size: 11px;
    color: #8888aa;
    cursor: pointer;
    user-select: none;
  }

  /* Confirm control for a held tool result (debug mode, FR-008/FR-009). */
  .confirm-btn {
    margin-left: auto;
    background: rgba(139, 233, 253, 0.12);
    border: 1px solid rgba(139, 233, 253, 0.4);
    color: #8be9fd;
    font-weight: 600;
  }

  .confirm-btn:hover {
    background: rgba(139, 233, 253, 0.2);
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
