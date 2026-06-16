<script lang="ts">
  import type { Session, Agent, AgentProfile } from '../api'

  let {
    session,
    agent,
    profiles,
    selectedProfile,
    loading,
    error,
    onDeleteSession,
    onRefresh,
    onEnterPlay,
    onBack,
    onSelectProfile,
  }: {
    session: Session | null
    agent: Agent | null
    profiles: AgentProfile[]
    selectedProfile: string
    loading: boolean
    error: string | null
    onDeleteSession: () => void
    onRefresh: () => void
    onEnterPlay: () => void
    onBack: () => void
    onSelectProfile: (profileName: string) => void
  } = $props()

  let canEnterPlay = $derived(selectedProfile !== '' && !loading)

  function formatTime(t: string | undefined): string {
    if (!t) return '—'
    return new Date(t).toLocaleString()
  }
</script>

<div class="session-detail">
  <div class="detail-header">
    <button class="btn btn-small" onclick={onBack}>Back</button>
    <span class="detail-title">Session Detail</span>
  </div>

  <div class="detail-body">
    {#if !session}
      <div class="detail-empty">No session selected</div>
    {:else}
      <!-- Session Info (always visible) -->
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

      <!-- Profile selector (always visible — adapter is created on-demand at first message) -->
      <div class="detail-section" data-testid="profile-selector">
        <div class="section-label">Agent Profile</div>
        <div class="profile-row">
          <select
            class="profile-select"
            value={selectedProfile}
            onchange={(e) => onSelectProfile((e.target as HTMLSelectElement).value)}
            disabled={loading}
          >
            <option value="" disabled>Select a profile...</option>
            {#each profiles as profile}
              <option value={profile.agentProfileName}>{profile.name || profile.agentProfileName}</option>
            {/each}
          </select>
        </div>
      </div>

      <!-- Active agent info (optional — populated by GetAgent after first message) -->
      {#if agent}
        <div class="detail-section" data-testid="agent-summary">
          <div class="section-label">Agent</div>
          <div class="info-grid">
            <span class="info-key">Name</span>
            <span class="info-value">{agent.name ?? '—'}</span>
            <span class="info-key">Profile</span>
            <span class="info-value">{agent.agentProfileName || '—'}</span>
            <span class="info-key">Created</span>
            <span class="info-value">{formatTime(agent.createTime)}</span>
          </div>
        </div>
      {/if}

      <div class="detail-section">
        <button class="btn btn-primary enter-play-btn" onclick={onEnterPlay} disabled={!canEnterPlay}>
          Enter Play
        </button>
      </div>

      <div class="detail-section">
        <div class="ops-buttons">
          <button class="btn" onclick={onRefresh} disabled={loading}>Refresh</button>
        </div>
      </div>

      <!-- Danger Zone (always visible when session selected) -->
      <div class="detail-section">
        <div class="section-label">Danger Zone</div>
        <button class="btn btn-danger" onclick={onDeleteSession} disabled={loading}>
          Delete Session
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

  .profile-row {
    display: flex;
    gap: 8px;
  }

  .profile-select {
    flex: 1;
    padding: 6px 8px;
    font-size: 12px;
    font-family: inherit;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
    cursor: pointer;
  }

  .profile-select:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .profile-select option {
    background: #1a1a2e;
    color: #e0e0e0;
  }

  .enter-play-btn {
    width: 100%;
  }

  .btn-danger {
    background: rgba(255, 107, 107, 0.15);
    color: #ff6b6b;
    border: 1px solid rgba(255, 107, 107, 0.3);
    width: 100%;
  }

  .btn-danger:hover {
    background: rgba(255, 107, 107, 0.25);
  }
</style>
