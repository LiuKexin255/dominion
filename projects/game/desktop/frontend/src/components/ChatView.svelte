<script lang="ts">
  import ChatMessage from './ChatMessage.svelte'
  import { FrameSender } from '../api'

  type ChatEntry = {
    sender: FrameSender
    type: 'thinking' | 'text' | 'warn'
    content: string
    timestamp: string
  }

  let {
    messages,
    processing = false,
    queueCount = 0,
    loadingMessages = false,
    messagesError = null,
    onSend,
  }: {
    messages: ChatEntry[]
    processing?: boolean
    queueCount?: number
    loadingMessages?: boolean
    messagesError?: string | null
    onSend: (text: string) => void
  } = $props()

  let inputText = $state('')
  let scrollContainer: HTMLDivElement | undefined = $state()

  function handleSend() {
    const text = inputText.trim()
    if (!text) return
    onSend(text)
    inputText = ''
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
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
      {#each messages as msg (msg.timestamp)}
        <div data-testid="chat-message">
          <ChatMessage message={msg} />
        </div>
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
      disabled={processing || !inputText.trim()}
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
    gap: 8px;
    padding: 8px;
    border-top: 1px solid #0f3460;
    background: #1a1a2e;
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
</style>
