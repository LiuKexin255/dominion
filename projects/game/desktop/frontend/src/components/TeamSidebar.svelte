<script lang="ts">
  import type { Team, TeamAgent } from '../api'

  let {
    team,
    connectionState,
    selectedAgent,
    onSelectAgent,
    onDeleteSession,
    onBack,
    loading = false,
    onRefresh = () => {},
    refreshing = false,
  }: {
    team: Team | null
    connectionState: 'disconnected' | 'connecting' | 'connected' | 'error'
    selectedAgent: string
    onSelectAgent: (agentName: string) => void
    onDeleteSession: () => void
    onBack: () => void
    loading?: boolean
    onRefresh?: () => void
    refreshing?: boolean
  } = $props()

  let connected = $derived(connectionState === 'connected')

  // The agent list comes from the backend Team.agents (typed TeamAgent[]) —
  // never hardcoded (FR-025). acceptsUserInput marks the agent's input
  // capability (FR-031); planner (saolei) is observe-only (FR-032).
  function inputCapability(agent: TeamAgent): string {
    return agent.acceptsUserInput ? 'input' : 'observe'
  }
</script>

<div class="team-sidebar" data-testid="team-sidebar">
  <div class="sidebar-section">
    <button class="btn back-btn" onclick={onBack} disabled={loading}>← Back to Sessions</button>
  </div>

  <div class="sidebar-section">
    <div class="section-label">Team</div>
    <div class="info-row">
      <span class="info-key">Session</span>
      <span class="info-value" data-testid="team-session-id">{team?.sessionId ?? '—'}</span>
    </div>
    <div class="info-row">
      <span class="info-key">Agents</span>
      <span class="info-value" data-testid="team-agent-count">{team?.agents?.length ?? 0}</span>
    </div>
  </div>

  <div class="sidebar-section">
    <div class="section-label">Agents</div>
    {#if !team}
      <div class="empty-hint">Team not loaded.</div>
    {:else if team.agents.length === 0}
      <div class="empty-hint">No agents in team.</div>
    {:else}
      <div class="agent-list">
        {#each team.agents as agent (agent.name)}
          <button
            class="agent-row"
            class:selected={agent.name === selectedAgent}
            onclick={() => onSelectAgent(agent.name)}
            data-testid="team-agent-row"
          >
            <span class="agent-name">{agent.name}</span>
            <span
              class="agent-capability"
              class:cap-input={agent.acceptsUserInput}
              class:cap-observe={!agent.acceptsUserInput}
              title={agent.acceptsUserInput ? 'Accepts user input (FR-031)' : 'Observe only — input blocked (FR-032)'}
            >
              {inputCapability(agent)}
            </span>
          </button>
        {/each}
      </div>
    {/if}
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
    <button
      class="btn refresh-btn"
      data-testid="team-refresh-btn"
      onclick={onRefresh}
      disabled={refreshing || loading}
    >
      {refreshing ? 'Refreshing…' : 'Refresh Team'}
    </button>
  </div>

  <div class="sidebar-section danger-zone">
    <button class="btn delete-session-btn" onclick={onDeleteSession} disabled={loading}>Delete Session</button>
  </div>
</div>

<style>
  .team-sidebar {
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

  .empty-hint {
    font-size: 12px;
    color: #606080;
    font-style: italic;
  }

  .agent-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .agent-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    font-size: 12px;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    text-align: left;
  }

  .agent-row:hover:not(:disabled) {
    background: #1a4a80;
    border-color: #4a9eff;
  }

  .agent-row.selected {
    border-color: #4a9eff;
    background: rgba(74, 158, 255, 0.15);
  }

  .agent-name {
    word-break: break-all;
  }

  .agent-capability {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .cap-input {
    background: rgba(80, 250, 123, 0.12);
    color: #50fa7b;
  }

  .cap-observe {
    background: rgba(255, 184, 108, 0.12);
    color: #ffb86c;
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

  .refresh-btn {
    width: 100%;
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
