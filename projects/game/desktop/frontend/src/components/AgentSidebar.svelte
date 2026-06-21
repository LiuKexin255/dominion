<script lang="ts">
  import type { Agent, AgentProfile } from '../api'

  let {
    agent,
    connectionState,
    profiles,
    playState,
    selectedProfile = '',
    onSelectProfile,
    onDeleteSession,
    onBack,
    loading = false,
  }: {
    agent: Agent | null
    connectionState: 'disconnected' | 'connecting' | 'connected' | 'error'
    profiles: AgentProfile[]
    playState: 'connecting' | 'loading_messages' | 'chat_ready' | 'processing' | 'connection_error' | 'agent_lost'
    selectedProfile?: string
    onSelectProfile: (profileName: string) => void
    onDeleteSession: () => void
    onBack: () => void
    loading?: boolean
  } = $props()

  let showProfileDetails = $state(false)

  let activeProfileName = $derived(agent?.agentProfileName || selectedProfile)

  let matchedProfile = $derived<AgentProfile | null>(
    activeProfileName
      ? profiles.find(p => p.agentProfileName === activeProfileName) ?? null
      : null
  )

  let connected = $derived(connectionState === 'connected')

  function toggleProfileDetails() {
    showProfileDetails = !showProfileDetails
  }
</script>

<div class="agent-sidebar" data-testid="agent-sidebar">
  <div class="sidebar-section">
    <button class="btn back-btn" onclick={onBack} disabled={loading}>← Back to Sessions</button>
  </div>

  <div class="sidebar-section">
    <div class="section-label">Agent</div>
    <div class="info-row">
      <span class="info-key">Profile</span>
      <span class="info-value" data-testid="agent-profile-name">{activeProfileName || '—'}</span>
    </div>
    <div class="info-row">
      <span class="info-key">Model</span>
      <span class="info-value" data-testid="agent-model">{matchedProfile?.model ?? '—'}</span>
    </div>
  </div>

  <div class="sidebar-section">
    <div class="section-label">Profile Selector</div>
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

  <div class="sidebar-section">
    <div class="section-label">Connection</div>
    <div class="status-row">
      <span class="status-dot" class:connected class:disconnected={!connected}></span>
      <span class="status-text" data-testid="connection-status">
        {#if connected}Connected{:else}{connectionState}{/if}
      </span>
    </div>
  </div>

  <div class="sidebar-section">
    <button class="btn view-profile-btn" onclick={toggleProfileDetails}>
      {showProfileDetails ? 'Hide Profile' : 'View Profile'}
    </button>
    {#if showProfileDetails}
      <div class="profile-details" data-testid="profile-details">
        <div class="info-row">
          <span class="info-key">Enabled</span>
          <span class="info-value">{matchedProfile ? (matchedProfile.enabled ? 'Yes' : 'No') : '—'}</span>
        </div>
        <div class="profile-field">
          <span class="info-key">System Prompt</span>
          <pre class="profile-text">{matchedProfile?.systemPrompt || '—'}</pre>
        </div>
        <div class="info-row">
          <span class="info-key">Skills</span>
          <span class="info-value">{matchedProfile?.skillNames?.join(', ') || '—'}</span>
        </div>
        <div class="info-row">
          <span class="info-key">MCPs</span>
          <span class="info-value">{matchedProfile?.mcpNames?.join(', ') || '—'}</span>
        </div>
      </div>
    {/if}
  </div>

  <div class="sidebar-section danger-zone">
    <button class="btn delete-session-btn" onclick={onDeleteSession} disabled={loading}>Delete Session</button>
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
    max-width: 260px;
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

  .info-row {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 8px;
    font-size: 12px;
    line-height: 1.4;
  }

  .info-key {
    color: #606080;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .info-value {
    color: #e0e0e0;
    word-break: break-all;
    text-align: right;
  }

  .profile-select {
    width: 100%;
    padding: 6px 8px;
    font-size: 12px;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
  }

  .profile-select:focus {
    outline: none;
    border-color: #4a9eff;
  }

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

  .btn {
    padding: 6px 10px;
    font-size: 12px;
    background: #0f3460;
    border: 1px solid #1a4a80;
    border-radius: 4px;
    color: #ffffff;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .btn:hover:not(:disabled) {
    background: #1a4a80;
    border-color: #4a9eff;
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .back-btn {
    width: 100%;
    text-align: center;
  }

  .view-profile-btn {
    width: 100%;
  }

  .profile-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
  }

  .profile-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .profile-text {
    margin: 0;
    padding: 4px;
    font-size: 11px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    color: #e0e0e0;
    background: #1a1a2e;
    border-radius: 3px;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 120px;
    overflow-y: auto;
  }

  .danger-zone {
    margin-top: auto;
  }

  .delete-session-btn {
    width: 100%;
    background: #3a1520;
    border-color: #6a2540;
    color: #ff6b6b;
  }

  .delete-session-btn:hover:not(:disabled) {
    background: #5a2030;
    border-color: #ff6b6b;
  }
</style>
