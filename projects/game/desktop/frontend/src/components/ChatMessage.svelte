<script lang="ts">
  import { FrameSender, partKind } from '../api'
  import type { Part } from '../api'

  // ChatMessage renders a single Part as a simple bubble. It owns the
  // "plain" bubble shapes: user text, agent/system text, and thinking. Complex
  // part kinds (image, mouse move/click, tool result) are rendered inline by
  // ChatView. The part kind — not a `type` field — is the discriminator.
  let {
    part,
    sender,
    timestamp,
  }: {
    part: Part
    sender: FrameSender
    timestamp: string
  } = $props()

  let expanded = $state(false)

  function formatTime(t: string): string {
    try {
      return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  let kind = $derived(partKind(part))
  let isUser = $derived(sender === FrameSender.USER)
  let isUserText = $derived(kind === 'text' && isUser)
  let isSystemText = $derived(kind === 'text' && !isUser)
  let isThinking = $derived(kind === 'thinking')
</script>

{#if isUserText}
  <div class="msg-row msg-user">
    <div class="msg-bubble user-bubble">
      <div class="msg-sender">You</div>
      <div class="msg-content">{part.text?.content ?? ''}</div>
      <div class="msg-time">{formatTime(timestamp)}</div>
    </div>
  </div>
{:else if isThinking}
  <div class="msg-row msg-agent">
    <div class="msg-bubble thinking-bubble">
      <button class="thinking-toggle" onclick={() => expanded = !expanded}>
        <span class="thinking-icon">{expanded ? '▾' : '▸'}</span>
        <span class="thinking-label">Thinking…</span>
      </button>
      {#if expanded}
        <pre class="thinking-content">{part.thinking?.content ?? ''}</pre>
      {/if}
    </div>
  </div>
{:else if isSystemText}
  <div class="msg-row">
    <div class="msg-bubble system-bubble">
      <span class="msg-content">{part.text?.content ?? ''}</span>
    </div>
  </div>
{:else}
  <!-- Unhandled part kind: graceful degradation, render nothing. -->
{/if}

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
