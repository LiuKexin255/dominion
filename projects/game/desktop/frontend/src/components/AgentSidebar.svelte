<script lang="ts">
  import type { AgentProfile } from '../api'

  let {
    sessionId,
    profiles,
    selectedProfile,
    agentStatus,
    onCreateAgent,
    onSelectProfile,
  }: {
    sessionId: string
    profiles: AgentProfile[]
    selectedProfile: string
    agentStatus: string
    onCreateAgent: (profileName: string) => void
    onSelectProfile: (profileName: string) => void
  } = $props()

  let connected = $derived(agentStatus === 'connected')
  let canCreate = $derived(selectedProfile !== '' && agentStatus !== 'connected')
</script>

<div class="agent-sidebar">
  <!-- Session Info -->
  <div class="sidebar-section">
    <div class="section-label">Session</div>
    <div class="session-id">{sessionId || '—'}</div>
  </div>

  <!-- Connection Status -->
  <div class="sidebar-section">
    <div class="section-label">Connection</div>
    <div class="status-row">
      <span class="status-dot" class:connected class:disconnected={!connected}></span>
      <span class="status-text">{connected ? 'Connected' : 'Disconnected'}</span>
    </div>
  </div>

  <!-- Profile Selection -->
  <div class="sidebar-section">
    <div class="section-label">Agent Profile</div>
    <select
      class="profile-select"
      value={selectedProfile}
      onchange={(e) => onSelectProfile((e.target as HTMLSelectElement).value)}
    >
      <option value="" disabled>Select a profile...</option>
      {#each profiles as profile}
        <option value={profile.agentProfileName}>{profile.name || profile.agentProfileName}</option>
      {/each}
    </select>
  </div>

  <!-- Create Agent -->
  <div class="sidebar-section">
    <button
      class="btn btn-primary create-btn"
      onclick={() => onCreateAgent(selectedProfile)}
      disabled={!canCreate}
    >
      Create Agent
    </button>
  </div>
</div>

<style>
  .agent-sidebar {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 10px;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    min-width: 200px;
    max-width: 240px;
  }

  .sidebar-section {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .section-label {
    font-size: 11px;
    font-weight: 600;
    color: #a0a0b0;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .session-id {
    font-size: 11px;
    color: #606080;
    word-break: break-all;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    line-height: 1.4;
  }

  /* Status */
  .status-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .status-dot.connected {
    background: #50fa7b;
    box-shadow: 0 0 4px rgba(80, 250, 123, 0.5);
  }

  .status-dot.disconnected {
    background: #ff6b6b;
  }

  .status-text {
    font-size: 12px;
    color: #e0e0e0;
  }

  /* Profile Select */
  .profile-select {
    padding: 6px 8px;
    font-size: 12px;
    font-family: inherit;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
    cursor: pointer;
    width: 100%;
  }

  .profile-select:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .profile-select option {
    background: #1a1a2e;
    color: #e0e0e0;
  }

  /* Create Button */
  .create-btn {
    width: 100%;
  }
</style>
