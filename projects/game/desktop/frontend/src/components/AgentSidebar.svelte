<script lang="ts">
  import type { Agent, AgentProfile } from '../api'

  let {
    agent,
    connectionState,
    profiles,
    playState,
  }: {
    agent: Agent | null
    connectionState: 'disconnected' | 'connecting' | 'connected' | 'error'
    profiles: AgentProfile[]
    playState: 'connecting' | 'loading_messages' | 'chat_ready' | 'processing' | 'connection_error' | 'agent_lost'
  } = $props()

  let showProfileDetails = $state(false)

  // TODO(Task 8): once Agent carries agentProfileName, look up the matching profile
  // for model/system prompt/skill names/mcp names. For now, no reliable match is
  // possible since Agent only has name/sessionId/createTime.
  let matchedProfile = $derived<AgentProfile | null>(null)

  let connected = $derived(connectionState === 'connected')

  function toggleProfileDetails() {
    showProfileDetails = !showProfileDetails
  }

  function formatTime(t: string | undefined): string {
    if (!t) return '—'
    return new Date(t).toLocaleString()
  }
</script>

<div class="agent-sidebar" data-testid="agent-sidebar">
  <!-- Agent metadata -->
  <div class="sidebar-section">
    <div class="section-label">Agent</div>
    <div class="info-row">
      <span class="info-key">Name</span>
      <span class="info-value" data-testid="agent-name">{agent?.name ?? '—'}</span>
    </div>
    <div class="info-row">
      <span class="info-key">Profile</span>
      <!-- TODO(Task 8): display agent.agentProfileName once the field exists -->
      <span class="info-value" data-testid="agent-profile-name">{matchedProfile?.name ?? '—'}</span>
    </div>
    <div class="info-row">
      <span class="info-key">Model</span>
      <!-- TODO(Task 8): look up model from matched profile -->
      <span class="info-value" data-testid="agent-model">{matchedProfile?.model ?? '—'}</span>
    </div>
    {#if agent}
      <div class="info-row">
        <span class="info-key">Created</span>
        <span class="info-value">{formatTime(agent.createTime)}</span>
      </div>
    {/if}
  </div>

  <!-- Connection Status -->
  <div class="sidebar-section">
    <div class="section-label">Connection</div>
    <div class="status-row">
      <span class="status-dot" class:connected class:disconnected={!connected}></span>
      <span class="status-text" data-testid="connection-status">
        {#if connected}Connected{:else}{connectionState}{/if}
      </span>
    </div>
  </div>

  <!-- View Profile (expandable read-only details per FR-005) -->
  <div class="sidebar-section">
    <button class="btn view-profile-btn" onclick={toggleProfileDetails}>
      {showProfileDetails ? 'Hide Profile' : 'View Profile'}
    </button>
    {#if showProfileDetails}
      <div class="profile-details" data-testid="profile-details">
        <!-- TODO(Task 8): populate with real profile data via GetAgentProfile -->
        <div class="info-row">
          <span class="info-key">Enabled</span>
          <span class="info-value">{matchedProfile ? (matchedProfile.enabled ? 'Yes' : 'No') : '—'}</span>
        </div>
        <div class="profile-field">
          <span class="info-key">System Prompt</span>
          <pre class="profile-text">{matchedProfile?.systemPrompt ?? '—'}</pre>
        </div>
        <div class="info-row">
          <span class="info-key">Skills</span>
          <span class="info-value">{matchedProfile?.skillNames?.join(', ') ?? '—'}</span>
        </div>
        <div class="info-row">
          <span class="info-key">MCPs</span>
          <span class="info-value">{matchedProfile?.mcpNames?.join(', ') ?? '—'}</span>
        </div>
      </div>
    {/if}
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

  /* View Profile button */
  .view-profile-btn {
    width: 100%;
  }

  /* Profile details expandable */
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
</style>
