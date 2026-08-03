<script lang="ts">
  import type { TeamProfile, CreateTeamProfileRequest } from '../api'
  import { TEMPLATE_SAOLEI } from '../api'

  // ─── Typed oneof variant dispatch (D1) ───────────────────────────────────
  // The profile form is specialized per template by the TeamProfile typed
  // oneof (spec.saolei → SaoleiProfile{player_model, planner_model,
  // player_prompt, planner_prompt} — specs/031-team-template-mode/
  // data-model.md §1.5; the prompts are optional, empty = template default
  // base, FR-034). Dispatch is a TYPED template→variant map — NOT a generic
  // key-value form and NOT a hardcoded template-name rule
  // (specs/031-team-template-mode/contracts/desktop-contract.md §3;
  // spec.md FR-029). A new template adds a typed variant here and a typed
  // form branch below (extension point).
  interface SaoleiProfileVariant {
    kind: 'saolei'
    playerModelLabel: string
    plannerModelLabel: string
    playerPromptLabel: string
    plannerPromptLabel: string
  }

  type ProfileVariant = SaoleiProfileVariant

  const PROFILE_VARIANTS: Record<string, ProfileVariant> = {
    [TEMPLATE_SAOLEI]: {
      kind: 'saolei',
      playerModelLabel: 'Player Model',
      plannerModelLabel: 'Planner Model',
      playerPromptLabel: 'Player Base Prompt',
      plannerPromptLabel: 'Planner Base Prompt',
    },
  }

  // MODEL_OPTIONS is the frontend-hardcoded list of selectable model ids. It
  // has no contract binding with the backend: it MUST stay in sync with the
  // models actually supported by projects/game/agent/src/model-provider.ts
  // (selecting an id the backend does not support fails later CreateTeam
  // model instantiation). The value format is `{provider}/{model}` per
  // specs/031-team-template-mode/data-model.md §1.5. A full solution (backend
  // serving the model list via Config) can be a future optimization.
  const MODEL_OPTIONS: { value: string; label: string }[] = [
    { value: 'opencode-go/glm-5.2', label: 'GLM-5.2' },
    { value: 'opencode-go/glm-5.1', label: 'GLM-5.1' },
    { value: 'opencode-go/kimi-k2.7-code', label: 'Kimi K2.7 Code' },
    { value: 'opencode-go/kimi-k2.6', label: 'Kimi K2.6' },
    { value: 'opencode-go/deepseek-v4-pro', label: 'DeepSeek V4 Pro' },
    { value: 'opencode-go/deepseek-v4-flash', label: 'DeepSeek V4 Flash' },
    { value: 'opencode-go/mimo-v2.5', label: 'MiMo V2.5' },
    { value: 'opencode-go/mimo-v2.5-pro', label: 'MiMo V2.5 Pro' },
    { value: 'opencode-go/minimax-m3', label: 'MiniMax M3' },
    { value: 'opencode-go/minimax-m2.7', label: 'MiniMax M2.7' },
    { value: 'opencode-go/minimax-m2.5', label: 'MiniMax M2.5' },
    { value: 'opencode-go/qwen3.7-max', label: 'Qwen3.7 Max' },
    { value: 'opencode-go/qwen3.7-plus', label: 'Qwen3.7 Plus' },
    { value: 'opencode-go/qwen3.6-plus', label: 'Qwen3.6 Plus' },
  ]

  // The variant active for the current template; null when the template has
  // no typed profile variant (currently saolei only).
  let {
    template,
    profiles,
    loading,
    error,
    onCreate,
    onDelete,
    onRefresh,
    onBack,
    onUpdate = async () => {},
  }: {
    template: string
    profiles: TeamProfile[]
    loading: boolean
    error: string | null
    onCreate: (req: CreateTeamProfileRequest) => Promise<void>
    onDelete: (profileName: string) => Promise<void>
    onRefresh: () => Promise<void>
    onBack: () => void
    onUpdate?: (
      profileName: string,
      profile: TeamProfile,
      updateMaskPaths: string[],
    ) => Promise<void>
  } = $props()

  let variant = $derived(PROFILE_VARIANTS[template] ?? null)

  let showCreateForm = $state(false)
  let formName = $state('')
  let formPlayerModel = $state('')
  let formPlannerModel = $state('')
  let formPlayerPrompt = $state('')
  let formPlannerPrompt = $state('')
  let createError = $state<string | null>(null)
  let creating = $state(false)

  let confirmDeleteName = $state<string | null>(null)
  let deleteErrors = $state<Record<string, string>>({})
  let deleting = $state<string | null>(null)

  let editingName = $state<string | null>(null)
  let editPlayerModel = $state('')
  let editPlannerModel = $state('')
  let editPlayerPrompt = $state('')
  let editPlannerPrompt = $state('')
  let editError = $state<string | null>(null)
  let saving = $state(false)

  // canSubmit/canSave validate only the required fields — the base prompts
  // are OPTIONAL (empty = template default base, FR-034), so they must not
  // gate submission.
  let canSubmit = $derived(
    formName.trim() !== '' && formPlayerModel !== '' && formPlannerModel !== '' && !creating,
  )
  let canSave = $derived(editPlayerModel !== '' && editPlannerModel !== '' && !saving)

  function toggleCreateForm() {
    showCreateForm = !showCreateForm
    createError = null
  }

  function resetForm() {
    formName = ''
    formPlayerModel = ''
    formPlannerModel = ''
    formPlayerPrompt = ''
    formPlannerPrompt = ''
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
        profileName: formName.trim(),
        playerModel: formPlayerModel,
        plannerModel: formPlannerModel,
        playerPrompt: formPlayerPrompt,
        plannerPrompt: formPlannerPrompt,
      })
      resetForm()
      showCreateForm = false
    } catch (err) {
      createError = err instanceof Error ? err.message : 'Failed to create profile'
    } finally {
      creating = false
    }
  }

  function startEdit(profile: TeamProfile) {
    editingName = profile.profileName
    editPlayerModel = profile.playerModel ?? ''
    editPlannerModel = profile.plannerModel ?? ''
    editPlayerPrompt = profile.playerPrompt ?? ''
    editPlannerPrompt = profile.plannerPrompt ?? ''
    editError = null
  }

  function cancelEdit() {
    editingName = null
    editError = null
  }

  async function handleEditSubmit(e: SubmitEvent, profile: TeamProfile) {
    e.preventDefault()
    if (!canSave) return
    saving = true
    editError = null
    try {
      const updated: TeamProfile = {
        ...profile,
        playerModel: editPlayerModel,
        plannerModel: editPlannerModel,
        playerPrompt: editPlayerPrompt,
        plannerPrompt: editPlannerPrompt,
      }
      // oneof-member update_mask paths (AIP-161): the prompt service applies
      // saolei.player_model / saolei.planner_model / saolei.player_prompt /
      // saolei.planner_prompt (T007 — specs/031-team-template-mode/tasks.md
      // Phase 3; FR-034). The desktop form submits the full saolei form, so
      // all four paths are in the mask.
      await onUpdate(profile.profileName, updated, [
        'saolei.player_model',
        'saolei.planner_model',
        'saolei.player_prompt',
        'saolei.planner_prompt',
      ])
      editingName = null
    } catch (err) {
      editError = err instanceof Error ? err.message : 'Failed to update profile'
    } finally {
      saving = false
    }
  }

  function startDelete(profileName: string) {
    confirmDeleteName = profileName
    delete deleteErrors[profileName]
  }

  function cancelDelete() {
    confirmDeleteName = null
  }

  async function confirmDeleteProfile(profileName: string) {
    deleting = profileName
    try {
      await onDelete(profileName)
      confirmDeleteName = null
    } catch (err) {
      deleteErrors[profileName] = err instanceof Error ? err.message : 'Failed to delete profile'
    } finally {
      deleting = null
    }
  }

  function formatTime(t?: string): string {
    if (!t) return '—'
    return new Date(t).toLocaleString()
  }
</script>

<div class="profile-management">
  <div class="detail-header">
    <button class="btn btn-small" onclick={onBack}>Back</button>
    <span class="detail-title">Team Profiles · {template}</span>
  </div>

  <div class="detail-body">
    {#if variant && variant.kind === 'saolei'}
      <!-- Create Form Section (saolei variant: player/planner model only —
           FR-027; tools/mcp are template-fixed, FR-028) -->
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
              <label for="profile-name">Profile Name</label>
              <input
                id="profile-name"
                type="text"
                bind:value={formName}
                placeholder="e.g., default"
                required
              />
            </div>

            <div class="form-field">
              <label for="profile-player-model">{variant.playerModelLabel}</label>
              <select id="profile-player-model" bind:value={formPlayerModel}>
                <option value="" disabled selected={formPlayerModel === ''}>Select model...</option>
                {#each MODEL_OPTIONS as opt}
                  <option value={opt.value}>{opt.label}</option>
                {/each}
              </select>
            </div>

            <div class="form-field">
              <label for="profile-planner-model">{variant.plannerModelLabel}</label>
              <select id="profile-planner-model" bind:value={formPlannerModel}>
                <option value="" disabled selected={formPlannerModel === ''}>Select model...</option>
                {#each MODEL_OPTIONS as opt}
                  <option value={opt.value}>{opt.label}</option>
                {/each}
              </select>
            </div>

            <div class="form-field">
              <label for="profile-player-prompt">{variant.playerPromptLabel}</label>
              <textarea
                id="profile-player-prompt"
                rows="3"
                bind:value={formPlayerPrompt}
                placeholder="Optional — leave empty to use the template default base prompt (FR-034)"
              ></textarea>
            </div>

            <div class="form-field">
              <label for="profile-planner-prompt">{variant.plannerPromptLabel}</label>
              <textarea
                id="profile-planner-prompt"
                rows="3"
                bind:value={formPlannerPrompt}
                placeholder="Optional — leave empty to use the template default base prompt (FR-034)"
              ></textarea>
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
    {/if}

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
          {#each profiles as profile (profile.profileName)}
            <div class="profile-card">
              <div class="profile-header">
                <div class="profile-names">
                  <span class="profile-primary">{profile.profileName}</span>
                  <span class="profile-secondary">{profile.name}</span>
                </div>
                <div class="profile-actions">
                  {#if confirmDeleteName === profile.profileName}
                    <span class="confirm-text">Delete this profile?</span>
                    <button
                      class="btn btn-small btn-danger"
                      onclick={() => confirmDeleteProfile(profile.profileName)}
                      disabled={deleting === profile.profileName}
                    >
                      {deleting === profile.profileName ? 'Deleting...' : 'Confirm'}
                    </button>
                    <button
                      class="btn btn-small"
                      onclick={cancelDelete}
                      disabled={deleting === profile.profileName}
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
                      onclick={() => startDelete(profile.profileName)}
                      disabled={deleting !== null || editingName !== null}
                    >
                      Delete
                    </button>
                  {/if}
                </div>
              </div>

              <div class="profile-meta">
                {#if profile.playerModel}
                  <span class="badge badge-model">player: {profile.playerModel}</span>
                {/if}
                {#if profile.plannerModel}
                  <span class="badge badge-planner">planner: {profile.plannerModel}</span>
                {/if}
              </div>

              <div class="profile-time">
                Created: {formatTime(profile.createTime)}
              </div>

              {#if variant && variant.kind === 'saolei' && editingName === profile.profileName}
                <form class="edit-form" onsubmit={(e) => handleEditSubmit(e, profile)}>
                  <div class="section-label">Edit Profile</div>

                  {#if editError}
                    <div class="detail-error">{editError}</div>
                  {/if}

                  <div class="form-field">
                    <label for={`edit-player-model-${profile.profileName}`}>{variant.playerModelLabel}</label>
                    <select id={`edit-player-model-${profile.profileName}`} bind:value={editPlayerModel}>
                      <option value="" disabled selected={editPlayerModel === ''}>Select model...</option>
                      {#each MODEL_OPTIONS as opt}
                        <option value={opt.value}>{opt.label}</option>
                      {/each}
                    </select>
                  </div>

                  <div class="form-field">
                    <label for={`edit-planner-model-${profile.profileName}`}>{variant.plannerModelLabel}</label>
                    <select id={`edit-planner-model-${profile.profileName}`} bind:value={editPlannerModel}>
                      <option value="" disabled selected={editPlannerModel === ''}>Select model...</option>
                      {#each MODEL_OPTIONS as opt}
                        <option value={opt.value}>{opt.label}</option>
                      {/each}
                    </select>
                  </div>

                  <div class="form-field">
                    <label for={`edit-player-prompt-${profile.profileName}`}>{variant.playerPromptLabel}</label>
                    <textarea
                      id={`edit-player-prompt-${profile.profileName}`}
                      rows="3"
                      bind:value={editPlayerPrompt}
                      placeholder="Optional — leave empty to use the template default base prompt (FR-034)"
                    ></textarea>
                  </div>

                  <div class="form-field">
                    <label for={`edit-planner-prompt-${profile.profileName}`}>{variant.plannerPromptLabel}</label>
                    <textarea
                      id={`edit-planner-prompt-${profile.profileName}`}
                      rows="3"
                      bind:value={editPlannerPrompt}
                      placeholder="Optional — leave empty to use the template default base prompt (FR-034)"
                    ></textarea>
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

              {#if deleteErrors[profile.profileName]}
                <div class="detail-error">{deleteErrors[profile.profileName]}</div>
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

  .form-field input[type='text'] {
    padding: 6px 8px;
    font-size: 12px;
    font-family: inherit;
    background: #1a1a2e;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
  }

  .form-field input[type='text']:focus {
    outline: none;
    border-color: #4a9eff;
  }

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

  .form-field textarea:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .form-field select {
    padding: 6px 8px;
    font-size: 12px;
    font-family: inherit;
    background: #1a1a2e;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
  }

  .form-field select:focus {
    outline: none;
    border-color: #4a9eff;
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

  .badge-planner {
    background: rgba(80, 250, 123, 0.12);
    color: #50fa7b;
  }

  .edit-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    margin-top: 4px;
    background: #0f3460;
    border-radius: 4px;
    border: 1px solid #1a3a6e;
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
