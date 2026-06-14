<script lang="ts">
  import { FrameSender } from '../api'

  let {
    message,
  }: {
    message: {
      sender: FrameSender
      type: 'thinking' | 'text' | 'warn'
      content: string
      timestamp: string
    }
  } = $props()

  let expanded = $state(false)

  function formatTime(t: string): string {
    try {
      return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  let isUser = $derived(message.sender === FrameSender.USER)
  let isAgent = $derived(message.sender === FrameSender.AGENT)
  let isSystem = $derived(message.sender === FrameSender.SYSTEM)
  let isThinking = $derived(isAgent && message.type === 'thinking')
  let isWarn = $derived(isSystem && message.type === 'warn')
  let isAgentText = $derived(isAgent && message.type === 'text')
</script>

<div class="msg-row" class:msg-user={isUser} class:msg-agent={isAgentText || isThinking} class:msg-warn={isWarn}>
  {#if isUser}
    <div class="msg-bubble user-bubble">
      <div class="msg-sender">You</div>
      <div class="msg-content">{message.content}</div>
      <div class="msg-time">{formatTime(message.timestamp)}</div>
    </div>
  {:else if isThinking}
    <div class="msg-bubble thinking-bubble">
      <button class="thinking-toggle" onclick={() => expanded = !expanded}>
        <span class="thinking-icon">{expanded ? '▾' : '▸'}</span>
        <span class="thinking-label">Thinking…</span>
      </button>
      {#if expanded}
        <pre class="thinking-content">{message.content}</pre>
      {/if}
    </div>
  {:else if isAgentText}
    <div class="msg-bubble agent-bubble">
      <div class="msg-sender">Agent</div>
      <div class="msg-content">{message.content}</div>
      <div class="msg-time">{formatTime(message.timestamp)}</div>
    </div>
  {:else if isWarn}
    <div class="msg-bubble warn-bubble">
      <span class="warn-icon">&#9888;</span>
      <span class="msg-content">{message.content}</span>
    </div>
  {:else}
    <div class="msg-bubble system-bubble">
      <span class="msg-content">{message.content}</span>
    </div>
  {/if}
</div>

<style>
  .msg-row {
    display: flex;
    padding: 2px 12px;
  }

  .msg-row.msg-user {
    justify-content: flex-end;
  }

  .msg-row.msg-agent {
    justify-content: flex-start;
  }

  .msg-row.msg-warn {
    justify-content: flex-start;
  }

  .msg-bubble {
    max-width: 80%;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 12px;
    line-height: 1.5;
    position: relative;
  }

  /* ── User ── */
  .user-bubble {
    background: #0f3460;
    border: 1px solid #1a4a80;
    color: #e0e0e0;
    border-bottom-right-radius: 2px;
  }

  .user-bubble .msg-sender {
    font-size: 10px;
    font-weight: 600;
    color: #4a9eff;
    margin-bottom: 2px;
  }

  .user-bubble .msg-time {
    font-size: 10px;
    color: rgba(74, 158, 255, 0.5);
    text-align: right;
    margin-top: 4px;
  }

  .user-bubble .msg-content {
    white-space: pre-wrap;
    word-break: break-word;
  }

  /* ── Agent Text ── */
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

  .agent-bubble .msg-content {
    white-space: pre-wrap;
    word-break: break-word;
  }

  /* ── Thinking ── */
  .thinking-bubble {
    background: rgba(136, 136, 170, 0.06);
    border: 1px dashed #333355;
    color: #8888aa;
    font-style: italic;
    padding: 6px 12px;
  }

  .thinking-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    background: none;
    border: none;
    cursor: pointer;
    color: #8888aa;
    font-size: 12px;
    font-style: italic;
    padding: 0;
    width: 100%;
    text-align: left;
  }

  .thinking-toggle:hover {
    color: #aaaacc;
  }

  .thinking-icon {
    font-size: 10px;
    font-style: normal;
  }

  .thinking-label {
    user-select: none;
  }

  .thinking-content {
    margin: 8px 0 0 0;
    padding: 8px;
    background: #0d0d2b;
    border-radius: 4px;
    font-size: 11px;
    font-style: normal;
    white-space: pre-wrap;
    word-break: break-word;
    color: #8888aa;
    max-height: 200px;
    overflow-y: auto;
    line-height: 1.4;
  }

  /* ── Warning ── */
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

  /* ── System fallback ── */
  .system-bubble {
    background: #1a1a3e;
    border: 1px solid #0f3460;
    color: #a0a0b0;
  }

  .system-bubble .msg-content {
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
