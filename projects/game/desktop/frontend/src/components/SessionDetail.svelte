<script lang="ts">
  import type { Session, Agent } from '../api'

  let {
    session,
    agent,
    wsConnected,
    loading,
    error,
    onCreateAgent,
    onGetAgent,
    onDeleteAgent,
    onConnectAgent,
    onEnterPlay,
    onBack
  }: {
    session: Session | null
    agent: Agent | null
    wsConnected: boolean
    loading: boolean
    error: string | null
    onCreateAgent: () => void
    onGetAgent: () => void
    onDeleteAgent: () => void
    onConnectAgent: () => void
    onEnterPlay: () => void
    onBack: () => void
  } = $props()

  function formatTime(t: string): string {
    return new Date(t).toLocaleString()
  }
</script>

<div class="session-detail">
  <div class="detail-header">
    <button class="btn btn-small" onclick={onBack}>Back</button>
    <span class="detail-title">Session Detail</span>
    <span class="ws-status">
      <span class="ws-dot" class:connected={wsConnected} class:disconnected={!wsConnected}></span>
      <span class="ws-text">{wsConnected ? 'Connected' : 'Disconnected'}</span>
    </span>
  </div>

  <div class="detail-body">
    {#if !session}
      <div class="detail-empty">No session selected</div>
    {:else}
      <!-- Session Info -->
      <div class="detail-section">
        <div class="section-label">Session</div>
        <div class="info-grid">
          <span class="info-key">Session ID</span>
          <span class="info-value">{session.sessionId}</span>
          <span class="info-key">Created</span>
          <span class="info-value">{formatTime(session.createTime)}</span>
        </div>
      </div>

      {#if error}
        <div class="detail-error">{error}</div>
      {/if}

      <!-- Agent Operations -->
      <div class="detail-section">
        <div class="section-label">Agent Operations</div>
        <div class="ops-buttons">
          <button class="btn" onclick={onCreateAgent} disabled={loading}>Create Agent</button>
          <button class="btn" onclick={onGetAgent} disabled={loading}>Get Agent</button>
          <button class="btn" onclick={onDeleteAgent} disabled={loading}>Delete Agent</button>
          <button class="btn" onclick={onConnectAgent} disabled={loading}>Connect Agent</button>
        </div>
      </div>

      <!-- Agent Info -->
      {#if agent}
        <div class="detail-section">
          <div class="section-label">Agent</div>
          <div class="info-grid">
            <span class="info-key">Name</span>
            <span class="info-value">{agent.name}</span>
            <span class="info-key">Owner</span>
            <span class="info-value">{agent.owner}</span>
            <span class="info-key">Owner Index</span>
            <span class="info-value">{agent.ownerIndex}</span>
          </div>
        </div>
      {/if}

      <!-- Play -->
      <div class="detail-section">
        <button class="btn btn-primary enter-play-btn" onclick={onEnterPlay} disabled={loading || !wsConnected}>
          Enter Play
        </button>
      </div>
    {/if}
  </div>
</div>

<style>
  .session-detail {
    display: flex;
    flex-direction: column;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    overflow: hidden;
    height: 100%;
  }

  .detail-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid #0f3460;
    font-size: 12px;
    color: #a0a0b0;
  }

  .detail-title {
    font-weight: 600;
    flex: 1;
  }

  .ws-status {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .ws-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  .ws-dot.connected {
    background: #4caf50;
  }

  .ws-dot.disconnected {
    background: #ff6b6b;
  }

  .ws-text {
    font-size: 11px;
    color: #a0a0b0;
  }

  .detail-body {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .detail-empty {
    padding: 20px;
    text-align: center;
    color: #606080;
    font-size: 12px;
  }

  .detail-error {
    padding: 8px;
    color: #ff6b6b;
    font-size: 12px;
    background: rgba(255, 107, 107, 0.08);
    border-radius: 4px;
    border: 1px solid rgba(255, 107, 107, 0.2);
  }

  .detail-section {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .section-label {
    font-size: 11px;
    font-weight: 600;
    color: #a0a0b0;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .info-grid {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 12px;
    font-size: 12px;
  }

  .info-key {
    color: #606080;
    white-space: nowrap;
  }

  .info-value {
    color: #e0e0e0;
    word-break: break-all;
  }

  .ops-buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .enter-play-btn {
    width: 100%;
  }
</style>
