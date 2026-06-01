<script lang="ts">
  import type { Session } from '../api'

  let {
    sessions,
    selectedSessionId,
    loading,
    error,
    onSelect,
    onRefresh,
    onCreate,
    onDelete
  }: {
    sessions: Session[]
    selectedSessionId: string | null
    loading: boolean
    error: string | null
    onSelect: (session: Session) => void
    onRefresh: () => void
    onCreate: () => void
    onDelete: (sessionId: string) => void
  } = $props()

  function displayName(session: Session): string {
    return session.name || session.sessionId
  }

  function formatTime(t: string): string {
    return new Date(t).toLocaleString()
  }
</script>

<div class="session-list">
  <div class="session-header">
    <span class="session-title">Sessions ({sessions.length})</span>
    <div class="session-actions">
      <button class="btn btn-small" onclick={onRefresh} disabled={loading}>Refresh</button>
      <button class="btn btn-small btn-primary" onclick={onCreate} disabled={loading}>Create</button>
      <button
        class="btn btn-small"
        onclick={() => selectedSessionId && onDelete(selectedSessionId)}
        disabled={loading || !selectedSessionId}
      >
        Delete
      </button>
    </div>
  </div>

  <div class="session-container">
    {#if loading}
      <div class="session-empty">Loading sessions...</div>
    {:else if error}
      <div class="session-error">{error}</div>
    {:else if sessions.length === 0}
      <div class="session-empty">No sessions found. Click Create to add one.</div>
    {:else}
      {#each sessions as session (session.sessionId)}
        <div
          class="session-row"
          class:selected={session.sessionId === selectedSessionId}
          role="button"
          tabindex="0"
          onclick={() => onSelect(session)}
          onkeydown={(e) => e.key === 'Enter' && onSelect(session)}
        >
          <span class="session-name">{displayName(session)}</span>
          <span class="session-time">{formatTime(session.createTime)}</span>
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .session-list {
    display: flex;
    flex-direction: column;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    overflow: hidden;
    height: 100%;
  }

  .session-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 10px;
    border-bottom: 1px solid #0f3460;
    font-size: 12px;
    color: #a0a0b0;
  }

  .session-title {
    font-weight: 600;
  }

  .session-actions {
    display: flex;
    gap: 4px;
  }

  .session-container {
    flex: 1;
    overflow-y: auto;
    padding: 4px;
  }

  .session-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 8px;
    border-bottom: 1px solid #1a1a3e;
    cursor: pointer;
    border-radius: 3px;
  }

  .session-row:hover {
    background: #1a1a3e;
  }

  .session-row.selected {
    background: #0f3460;
    border-color: #1a4a80;
  }

  .session-row.selected:hover {
    background: #1a4a80;
  }

  .session-name {
    font-size: 12px;
    color: #e0e0e0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .session-time {
    font-size: 11px;
    color: #606080;
    flex-shrink: 0;
    margin-left: 8px;
  }

  .session-empty {
    padding: 20px;
    text-align: center;
    color: #606080;
    font-size: 12px;
  }

  .session-error {
    padding: 20px;
    text-align: center;
    color: #ff6b6b;
    font-size: 12px;
  }
</style>
