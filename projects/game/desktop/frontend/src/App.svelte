<script lang="ts">
  import { onMount } from 'svelte'
  import type { Session, Agent, WindowRef, AgentAckFrame, Config, AgentFrame, OperationResultView } from './api'
  import {
    setConfig,
    createSession,
    listSessions,
    deleteSession,
    createAgent,
    getAgent,
    deleteAgent,
    connectAgent,
    closeAgent,
    listWindows,
    bindWindow,
    captureScreenshot,
    sendScreenshot,
    executeOperation,
    sendNextScreenshot
  } from './api'
  import { log, setLogSink } from './logger'
  import type { LogEntry } from './logger'
  import SessionList from './components/SessionList.svelte'
  import SessionDetail from './components/SessionDetail.svelte'
  import PlayView from './components/PlayView.svelte'
  import LogPanel from './components/LogPanel.svelte'

  // --- Page state ---
  let page = $state<'sessions' | 'detail' | 'play'>('sessions')

  // --- Types ---
  type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'
  type AgentLoadState = 'idle' | 'loading' | 'loaded' | 'not_found' | 'error'

  // --- App-level state ---
  let selectedSession: Session | null = $state(null)
  let sessions: Session[] = $state([])
  let agent: Agent | null = $state(null)
  let connectionState: ConnectionState = $state('disconnected')
  let agentLoadState: AgentLoadState = $state('idle')
  let windows: WindowRef[] = $state([])
  let boundWindow: WindowRef | null = $state(null)
  let screenshotData: string | null = $state(null)
  let screenshotMeta: { width: number; height: number; encoding: string } | null = $state(null)
  let ackResult: AgentAckFrame | null = $state(null)
  let playState = $state('idle')
  let logEntries: LogEntry[] = $state([])
  let loading = $state(false)
  let error: string | null = $state(null)

  type AgentFrameItem = {
    type: 'text' | 'thinking' | 'operation' | 'warn'
    content?: string
    mouse?: { button: number; clickType: number; xPx: number; yPx: number }
    keyboard?: { keyCodes: string }
    operationId?: string
    screenshotId?: string
    sequence?: number
    message?: string
    code?: string
  }

  let agentFrames: AgentFrameItem[] = $state([])
  let operationResult: OperationResultView | null = $state(null)

  // --- Config state ---
  let gatewayURL = $state('https://game.liukexin.com')
  let env = $state('')

  setLogSink((entry: LogEntry) => {
    logEntries = [...logEntries, entry]
  })

  // --- Auto-load sessions on mount ---
  let initialized = false

  onMount(() => {
    if (!initialized) {
      initialized = true
      handleRefresh()
    }

    const runtime = window.runtime
    if (runtime?.EventsOn) {
      runtime.EventsOn('game:frame', (data: unknown) => {
        const frame = data as AgentFrame
        handleAgentFrame(frame)
      })
    }
  })

  // --- Config handlers ---
  async function handleApplyConfig() {
    try {
      loading = true
      error = null
      await setConfig({ gateway_url: gatewayURL, env })
      log('info', 'config', `Config applied: ${gatewayURL}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'config', `Apply config failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  // --- SessionList handlers ---
  async function handleRefresh() {
    try {
      loading = true
      error = null
      const resp = await listSessions(50, '')
      sessions = resp.sessions
      log('info', 'sessions', `Listed ${sessions.length} sessions`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Refresh failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleCreate() {
    try {
      loading = true
      error = null
      const session = await createSession()
      log('info', 'sessions', `Session created: ${session.sessionId}`)
      await handleRefresh()
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Create failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleDelete(sessionId: string) {
    try {
      loading = true
      error = null
      await deleteSession(sessionId)
      log('info', 'sessions', `Session deleted: ${sessionId}`)
      await handleRefresh()
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Delete failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  function handleSelectSession(session: Session) {
    selectedSession = session
    agent = null
    error = null
    agentLoadState = 'idle'
    connectionState = 'disconnected'
    page = 'detail'
    // Auto-load agent when entering detail
    handleAutoGetAgent(session.sessionId)
  }

  async function handleAutoGetAgent(sessionId: string) {
    try {
      agentLoadState = 'loading'
      error = null
      agent = await getAgent(sessionId)
      agentLoadState = 'loaded'
      log('info', 'agent', `Agent loaded: ${agent.name || agent.sessionId}`)
    } catch (e: unknown) {
      const errStr = String(e)
      if (errStr.includes('not found') || errStr.includes('NotFound') || errStr.includes('NOT_FOUND')) {
        agentLoadState = 'not_found'
      } else {
        agentLoadState = 'error'
        error = errStr
      }
      log('info', 'agent', `Get agent failed: ${errStr}`)
    }
  }

  // --- SessionDetail handlers ---
  async function handleCreateAgent() {
    if (!selectedSession) return
    try {
      loading = true
      error = null
      agent = await createAgent(selectedSession.sessionId)
      log('info', 'agent', `Agent created: ${agent.sessionId}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'agent', `Create agent failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleGetAgent() {
    if (!selectedSession) return
    try {
      loading = true
      error = null
      agent = await getAgent(selectedSession.sessionId)
      log('info', 'agent', `Agent: ${JSON.stringify(agent)}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'agent', `Get agent failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleDeleteAgent() {
    if (!selectedSession) return
    try {
      loading = true
      error = null
      await deleteAgent(selectedSession.sessionId)
      agent = null
      log('info', 'agent', `Agent deleted: ${selectedSession.sessionId}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'agent', `Delete agent failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleConnectAgent() {
    if (!selectedSession) return
    try {
      loading = true
      error = null
      connectionState = 'connecting'
      await connectAgent(selectedSession.sessionId)
      connectionState = 'connected'
      log('info', 'agent', 'Agent connected via WebSocket')
    } catch (e: unknown) {
      connectionState = 'error'
      error = String(e)
      log('error', 'agent', `Connect agent failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  function handleEnterPlay() {
    playState = 'idle'
    windows = []
    boundWindow = null
    screenshotData = null
    screenshotMeta = null
    ackResult = null
    agentFrames = []
    operationResult = null
    error = null
    page = 'play'
  }

  function handleAgentFrame(frame: AgentFrame) {
    if (frame.text) {
      agentFrames = [...agentFrames, { type: 'text', content: frame.text.content }]
    } else if (frame.thinking) {
      agentFrames = [...agentFrames, { type: 'thinking', content: frame.thinking.content }]
    } else if (frame.operation) {
      const op = frame.operation
      agentFrames = [...agentFrames, {
        type: 'operation',
        operationId: op.operationId,
        screenshotId: op.screenshotId,
        sequence: op.sequence,
        mouse: op.mouse ? { button: op.mouse.button, clickType: op.mouse.clickType, xPx: op.mouse.xPx, yPx: op.mouse.yPx } : undefined,
        keyboard: op.keyboard ? { keyCodes: op.keyboard.keyCodes } : undefined,
      }]
    } else if (frame.warn) {
      agentFrames = [...agentFrames, { type: 'warn', message: frame.warn.message, code: frame.warn.code }]
    } else if (frame.operationResult) {
      operationResult = frame.operationResult
    }
  }

  async function handleExecuteOperation(frame: AgentFrameItem) {
    try {
      loading = true
      error = null
      const result = await executeOperation(
        frame.operationId || '',
        frame.screenshotId || '',
        frame.sequence || 0,
        frame.mouse?.button || 1,
        frame.mouse?.clickType || 1,
        frame.mouse?.xPx || 0,
        frame.mouse?.yPx || 0,
        !!frame.mouse,
        frame.keyboard?.keyCodes || '',
        0,
        0,
      )
      operationResult = result
      log('info', 'play', `Operation executed: ${result.operationId} status=${result.status}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'play', `Execute operation failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  function handleBackToSessions() {
    selectedSession = null
    agent = null
    connectionState = 'disconnected'
    agentLoadState = 'idle'
    error = null
    page = 'sessions'
  }

  function handleBackToDetail() {
    error = null
    page = 'detail'
  }

  async function handleDeleteSessionFromDetail() {
    if (!selectedSession) return
    try {
      loading = true
      error = null
      // Close WS if connected
      if (connectionState === 'connected') {
        try {
          await closeAgent()
        } catch {
          // ignore close errors
        }
        connectionState = 'disconnected'
      }
      await deleteSession(selectedSession.sessionId)
      log('info', 'sessions', `Session deleted: ${selectedSession.sessionId}`)
      selectedSession = null
      agent = null
      agentLoadState = 'idle'
      page = 'sessions'
      await handleRefresh()
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Delete session failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleRefreshFromDetail() {
    if (!selectedSession) return
    await handleAutoGetAgent(selectedSession.sessionId)
    log('info', 'agent', 'Agent refreshed')
  }

  // --- PlayView handlers ---
  async function handleListWindows() {
    try {
      loading = true
      error = null
      windows = await listWindows()
      log('info', 'play', `Listed ${windows.length} windows`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'play', `List windows failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleBindWindow(hwnd: number) {
    try {
      loading = true
      error = null
      await bindWindow(hwnd)
      const found = windows.find(w => w.handle === hwnd)
      if (found) {
        boundWindow = found
      }
      playState = 'window_bound'
      log('info', 'play', `Bound window: ${found?.title || String(hwnd)}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'play', `Bind window failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleCaptureScreenshot() {
    try {
      loading = true
      error = null
      const img = await captureScreenshot()
      // img.data is already a base64-encoded PNG string (Wails serializes Go []byte as base64)
      screenshotData = img.data
      screenshotMeta = { width: img.widthPx, height: img.heightPx, encoding: img.encoding }
      playState = 'screenshot_captured'
      log('info', 'play', `Screenshot captured: ${img.widthPx}x${img.heightPx} ${img.encoding}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'play', `Capture screenshot failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleSendScreenshot(hwnd: number) {
    try {
      loading = true
      error = null
      ackResult = await sendScreenshot(hwnd)
      playState = 'screenshot_sent'
      log('info', 'play', `Screenshot sent, ack: ${ackResult.ackFrameId}`)
    } catch (e: unknown) {
      error = String(e)
      log('error', 'play', `Send screenshot failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  async function handleCaptureNext() {
    try {
      loading = true
      error = null
      await sendNextScreenshot()
      operationResult = null
      agentFrames = []
      playState = 'next_screenshot_sent'
      log('info', 'play', 'Next screenshot sent, waiting for agent response')
    } catch (e: unknown) {
      error = String(e)
      log('error', 'play', `Capture next screenshot failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  // --- Log handler ---
  function handleClearLogs() {
    logEntries = []
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
    <button class="btn btn-primary" onclick={handleApplyConfig} disabled={loading}>Apply Config</button>
  </div>

  <!-- Page Content (middle) -->
  {#if page === 'sessions'}
    <SessionList
      {sessions}
      selectedSessionId={selectedSession?.sessionId ?? null}
      {loading}
      {error}
      onSelect={handleSelectSession}
      onRefresh={handleRefresh}
      onCreate={handleCreate}
      onDelete={handleDelete}
    />
  {:else if page === 'detail'}
    <SessionDetail
      session={selectedSession}
      {agent}
      {connectionState}
      {agentLoadState}
      {loading}
      {error}
      onCreateAgent={handleCreateAgent}
      onDeleteAgent={handleDeleteAgent}
      onConnectAgent={handleConnectAgent}
      onDeleteSession={handleDeleteSessionFromDetail}
      onRefresh={handleRefreshFromDetail}
      onEnterPlay={handleEnterPlay}
      onBack={handleBackToSessions}
    />
  {:else if page === 'play'}
    <PlayView
      sessionId={selectedSession?.sessionId ?? ''}
      {windows}
      {boundWindow}
      {screenshotData}
      {screenshotMeta}
      {ackResult}
      {agentFrames}
      {operationResult}
      {playState}
      wsConnected={connectionState === 'connected'}
      {loading}
      {error}
      onListWindows={handleListWindows}
      onBindWindow={handleBindWindow}
      onCaptureScreenshot={handleCaptureScreenshot}
      onSendScreenshot={handleSendScreenshot}
      onExecuteOperation={handleExecuteOperation}
      onCaptureNext={handleCaptureNext}
      onBack={handleBackToDetail}
    />
  {/if}

  <!-- Log Panel (bottom, always visible) -->
  <LogPanel logs={logEntries} onclear={handleClearLogs} />
</div>
