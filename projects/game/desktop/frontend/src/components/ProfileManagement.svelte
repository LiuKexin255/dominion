<script lang="ts">
  import type { AgentProfile, CreateAgentProfileRequest } from '../api'

  let {
    profiles,
    loading,
    error,
    onCreate,
    onDelete,
    onRefresh,
    onBack,
    onUpdate = async () => {},
  }: {
    profiles: AgentProfile[]
    loading: boolean
    error: string | null
    onCreate: (req: CreateAgentProfileRequest) => Promise<void>
    onDelete: (agentProfileName: string) => Promise<void>
    onRefresh: () => Promise<void>
    onBack: () => void
    onUpdate?: (
      agentProfileName: string,
      profile: AgentProfile,
      updateMaskPaths: string[],
    ) => Promise<void>
  } = $props()

  let showCreateForm = $state(false)
  let formName = $state('')
  let formModel = $state('')
  let formSystemPrompt = $state('')
  let formEnabled = $state(true)
  let formToolNames = $state('')
  let createError = $state<string | null>(null)
  let creating = $state(false)

  let confirmDeleteName = $state<string | null>(null)
  let deleteErrors = $state<Record<string, string>>({})
  let deleting = $state<string | null>(null)

  let editingName = $state<string | null>(null)
  let editModel = $state('')
  let editSystemPrompt = $state('')
  let editEnabled = $state(true)
  let editToolNames = $state('')
  let editError = $state<string | null>(null)
  let saving = $state(false)

  let canSubmit = $derived(formName.trim() !== '' && !creating)
  let canSave = $derived(!saving)

  function toggleCreateForm() {
    showCreateForm = !showCreateForm
    createError = null
  }

  function resetForm() {
    formName = ''
    formModel = ''
    formSystemPrompt = ''
    formEnabled = true
    formToolNames = ''
  }

  function cancelCreateForm() {
    showCreateForm = false
    createError = null
    resetForm()
  }

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault()
    if (!canSubmit) return
    creating = true
    createError = null
    try {
      await onCreate({
        agentProfileName: formName.trim(),
        model: formModel.trim() || undefined,
        systemPrompt: formSystemPrompt || undefined,
        enabled: formEnabled,
        toolNames: formToolNames.split(',').map((s) => s.trim()).filter(Boolean),
      })
      resetForm()
      showCreateForm = false
    } catch (err) {
      createError = err instanceof Error ? err.message : 'Failed to create profile'
    } finally {
      creating = false
    }
  }

  function startEdit(profile: AgentProfile) {
    editingName = profile.agentProfileName
    editModel = profile.model || ''
    editSystemPrompt = profile.systemPrompt || ''
    editEnabled = profile.enabled
    editToolNames = profile.toolNames?.join(', ') || ''
    editError = null
  }

  function cancelEdit() {
    editingName = null
    editError = null
  }

  async function handleEditSubmit(e: SubmitEvent, profile: AgentProfile) {
    e.preventDefault()
    if (!canSave) return
    saving = true
    editError = null
    try {
      const updated: AgentProfile = {
        ...profile,
        model: editModel,
        systemPrompt: editSystemPrompt,
        enabled: editEnabled,
        toolNames: editToolNames.split(',').map((s) => s.trim()).filter(Boolean),
      }
      await onUpdate(profile.agentProfileName, updated, [
        'model',
        'system_prompt',
        'tool_names',
        'enabled',
      ])
      editingName = null
    } catch (err) {
      editError = err instanceof Error ? err.message : String(err)
    } finally {
      saving = false
    }
  }

  function startDelete(agentProfileName: string) {
    confirmDeleteName = agentProfileName
    delete deleteErrors[agentProfileName]
  }

  function cancelDelete() {
    confirmDeleteName = null
  }

  async function confirmDeleteProfile(agentProfileName: string) {
    deleting = agentProfileName
    try {
      await onDelete(agentProfileName)
      confirmDeleteName = null
    } catch (err) {
      deleteErrors[agentProfileName] = err instanceof Error ? err.message : 'Failed to delete profile'
    } finally {
      deleting = null
    }
  }

  function formatTime(t?: string): string {
    if (!t) return '—'
    return new Date(t).toLocaleString()
  }

  function truncatePrompt(prompt: string): string {
    if (!prompt) return ''
    const firstLine = prompt.split('\n')[0]
    return firstLine.length > 80 ? firstLine.slice(0, 80) + '\u2026' : firstLine
  }
</script>

<div class="profile-management">
  <div class="detail-header">
    <button class="btn btn-small" onclick={onBack}>Back</button>
    <span class="detail-title">Agent Profiles</span>
  </div>

  <div class="detail-body">
    <!-- Create Form Section -->
    <div class="detail-section">
      <div class="create-actions">
        <button class="btn btn-primary" onclick={toggleCreateForm}>
          {showCreateForm ? 'Close' : 'New Profile'}
        </button>
      </div>

      {#if showCreateForm}
        <form class="create-form" onsubmit={handleSubmit}>
          <div class="section-label">Create Profile</div>

          {#if createError}
            <div class="detail-error">{createError}</div>
          {/if}

          <div class="form-field">
            <label for="profile-name">Agent Profile Name</label>
            <input
              id="profile-name"
              type="text"
              bind:value={formName}
              placeholder="e.g., default-agent"
              required
            />
          </div>

          <div class="form-field">
            <label for="profile-model">Model</label>
            <input
              id="profile-model"
              type="text"
              bind:value={formModel}
              placeholder="e.g., opencode-go/deepseek-v4-pro (optional)"
            />
          </div>

          <div class="form-field">
            <label for="profile-prompt">System Prompt</label>
            <textarea
              id="profile-prompt"
              bind:value={formSystemPrompt}
              rows="4"
              placeholder="Optional system prompt..."
            ></textarea>
          </div>

          <div class="form-field form-check">
            <label for="profile-enabled">
              <input id="profile-enabled" type="checkbox" bind:checked={formEnabled} />
              Enabled
            </label>
          </div>

          <div class="form-field">
            <label for="profile-tools">Tool Names</label>
            <input
              id="profile-tools"
              type="text"
              bind:value={formToolNames}
              placeholder="comma-separated, e.g. mouse,keyboard"
            />
          </div>

          <div class="form-buttons">
            <button type="submit" class="btn btn-primary" disabled={!canSubmit}>
              {creating ? 'Creating...' : 'Create'}
            </button>
            <button type="button" class="btn" onclick={cancelCreateForm}>Cancel</button>
          </div>
        </form>
      {/if}
    </div>

    <!-- Profile List Section -->
    <div class="detail-section">
      <div class="section-label">Profiles</div>

      {#if error}
        <div class="list-error">
          <span class="list-error-msg">{error}</span>
          <button class="btn btn-small" onclick={() => onRefresh()} disabled={loading}>
            Retry
          </button>
        </div>
      {/if}

      {#if profiles.length === 0}
        <div class="detail-empty">No profiles yet. Create one above.</div>
      {:else}
        <div class="profile-list">
          {#each profiles as profile (profile.agentProfileName)}
            <div class="profile-card" class:dimmed={!profile.enabled}>
              <div class="profile-header">
                <div class="profile-names">
                  <span class="profile-primary">{profile.agentProfileName}</span>
                  <span class="profile-secondary">{profile.name || profile.agentProfileName}</span>
                </div>
                <div class="profile-actions">
                  {#if confirmDeleteName === profile.agentProfileName}
                    <span class="confirm-text">Delete this profile?</span>
                    <button
                      class="btn btn-small btn-danger"
                      onclick={() => confirmDeleteProfile(profile.agentProfileName)}
                      disabled={deleting === profile.agentProfileName}
                    >
                      {deleting === profile.agentProfileName ? 'Deleting...' : 'Confirm'}
                    </button>
                    <button
                      class="btn btn-small"
                      onclick={cancelDelete}
                      disabled={deleting === profile.agentProfileName}
                    >
                      Cancel
                    </button>
                  {:else}
                    <button
                      class="btn btn-small"
                      onclick={() => startEdit(profile)}
                      disabled={deleting !== null || editingName !== null}
                    >
                      Edit
                    </button>
                    <button
                      class="btn btn-small btn-danger"
                      onclick={() => startDelete(profile.agentProfileName)}
                      disabled={deleting !== null || editingName !== null}
                    >
                      Delete
                    </button>
                  {/if}
                </div>
              </div>

              <div class="profile-meta">
                {#if profile.model}
                  <span class="badge badge-model">{profile.model}</span>
                {/if}
                <span
                  class="badge"
                  class:badge-enabled={profile.enabled}
                  class:badge-disabled={!profile.enabled}
                >
                  {profile.enabled ? 'Enabled' : 'Disabled'}
                </span>
                {#if profile.toolNames?.length > 0}
                  <span class="badge badge-tools">tools: {profile.toolNames.join(', ')}</span>
                {/if}
              </div>

              {#if profile.systemPrompt}
                <div class="profile-prompt">{truncatePrompt(profile.systemPrompt)}</div>
              {:else}
                <div class="profile-prompt profile-prompt-empty">No system prompt</div>
              {/if}

              <div class="profile-time">
                Created: {formatTime(profile.createTime)}
              </div>

              {#if editingName === profile.agentProfileName}
                <form class="edit-form" onsubmit={(e) => handleEditSubmit(e, profile)}>
                  <div class="section-label">Edit Profile</div>

                  {#if editError}
                    <div class="detail-error">{editError}</div>
                  {/if}

                  <div class="form-field">
                    <label for={`edit-model-${profile.agentProfileName}`}>Model</label>
                    <input
                      id={`edit-model-${profile.agentProfileName}`}
                      type="text"
                      bind:value={editModel}
                      placeholder="e.g., opencode-go/deepseek-v4-pro (optional)"
                    />
                  </div>

                  <div class="form-field">
                    <label for={`edit-prompt-${profile.agentProfileName}`}>System Prompt</label>
                    <textarea
                      id={`edit-prompt-${profile.agentProfileName}`}
                      bind:value={editSystemPrompt}
                      rows="4"
                      placeholder="Optional system prompt..."
                    ></textarea>
                  </div>

                  <div class="form-field form-check">
                    <label for={`edit-enabled-${profile.agentProfileName}`}>
                      <input
                        id={`edit-enabled-${profile.agentProfileName}`}
                        type="checkbox"
                        bind:checked={editEnabled}
                      />
                      Enabled
                    </label>
                  </div>

                  <div class="form-field">
                    <label for={`edit-tools-${profile.agentProfileName}`}>Tool Names</label>
                    <input
                      id={`edit-tools-${profile.agentProfileName}`}
                      type="text"
                      bind:value={editToolNames}
                      placeholder="comma-separated, e.g. mouse,keyboard"
                    />
                  </div>

                  <div class="form-buttons">
                    <button type="submit" class="btn btn-primary" disabled={!canSave}>
                      {saving ? 'Saving...' : 'Save'}
                    </button>
                    <button type="button" class="btn" onclick={cancelEdit} disabled={saving}>
                      Cancel
                    </button>
                  </div>
                </form>
              {/if}

              {#if deleteErrors[profile.agentProfileName]}
                <div class="detail-error">{deleteErrors[profile.agentProfileName]}</div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .profile-management {
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

  /* Create Form */
  .create-actions {
    display: flex;
    gap: 4px;
  }

  .create-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    background: #0f3460;
    border-radius: 4px;
    border: 1px solid #1a3a6e;
  }

  .form-field {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .form-field label {
    font-size: 11px;
    color: #a0a0b0;
  }

  .form-field input[type='text'],
  .form-field textarea {
    padding: 6px 8px;
    font-size: 12px;
    font-family: inherit;
    background: #1a1a2e;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
    resize: vertical;
  }

  .form-field input[type='text']:focus,
  .form-field textarea:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .form-check {
    flex-direction: row;
    align-items: center;
    gap: 6px;
  }

  .form-check label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: #e0e0e0;
    cursor: pointer;
  }

  .form-check input[type='checkbox'] {
    width: 14px;
    height: 14px;
    cursor: pointer;
  }

  .form-buttons {
    display: flex;
    gap: 4px;
  }

  /* List Error */
  .list-error {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px;
    background: rgba(255, 107, 107, 0.08);
    border-radius: 4px;
    border: 1px solid rgba(255, 107, 107, 0.2);
  }

  .list-error-msg {
    flex: 1;
    color: #ff6b6b;
    font-size: 12px;
  }

  /* Profile List */
  .profile-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .profile-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
    background: #0f3460;
    border-radius: 4px;
    border: 1px solid #1a3a6e;
  }

  .profile-card.dimmed {
    opacity: 0.6;
  }

  .profile-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 8px;
  }

  .profile-names {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }

  .profile-primary {
    font-size: 13px;
    font-weight: 600;
    color: #e0e0e0;
    word-break: break-all;
  }

  .profile-secondary {
    font-size: 11px;
    color: #a0a0b0;
    word-break: break-all;
  }

  .profile-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
  }

  .confirm-text {
    font-size: 11px;
    color: #ff6b6b;
    white-space: nowrap;
  }

  .profile-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .badge {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    white-space: nowrap;
  }

  .badge-model {
    background: rgba(74, 158, 255, 0.12);
    color: #4a9eff;
  }

  .badge-enabled {
    background: rgba(80, 250, 123, 0.12);
    color: #50fa7b;
  }

  .badge-disabled {
    background: rgba(255, 107, 107, 0.12);
    color: #ff6b6b;
  }

  .badge-tools {
    background: rgba(80, 250, 123, 0.12);
    color: #50fa7b;
  }

  .edit-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    margin-top: 4px;
    background: #1a1a2e;
    border-radius: 4px;
    border: 1px solid #1a3a6e;
  }

  .profile-prompt {
    font-size: 11px;
    color: #a0a0b0;
    line-height: 1.4;
    word-break: break-all;
  }

  .profile-prompt-empty {
    color: #606080;
    font-style: italic;
  }

  .profile-time {
    font-size: 10px;
    color: #606080;
  }

  .btn-danger {
    background: rgba(255, 107, 107, 0.15);
    color: #ff6b6b;
    border: 1px solid rgba(255, 107, 107, 0.3);
  }

  .btn-danger:hover {
    background: rgba(255, 107, 107, 0.25);
  }
</style>
