<script lang="ts">
  import { onMount } from 'svelte'
  import type { Session, Config, WindowRef, HeldOperation, DebugResultHeldPayload, DebugResultReleasedPayload } from './api'
  import { TEMPLATE_SAOLEI, TEMPLATES } from './api'
  import {
    setConfig,
    listSessions,
    connect,
    closeAgent,
    listWindows,
    setSelectedWindow,
    getSelectedWindow,
    setDebugMode,
    confirmToolResult,
  } from './api'
  import { log, setDebugEnabled, setLogSink } from './logger'
  import type { LogEntry } from './logger'
  import SessionList from './components/SessionList.svelte'
  import OperationConfirmDrawer from './components/OperationConfirmDrawer.svelte'
  import LogPanel from './components/LogPanel.svelte'

  // --- Page state ---
  // The template is the TOP-LEVEL control plane: it is a local constant and
  // switching it MUST NOT issue any template-list API request (FR-024).
  // Session listing is scoped to the active template; the desktop is a flow
  // control terminal — session selection is read-only
  // (contracts/desktop-bridge.md §5).
  let page = $state<'sessions' | 'session'>('sessions')
  let template = $state(TEMPLATE_SAOLEI)

  // --- Types ---
  type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'

  // --- App-level state ---
  let selectedSession: Session | null = $state(null)
  let sessions: Session[] = $state([])
  let connectionState = $state<ConnectionState>('disconnected')
  let logEntries: LogEntry[] = $state([])
  let loading = $state(false)
  let error: string | null = $state(null)

  // --- Debug mode (FR-001): developer-facing verbose-logging toggle. Not
  // persisted; resets to OFF on page/session exit (FR-002). The frontend owns
  // the flag and mirrors it to the Go backend via the SetDebugMode bound method
  // (specs/022-desktop-debug-mode/research.md D1).
  let debugMode = $state(false)

  // --- Held operations awaiting confirmation (debug drawer,
  // contracts/debug-drawer-contract.md §3.1). The session-top Confirm drawer
  // is driven entirely by the operation channel: each entry's `toolId` is the
  // bridge-minted operation id. Populated by the EXTENDED
  // `game:debug:result-held` payload and removed on `result-released`.
  // Arrival order is preserved so multiple simultaneous holds stack in the
  // drawer (§5). Reactive via $state.
  let heldOperations = $state<HeldOperation[]>([])

  // --- Config state ---
  let gatewayURL = $state('https://game.liukexin.com')
  let env = $state('')

  // --- Window selection state ---
  let windows: WindowRef[] = $state([])
  let selectedWindowHandle: number | undefined = $state(undefined)

  function resetSessionViewState() {
    selectedWindowHandle = undefined
    windows = []
    // FR-002: debug mode is not persisted; reset to OFF on page/session exit
    // and notify both layers so they stay in sync.
    if (debugMode) {
      debugMode = false
      applyDebugMode()
    }
    // Clear any residual drawer entries. The backend's SetDebugMode(false)
    // above releases all holds and emits result-released for each, but those
    // events arrive asynchronously; clearing here avoids a flicker of stale
    // rows on the next session entry (contracts/debug-drawer-contract.md §5).
    heldOperations = []
  }

  // Push the selected window handle to the backend on every dropdown change.
  // Selecting a window is sufficient to make it the target for every
  // screenshot and operation — there is no separate "bind" step (spec 025
  // FR-001/FR-006, contracts/window-select-contract.md §2.1). Re-selecting a
  // different window retargets subsequent ops (FR-004). The undefined initial
  // value and the resetSessionViewState clear are skipped (no selection).
  $effect(() => {
    const h = selectedWindowHandle
    if (h == null) return
    void setSelectedWindow(h).catch((e: unknown) => {
      log('error', 'windows', `SetSelectedWindow failed: ${String(e)}`)
    })
  })

  // applyDebugMode pushes the current debugMode to the frontend logger gate and
  // the Go backend SetDebugMode bound method, keeping the two layers in sync
  // (FR-001 toggle, FR-004 both layers emit DEBUG). Called on toggle and on the
  // page/session-exit reset above (FR-002).
  function applyDebugMode() {
    setDebugEnabled(debugMode)
    void setDebugMode(debugMode).catch((e: unknown) => {
      log('error', 'debug', `SetDebugMode failed: ${String(e)}`)
    })
  }

  // handleConfirm releases a held operation result so the Go backend sends it
  // to the agent (FR-009 / FR-025). Called by OperationConfirmDrawer's "Confirm"
  // button via the onConfirm callback prop
  // (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §3).
  function handleConfirm(toolID: string) {
    void confirmToolResult(toolID).catch((e: unknown) => {
      log('error', 'debug', `ConfirmToolResult failed for ${toolID}: ${String(e)}`)
    })
  }

  setLogSink((entry: LogEntry) => {
    logEntries = [...logEntries, entry]
  })

  // --- Auto-load sessions on mount ---
  let initialized = false

  onMount(() => {
    if (!initialized) {
      initialized = true
      void handleRefresh()
    }

    // Debug hold events: the Go backend emits game:debug:result-held /
    // result-released when an operation result begins/ends being held for
    // confirmation (contracts/debug-drawer-contract.md §2/§3.1). The held
    // payload is extended with the operation descriptor (kind/summary/details)
    // so the session-top drawer can render the request content. `toolId` is
    // the operation-channel id.
    const runtime = window.runtime
    if (runtime?.EventsOn) {
      runtime.EventsOn('game:debug:result-held', (payload: unknown) => {
        const p = payload as DebugResultHeldPayload | undefined
        if (!p?.toolId) return
        const op = p.operation
        heldOperations = [...heldOperations, {
          toolId: p.toolId,
          kind: op?.kind ?? '',
          summary: op?.summary ?? '',
          details: op?.details ?? {},
        }]
      })
      runtime.EventsOn('game:debug:result-released', (payload: unknown) => {
        const p = payload as DebugResultReleasedPayload | undefined
        if (!p?.toolId) return
        heldOperations = heldOperations.filter(h => h.toolId !== p.toolId)
      })
    }
  })

  // --- Config handlers ---
  async function handleApplyConfig() {
    try {
      loading = true
      error = null
      const cfg: Config = { gateway_url: gatewayURL, env }
      await setConfig(cfg)
      log('info', 'config', `Config applied: ${gatewayURL}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'config', `Apply config failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  // handleTemplateChange switches the top-level template control plane. The
  // template list is a LOCAL constant (TEMPLATES) — switching performs NO
  // network request (FR-024). Sessions are template-scoped, so switching
  // returns to the sessions page and re-lists for the new template.
  function handleTemplateChange() {
    selectedSession = null
    page = 'sessions'
    void handleRefresh()
    log('info', 'template', `Switched template (local constant, no request): ${template}`)
  }

  // --- SessionList handlers ---
  async function handleRefresh() {
    try {
      loading = true
      error = null
      const resp = await listSessions(template, 50, '')
      sessions = resp.sessions
      log('info', 'sessions', `Listed ${sessions.length} sessions`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Refresh failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  // handleSelectSession enters the chosen session: the list is read-only
  // (A4) — selection directly drives the flow-channel connect; there is no
  // team materialization or profile choice on this terminal.
  async function handleSelectSession(session: Session) {
    resetSessionViewState()
    selectedSession = session
    error = null
    connectionState = 'disconnected'

    page = 'session'
    // Load the window list and restore the dropdown to the backend's selected
    // window (if it still exists), so re-entering a session keeps the prior
    // selection instead of forcing a re-select
    // (contracts/window-select-contract.md §2.1).
    void handleLoadWindows().then(syncSelectedWindow)

    await handleConnect()
  }

  async function handleConnect() {
    if (!selectedSession) return
    try {
      connectionState = 'connecting'
      await connect(template, selectedSession.sessionId)
      connectionState = 'connected'
      log('info', 'session', 'Session connected via WebSocket')
    } catch (e: unknown) {
      connectionState = 'error'
      error = String(e)
      log('error', 'session', `Connect failed: ${String(e)}`)
    }
  }

  // handleBackToSessions tears down the session flow and re-enters the
  // sessions list. Returning MUST re-list sessions so external changes (web
  // creates/deletes) are reflected on entry (FR-007,
  // specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §4.2):
  // handleRefresh writes only sessions/loading/error — never
  // selectedSession/page — so the refresh cannot disturb navigation
  // (specs/055-agent-v2-ui-fixes/data-model.md §4); on failure the existing
  // catch semantics surface the error without clearing the rendered list.
  async function handleBackToSessions() {
    error = null
    if (connectionState === 'connected' || connectionState === 'connecting') {
      try {
        await closeAgent()
      } catch {
        // ignore close errors on teardown
      }
      connectionState = 'disconnected'
    }
    resetSessionViewState()
    selectedSession = null
    page = 'sessions'
    void handleRefresh()
  }

  // --- Log handler ---
  function handleClearLogs() {
    logEntries = []
  }

  // --- Window selection handlers ---
  async function handleLoadWindows() {
    try {
      windows = await listWindows()
    } catch (e: unknown) {
      log('warn', 'windows', `Failed to list windows: ${String(e)}`)
    }
  }

  // syncSelectedWindow restores the dropdown to the backend's selected window
  // handle after the window list is (re)loaded on session entry. The handle is
  // only restored when it still exists in the live list — a closed window
  // leaves the dropdown at "Select window..." (spec 025 FR-005).
  async function syncSelectedWindow() {
    try {
      const hwnd = await getSelectedWindow()
      if (hwnd && windows.some(w => w.handle === hwnd)) {
        selectedWindowHandle = hwnd
      }
    } catch (e: unknown) {
      log('warn', 'windows', `GetSelectedWindow failed: ${String(e)}`)
    }
  }
</script>

<div class="app-container">
  <!-- Config Area (top) -->
  <div class="config-area">
    <div class="config-row">
      <label for="gateway-url">Gateway URL</label>
      <input id="gateway-url" type="text" bind:value={gatewayURL} placeholder="https://game.liukexin.com" />
    </div>
    <div class="config-row">
      <label for="env">Env</label>
      <input id="env" type="text" bind:value={env} placeholder="environment" />
    </div>
    <div class="config-row">
      <!-- Top-level template control plane (FR-024): the option list is the
           LOCAL TEMPLATES constant; switching issues NO network request. -->
      <label for="template-select">Template</label>
      <select id="template-select" data-testid="template-select" bind:value={template} onchange={handleTemplateChange}>
        {#each TEMPLATES as t}
          <option value={t}>{t}</option>
        {/each}
      </select>
    </div>
    <button class="btn btn-primary" onclick={handleApplyConfig} disabled={loading}>Apply Config</button>
  </div>

  <!-- Page Content (middle) -->
  {#if page === 'sessions'}
    <div class="sessions-page">
      <div class="sessions-toolbar">
        <span class="sessions-template">Template: {template}</span>
      </div>
      <SessionList
        {sessions}
        selectedSessionId={selectedSession?.sessionId ?? null}
        {loading}
        {error}
        onSelect={handleSelectSession}
      />
    </div>
  {:else if page === 'session'}
    <div class="session-layout">
      <div class="session-top-bar">
        <button class="btn btn-small" data-testid="back-btn" onclick={handleBackToSessions}>← Back to Sessions</button>
        <span class="session-label">Session: <strong>{selectedSession?.sessionId ?? ''}</strong></span>
        <!-- The window list reloads every time the dropdown opens so it
             reflects the current windows, not the ones at page entry. -->
        <select class="window-select" data-testid="window-select" bind:value={selectedWindowHandle} onfocus={handleLoadWindows}>
          <option value={undefined} disabled selected={selectedWindowHandle == null}>Select window...</option>
          {#each windows as w}
            <option value={w.handle}>{w.title}</option>
          {/each}
        </select>
        <span class="connection-status" data-testid="connection-status" class:connected={connectionState === 'connected'}>
          {connectionState}
        </span>
        <label class="debug-toggle" data-testid="debug-toggle" title="Toggle debug-level log output (FR-001)">
          <input type="checkbox" bind:checked={debugMode} onchange={applyDebugMode} />
          <span>Debug</span>
        </label>
        {#if connectionState === 'error'}
          <span class="session-error" data-testid="session-connection-error">{error ?? 'Connection failed'}</span>
        {/if}
      </div>
      <OperationConfirmDrawer
        heldOperations={heldOperations}
        onConfirm={handleConfirm}
      />
    </div>
  {/if}

  <!-- Log Panel (bottom, always visible) -->
  <LogPanel logs={logEntries} onclear={handleClearLogs} />
</div>

<style>
  .sessions-page {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    min-height: 0;
  }

  .sessions-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 10px;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    font-size: 12px;
    color: #a0a0b0;
    flex-shrink: 0;
  }

  .sessions-template {
    font-weight: 600;
  }

  .sessions-page :global(.session-list) {
    flex: 1 1 auto;
    height: auto;
    min-height: 0;
  }

  .session-layout {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    overflow: hidden;
  }

  .session-top-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    flex-wrap: wrap;
  }

  .session-label {
    font-size: 12px;
    color: #a0a0b0;
  }

  .session-label strong {
    color: #e0e0e0;
  }

  .window-select {
    padding: 6px 8px;
    font-size: 12px;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #e0e0e0;
    max-width: 220px;
    min-width: 120px;
    flex-shrink: 1;
  }

  .window-select:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .connection-status {
    font-size: 12px;
    color: #ff6b6b;
  }

  .connection-status.connected {
    color: #50fa7b;
  }

  .debug-toggle {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: #a0a0b0;
    cursor: pointer;
    user-select: none;
    flex-shrink: 0;
  }

  .debug-toggle input {
    cursor: pointer;
  }

  .session-error {
    margin-left: auto;
    font-size: 12px;
    color: #ff6b6b;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
