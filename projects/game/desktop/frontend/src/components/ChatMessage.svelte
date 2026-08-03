<script lang="ts">
  import { tick } from 'svelte'
  import { FrameSender, messagePartKind } from '../api'
  import type { MessagePart } from '../api'

  // ChatMessage renders a single MessagePart as a simple bubble. It owns the
  // "plain" bubble shapes: user text, agent/system text, and thinking. Complex
  // part kinds (image, tool_call, tool_result) are rendered inline by ChatView.
  // The part kind — not a `type` field — is the discriminator.
  let {
    part,
    sender,
    timestamp,
  }: {
    part: MessagePart
    sender: FrameSender
    timestamp: string
  } = $props()

  let expanded = $state(false)
  // bind:this on the .thinking-content <pre>; drives the auto-scroll $effects
  // below (FR-002..004).
  let contentEl: HTMLPreElement | undefined = $state()

  function formatTime(t: string): string {
    try {
      return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  let kind = $derived(messagePartKind(part))
  let isUser = $derived(sender === FrameSender.USER)
  let isUserText = $derived(kind === 'text' && isUser)
  let isSystemText = $derived(kind === 'text' && !isUser)
  let isThinking = $derived(kind === 'thinking')

  // TOLERANCE for the at-bottom test (px); absorbs sub-pixel float jitter so
  // "at the bottom" is stable across browsers.
  // Ref: specs/027-chat-bubble-game-state/data-model.md §6 (TOLERANCE = 8);
  //      specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §2.
  const TOLERANCE = 8

  // FR-004 (specs/027-chat-bubble-game-state/spec.md): open scrolled to the
  // bottom when the bubble is expanded, so the latest reasoning is visible
  // immediately. Mirrors the chat-thread pattern in
  // projects/game/desktop/frontend/src/components/ChatView.svelte
  // ($effect + requestAnimationFrame after the DOM update).
  // Design: specs/027-chat-bubble-game-state/data-model.md §6;
  //         specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §2 (rule 1);
  //         specs/027-chat-bubble-game-state/research.md D2 ($effect / $effect.pre split).
  $effect(() => {
    if (!expanded) return
    const el = contentEl
    if (!el) return
    requestAnimationFrame(() => {
      el.scrollTop = el.scrollHeight
    })
  })

  // FR-002/FR-003 (specs/027-chat-bubble-game-state/spec.md): while expanded,
  // follow the streaming content if the operator is at the bottom; if they've
  // scrolled up, do nothing (pause) so they can read history. The at-bottom
  // test is `scrollTop + clientHeight >= scrollHeight − TOLERANCE` per
  // specs/027-chat-bubble-game-state/data-model.md §6 /
  // specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §2 (rule 2..4).
  // The split (open-to-bottom `$effect` vs follow-or-pause `$effect.pre`) is
  // decision specs/027-chat-bubble-game-state/research.md D2.
  //
  // MUST run as `$effect.pre` (BEFORE the DOM update), NOT `$effect`: the
  // at-bottom check reads `scrollHeight`. After the DOM update (i.e. inside a
  // regular `$effect`), `scrollHeight` already reflects the newly-appended
  // reasoning while `scrollTop` is still the operator's pre-update position,
  // so `scrollTop + clientHeight >= scrollHeight − TOLERANCE` is false on
  // every content growth — the bubble freezes at the top instead of following
  // the stream. `$effect.pre` reads `scrollHeight` before the new content
  // renders (the height the operator currently sees), so the at-bottom test
  // reflects the operator's true position; `tick().then(...)` then scrolls
  // after the DOM update lands the new bottom. This is the Svelte autoscroll
  // pattern: https://svelte.dev/docs/svelte/$effect#$effect.pre
  $effect.pre(() => {
    if (!expanded) return
    // reactive dependency: re-run when the streaming content grows
    part.thinking?.content
    const el = contentEl
    if (!el) return
    const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - TOLERANCE
    if (!atBottom) return
    tick().then(() => {
      el.scrollTop = el.scrollHeight
    })
  })
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
        <pre class="thinking-content" bind:this={contentEl}>{part.thinking?.content ?? ''}</pre>
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

  /* FR-001 (specs/027-chat-bubble-game-state/spec.md): hide the visible
   * scrollbar track/thumb on overflow while keeping the area scrollable
   * (overflow-y: auto and max-height: 200px are unchanged). `scrollbar-width`
   * is the standard (CSS Scrollbars Styling L1) rule, portable to Firefox;
   * `::-webkit-scrollbar { display: none }` is the WebKit/Chromium rule,
   * operative on the Wails v2 WebView2 runtime. Design:
   * specs/027-chat-bubble-game-state/data-model.md §6 (CSS);
   * specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md §1;
   * specs/027-chat-bubble-game-state/research.md D1.
   * External refs:
   * - https://developer.mozilla.org/en-US/docs/Web/CSS/scrollbar-width
   * - https://developer.mozilla.org/en-US/docs/Web/CSS/::-webkit-scrollbar */
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
    scrollbar-width: none;
  }

  .thinking-content::-webkit-scrollbar {
    display: none;
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
