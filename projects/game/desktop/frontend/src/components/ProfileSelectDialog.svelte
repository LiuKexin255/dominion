<script lang="ts">
  import type { TeamProfile } from '../api'

  // Profile selection modal shown when entering a session whose Team does not
  // exist yet: the user picks the TeamProfile to create it with (replacing the
  // former hardcoded `default` profile auto-creation). onSelect receives the
  // TeamProfile full resource name (TeamProfile.name) for CreateTeam.
  let {
    profiles,
    loading,
    error,
    onSelect,
    onCancel,
    onRefresh,
    onGoToProfiles,
  }: {
    profiles: TeamProfile[]
    loading: boolean
    error: string | null
    onSelect: (profileFullName: string) => void
    onCancel: () => void
    onRefresh: () => void
    onGoToProfiles: () => void
  } = $props()

  // selectedName is the chosen TeamProfile full resource name ('' = none).
  // The `default` profile is pre-selected when present (the desktop's
  // conventional default); user clicks override it. The component instance is
  // re-created on each dialog open ({#if showProfileSelect}), so the
  // pre-selection restarts fresh per entry.
  let selectedName = $state('')

  $effect(() => {
    if (selectedName) return
    const def = profiles.find(p => p.profileName === 'default')
    if (def) selectedName = def.name
  })

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onCancel()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div
  class="profile-select-overlay"
  onclick={(e) => {
    if (e.target === e.currentTarget) onCancel()
  }}
  role="button"
  tabindex={0}
  data-testid="profile-select-dialog"
>
  <div class="profile-select-panel">
    <div class="profile-select-title">Select Agent Profile</div>
    <div class="profile-select-subtitle">
      No team exists for this session yet. Choose the profile used to create it.
    </div>

    {#if loading}
      <div class="profile-select-status">Loading profiles...</div>
    {:else if error}
      <div class="profile-select-error">
        <span class="profile-select-status-msg">{error}</span>
        <button class="btn btn-small" onclick={onRefresh} disabled={loading}>Retry</button>
      </div>
    {:else if profiles.length === 0}
      <div class="profile-select-empty">
        <div class="profile-select-status-msg">No profiles available for this template.</div>
        <div class="profile-select-empty-hint">Create one in the Team Profile Management page first.</div>
        <button class="btn btn-small" onclick={onGoToProfiles} data-testid="profile-select-go-profiles">
          Go to Profile Management
        </button>
      </div>
    {:else}
      <div class="profile-select-list">
        {#each profiles as profile (profile.name)}
          <button
            type="button"
            class="profile-select-option"
            class:selected={selectedName === profile.name}
            onclick={() => (selectedName = profile.name)}
            data-testid="profile-option"
          >
            <span class="profile-select-name">{profile.profileName}</span>
            <span class="profile-select-meta">
              {#if profile.playerModel}
                <span class="profile-select-badge profile-select-badge-player">player: {profile.playerModel}</span>
              {/if}
              {#if profile.plannerModel}
                <span class="profile-select-badge profile-select-badge-planner">planner: {profile.plannerModel}</span>
              {/if}
            </span>
          </button>
        {/each}
      </div>
    {/if}

    <div class="profile-select-actions">
      <button class="btn" onclick={onCancel} data-testid="profile-select-cancel">Cancel</button>
      <button
        class="btn btn-primary"
        onclick={() => selectedName && onSelect(selectedName)}
        disabled={!selectedName || loading}
        data-testid="profile-select-confirm"
      >
        Confirm
      </button>
    </div>
  </div>
</div>

<style>
  .profile-select-overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.85);
    z-index: 1000;
    cursor: pointer;
  }

  .profile-select-panel {
    width: min(520px, 90vw);
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px;
    background: #16213e;
    border: 1px solid #0f3460;
    border-radius: 8px;
    cursor: default;
  }

  .profile-select-title {
    font-size: 14px;
    font-weight: 600;
    color: #e0e0e0;
  }

  .profile-select-subtitle {
    font-size: 11px;
    color: #a0a0b0;
  }

  .profile-select-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-y: auto;
    min-height: 0;
  }

  .profile-select-option {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
    text-align: left;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    cursor: pointer;
    color: #e0e0e0;
  }

  .profile-select-option:hover {
    border-color: #4a9eff;
  }

  .profile-select-option.selected {
    border-color: #4a9eff;
    background: #1a4a80;
  }

  .profile-select-name {
    font-size: 13px;
    font-weight: 600;
  }

  .profile-select-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .profile-select-badge {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    white-space: nowrap;
  }

  .profile-select-badge-player {
    background: rgba(74, 158, 255, 0.12);
    color: #4a9eff;
  }

  .profile-select-badge-planner {
    background: rgba(80, 250, 123, 0.12);
    color: #50fa7b;
  }

  .profile-select-status,
  .profile-select-empty {
    padding: 12px;
    text-align: center;
    color: #a0a0b0;
    font-size: 12px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
  }

  .profile-select-error {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px;
    background: rgba(255, 107, 107, 0.08);
    border-radius: 4px;
    border: 1px solid rgba(255, 107, 107, 0.2);
  }

  .profile-select-status-msg {
    flex: 1;
    color: #ff6b6b;
    font-size: 12px;
    text-align: left;
  }

  .profile-select-empty-hint {
    font-size: 11px;
    color: #606080;
  }

  .profile-select-actions {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }
</style>
