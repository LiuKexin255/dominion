<script lang="ts">
  import type { AgentFrame, CheckResult, LogEntry } from './api'
  import {
    connectAgent,
    createAgent,
    createSession,
    deleteAgent,
    deleteSession,
    getAgent,
    getSession,
    runConnectivityCheck,
    sendAgentFrame,
    setConfig
  } from './api'
  import { log, setLogSink } from './logger'

  let gatewayURL = $state('https://game.liukexin.com')
  let env = $state('')
  let sessionID = $state(`desktop-${Date.now()}`)
  let logEntries = $state<LogEntry[]>([])
  let isRunning = $state(false)
  let logContainer: HTMLDivElement

  setLogSink((entry: LogEntry) => {
    logEntries = [...logEntries, entry]
  })

  $effect(() => {
    if (logEntries.length && logContainer) {
      logContainer.scrollTop = logContainer.scrollHeight
    }
  })

  async function handleApplyConfig() {
    await setConfig({ gateway_url: gatewayURL, env })
    log('info', 'frontend', `Config applied: ${gatewayURL}`)
  }

  async function handleCreateSession() {
    try {
      const session = await createSession(sessionID)
      log('info', 'frontend', `Session created: ${session.session_id}`)
    } catch (e: unknown) { log('error', 'frontend', `Create session failed: ${String(e)}`) }
  }

  async function handleGetSession() {
    try {
      const session = await getSession(sessionID)
      log('info', 'frontend', `Session: ${JSON.stringify(session)}`)
    } catch (e: unknown) { log('error', 'frontend', `Get session failed: ${String(e)}`) }
  }

  async function handleDeleteSession() {
    try {
      await deleteSession(sessionID)
      log('info', 'frontend', `Session deleted: ${sessionID}`)
    } catch (e: unknown) { log('error', 'frontend', `Delete session failed: ${String(e)}`) }
  }

  async function handleCreateAgent() {
    try {
      const agent = await createAgent(sessionID)
      log('info', 'frontend', `Agent created: ${agent.session_id}`)
    } catch (e: unknown) { log('error', 'frontend', `Create agent failed: ${String(e)}`) }
  }

  async function handleGetAgent() {
    try {
      const agent = await getAgent(sessionID)
      log('info', 'frontend', `Agent: ${JSON.stringify(agent)}`)
    } catch (e: unknown) { log('error', 'frontend', `Get agent failed: ${String(e)}`) }
  }

  async function handleDeleteAgent() {
    try {
      await deleteAgent(sessionID)
      log('info', 'frontend', `Agent deleted: ${sessionID}`)
    } catch (e: unknown) { log('error', 'frontend', `Delete agent failed: ${String(e)}`) }
  }

  async function handleConnectAgent() {
    try {
      await connectAgent(sessionID)
      log('info', 'frontend', 'Agent connected via WebSocket')
    } catch (e: unknown) { log('error', 'frontend', `Connect agent failed: ${String(e)}`) }
  }

  async function handleSendStatus() {
    try {
      const frame: AgentFrame = { session_id: sessionID, type: 'status', payload: '' }
      const resp = await sendAgentFrame(frame)
      log('info', 'frontend', `Status response: ${JSON.stringify(resp)}`)
    } catch (e: unknown) { log('error', 'frontend', `Send status failed: ${String(e)}`) }
  }

  async function handleSendEcho() {
    try {
      const echoData = btoa('hello-desktop')
      const frame: AgentFrame = { session_id: sessionID, type: 'echo', payload: echoData }
      const resp = await sendAgentFrame(frame)
      log('info', 'frontend', `Echo response: ${JSON.stringify(resp)}`)
    } catch (e: unknown) { log('error', 'frontend', `Send echo failed: ${String(e)}`) }
  }

  async function handleFullCheck() {
    isRunning = true
    try {
      const result: CheckResult = await runConnectivityCheck(sessionID)
      log('info', 'frontend', result.success ? 'CONNECTIVITY CHECK PASSED' : 'CONNECTIVITY CHECK FAILED')
    } catch (e: unknown) {
      log('error', 'frontend', `Connectivity check failed: ${String(e)}`)
    } finally {
      isRunning = false
    }
  }

  function handleClearLogs() {
    logEntries = []
  }

  function formatTime(t: string): string {
    return new Date(t).toLocaleTimeString()
  }
</script>

<div class="app-container">
  <!-- Config Area -->
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
      <label for="session-id">Session ID</label>
      <input id="session-id" type="text" bind:value={sessionID} placeholder="desktop-..." />
    </div>
    <button class="btn btn-primary" onclick={handleApplyConfig}>Apply Config</button>
  </div>

  <!-- Action Area -->
  <div class="action-area">
    <button class="btn" onclick={handleCreateSession}>Create Session</button>
    <button class="btn" onclick={handleGetSession}>Get Session</button>
    <button class="btn" onclick={handleDeleteSession}>Delete Session</button>
    <button class="btn" onclick={handleCreateAgent}>Create Agent</button>
    <button class="btn" onclick={handleGetAgent}>Get Agent</button>
    <button class="btn" onclick={handleDeleteAgent}>Delete Agent</button>
    <button class="btn" onclick={handleConnectAgent}>Connect WS</button>
    <button class="btn" onclick={handleSendStatus}>Send Status</button>
    <button class="btn" onclick={handleSendEcho}>Send Echo</button>
    <button class="btn btn-primary" onclick={handleFullCheck} disabled={isRunning}>
      {isRunning ? 'Running...' : 'Full Check'}
    </button>
  </div>

  <!-- Log Area -->
  <div class="log-area">
    <div class="log-header">
      <span>Logs ({logEntries.length})</span>
      <button class="btn btn-small" onclick={handleClearLogs}>Clear Logs</button>
    </div>
    <div class="log-container" bind:this={logContainer}>
      {#if logEntries.length === 0}
        <div class="log-empty">No logs yet. Click a button to start.</div>
      {/if}
      {#each logEntries as entry}
        <div class="log-entry log-{entry.level}">
          <span class="log-time">{formatTime(entry.time)}</span>
          <span class="log-level">{entry.level.toUpperCase()}</span>
          <span class="log-source">[{entry.source}]</span>
          <span class="log-msg">{entry.message}</span>
          {#if entry.fields && Object.keys(entry.fields).length > 0}
            <span class="log-fields">
              {#each Object.entries(entry.fields) as [key, value]}
                <span class="log-field">{key}={JSON.stringify(value)}</span>
              {/each}
            </span>
          {/if}
        </div>
      {/each}
    </div>
  </div>
</div>
