<script lang="ts">
  import type { WindowRef, AgentAckFrame } from '../api'

  let {
    sessionId,
    windows,
    boundWindow,
    screenshotData,
    screenshotMeta,
    ackResult,
    playState,
    wsConnected,
    loading,
    error,
    onListWindows,
    onBindWindow,
    onCaptureScreenshot,
    onSendScreenshot,
    onBack,
  }: {
    sessionId: string
    windows: WindowRef[]
    boundWindow: WindowRef | null
    screenshotData: string | null
    screenshotMeta: { width: number; height: number; encoding: string } | null
    ackResult: AgentAckFrame | null
    playState: string
    wsConnected: boolean
    loading: boolean
    error: string | null
    onListWindows: () => void
    onBindWindow: (hwnd: number) => void
    onCaptureScreenshot: () => void
    onSendScreenshot: (hwnd: number) => void
    onBack: () => void
  } = $props()
</script>

<div class="play-view">
  <!-- Top Bar -->
  <div class="top-bar">
    <button class="btn" onclick={onBack} disabled={loading}>Back</button>
    <span class="session-label">Session: <strong>{sessionId}</strong></span>
    <span class="state-badge">{playState}</span>
  </div>

  {#if error}
    <div class="error-bar">{error}</div>
  {/if}

  <!-- Window Section -->
  <section class="section">
    <div class="section-header">
      <h3>Windows</h3>
      <button class="btn btn-primary" onclick={onListWindows} disabled={loading}>
        List Windows
      </button>
    </div>

    {#if windows.length > 0}
      <div class="window-list">
        {#each windows as win}
          <div class="window-item">
            <div class="window-info">
              <span class="window-title">{win.title || '(untitled)'}</span>
              <span class="window-detail">PID: {win.processID}</span>
              <span class="window-detail">{win.widthPx} × {win.heightPx}</span>
            </div>
            <button
              class="btn btn-small"
              onclick={() => onBindWindow(win.handle)}
              disabled={loading}
            >
              Bind
            </button>
          </div>
        {/each}
      </div>
    {:else}
      <div class="empty-hint">No windows enumerated yet.</div>
    {/if}

    {#if boundWindow}
      <div class="bound-info">
        Bound: <strong>{boundWindow.title || '(untitled)'}</strong>
        &nbsp;PID {boundWindow.processID}
        &nbsp;{boundWindow.widthPx} × {boundWindow.heightPx}
        &nbsp;Scale {boundWindow.scaleFactor}
      </div>
    {/if}
  </section>

  <!-- Screenshot Section -->
  <section class="section">
    <div class="section-header">
      <h3>Screenshot</h3>
      <button
        class="btn btn-primary"
        onclick={onCaptureScreenshot}
        disabled={loading || !boundWindow}
      >
        Capture Screenshot
      </button>
    </div>

    {#if screenshotData}
      <div class="screenshot-preview">
        <img
          src="data:image/png;base64,{screenshotData}"
          alt="Screenshot"
        />
        {#if screenshotMeta}
          <div class="screenshot-meta">
            {screenshotMeta.width} × {screenshotMeta.height} ({screenshotMeta.encoding})
          </div>
        {/if}
      </div>
    {:else}
      <div class="empty-hint">No screenshot captured yet.</div>
    {/if}
  </section>

  <!-- Send Section -->
  <section class="section">
    <div class="section-header">
      <h3>Send to Agent</h3>
      <button
        class="btn btn-primary"
        onclick={() => boundWindow && onSendScreenshot(boundWindow.handle)}
        disabled={loading || !boundWindow || !screenshotData}
      >
        Send Screenshot
      </button>
    </div>

    {#if ackResult}
      <div class="ack-result">
        <div class="ack-field">
          <span class="ack-label">Frame ID:</span>
          <span class="ack-value">{ackResult.ackFrameId}</span>
        </div>
        <div class="ack-field">
          <span class="ack-label">Message:</span>
          <span class="ack-value">{ackResult.message}</span>
        </div>
      </div>
    {:else}
      <div class="empty-hint">No ack received yet.</div>
    {/if}
  </section>

  <!-- Status Bar -->
  <div class="status-bar">
    <span class="status-text">State: {playState}</span>
    <span class="ws-indicator" class:connected={wsConnected}>
      WS: {wsConnected ? 'Connected' : 'Disconnected'}
    </span>
  </div>
</div>

<style>
  .play-view {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    padding: 8px;
    overflow-y: auto;
  }

  /* Top Bar */
  .top-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
  }

  .session-label {
    font-size: 12px;
    color: #a0a0b0;
  }

  .session-label strong {
    color: #e0e0e0;
  }

  .state-badge {
    margin-left: auto;
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 3px;
    background: #0f3460;
    color: #4a9eff;
  }

  /* Error Bar */
  .error-bar {
    padding: 8px 12px;
    background: rgba(255, 107, 107, 0.15);
    border: 1px solid #ff6b6b;
    border-radius: 4px;
    color: #ff6b6b;
    font-size: 12px;
  }

  /* Sections */
  .section {
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    padding: 10px;
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  .section-header h3 {
    font-size: 13px;
    font-weight: 600;
    color: #e0e0e0;
    margin: 0;
  }

  .empty-hint {
    padding: 12px;
    text-align: center;
    color: #606080;
    font-size: 12px;
  }

  /* Window List */
  .window-list {
    max-height: 200px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .window-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px;
    background: #1a1a3e;
    border-radius: 4px;
  }

  .window-item:hover {
    background: #2a2a5e;
  }

  .window-info {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .window-title {
    font-size: 12px;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 200px;
  }

  .window-detail {
    font-size: 11px;
    color: #a0a0b0;
    flex-shrink: 0;
  }

  .bound-info {
    margin-top: 8px;
    padding: 6px 10px;
    background: rgba(74, 158, 255, 0.1);
    border: 1px solid #0f3460;
    border-radius: 4px;
    font-size: 12px;
    color: #a0a0b0;
  }

  .bound-info strong {
    color: #4a9eff;
  }

  /* Screenshot */
  .screenshot-preview {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .screenshot-preview img {
    max-width: 100%;
    max-height: 300px;
    border: 1px solid #0f3460;
    border-radius: 4px;
    object-fit: contain;
  }

  .screenshot-meta {
    font-size: 11px;
    color: #a0a0b0;
  }

  /* Ack Result */
  .ack-result {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
    background: #1a1a3e;
    border-radius: 4px;
  }

  .ack-field {
    display: flex;
    gap: 8px;
    font-size: 12px;
  }

  .ack-label {
    color: #a0a0b0;
    min-width: 70px;
  }

  .ack-value {
    color: #8be9fd;
    word-break: break-all;
  }

  /* Status Bar */
  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 14px;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    font-size: 12px;
    color: #a0a0b0;
  }

  .ws-indicator {
    color: #ff6b6b;
  }

  .ws-indicator.connected {
    color: #50fa7b;
  }
</style>
