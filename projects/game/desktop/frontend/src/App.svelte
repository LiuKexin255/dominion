<script lang="ts">
  import { onMount } from 'svelte'
  import type { Session, Agent, AgentFrame, Config, AgentProfile, CreateAgentProfileRequest, AgentOperationFrame, AgentOperationResultFrame, WindowRef, CapturedImage } from './api'
  import { FrameSender } from './api'
  import {
    setConfig,
    createSession,
    listSessions,
    deleteSession,
    getAgent,
    connectAgent,
    closeAgent,
    listAgentProfiles,
    createAgentProfile,
    deleteAgentProfile,
    updateAgentProfile,
    refreshAgent,
    listWindows,
    bindWindow,
    captureScreenshot,
    sendUserTurn,
    listMessages,
  } from './api'
  import { log, setLogSink } from './logger'
  import type { LogEntry } from './logger'
  import SessionList from './components/SessionList.svelte'
  import ChatView from './components/ChatView.svelte'
  import ProfileManagement from './components/ProfileManagement.svelte'
  import AgentSidebar from './components/AgentSidebar.svelte'
  import LogPanel from './components/LogPanel.svelte'
  import ScreenshotModal from './components/ScreenshotModal.svelte'

  // --- Page state ---
  let page = $state<'sessions' | 'chat' | 'profiles'>('sessions')

  // --- Types ---
  type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'
  type PlayState =
    | 'connecting'
    | 'loading_messages'
    | 'chat_ready'
    | 'processing'
    | 'connection_error'
    | 'agent_lost'

  type ChatEntry = {
    messageId: string
    sender: FrameSender
    type: 'thinking' | 'text' | 'warn' | 'image' | 'operation' | 'operation_result'
    content: string
    timestamp: string
    agentProfileName?: string
    imageUrl?: string
    operation?: AgentOperationFrame
    operationResult?: AgentOperationResultFrame
  }

  // --- App-level state ---
  let selectedSession: Session | null = $state(null)
  let sessions: Session[] = $state([])
  let agent: Agent | null = $state(null)
  let connectionState: ConnectionState = $state('disconnected')
  let logEntries: LogEntry[] = $state([])
  let loading = $state(false)
  let error: string | null = $state(null)

  // --- Chat state ---
  let chatMessages: ChatEntry[] = $state([])
  let processing = $state(false)
  let queueCount = $state(0)
  let playState = $state<PlayState>('connecting')
  let messagesError = $state<string | null>(null)

  // --- Profile state ---
  let profiles: AgentProfile[] = $state([])
  let selectedProfile = $state('')

  // --- Profile management state ---
  let managedProfiles: AgentProfile[] = $state([])
  let profileMgmtLoading = $state(false)
  let profileMgmtError: string | null = $state(null)

  // --- Config state ---
  let gatewayURL = $state('https://game.liukexin.com')
  let env = $state('')

  // --- Window + Screenshot state ---
  let windows: WindowRef[] = $state([])
  let selectedWindowHandle: number | undefined = $state(undefined)
  let pendingScreenshot: { dataUrl: string; data: string; widthPx: number; heightPx: number } | null = $state(null)
  let capturing = $state(false)
  let refreshing = $state(false)
  let zoomedImageUrl: string | null = $state(null)

  function resetPlayPageState() {
    pendingScreenshot = null
    selectedWindowHandle = undefined
    windows = []
  }

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
        handleAgentFrame(data as AgentFrame & { wait?: { reason?: string } })
      })
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (page !== 'chat') return
      if (selectedWindowHandle == null) return
      if (e.ctrlKey && e.shiftKey && e.key.toUpperCase() === 'S') {
        e.preventDefault()
        handleCaptureScreenshot()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
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

  async function handleSelectSession(session: Session) {
    resetPlayPageState()
    selectedSession = session
    agent = null
    error = null
    messagesError = null
    connectionState = 'disconnected'
    chatMessages = []

    await loadProfiles()

    if (profiles.length > 0 && !selectedProfile) {
      selectedProfile = profiles[0].agentProfileName
    }

    page = 'chat'
    handleLoadWindows()
    playState = 'connecting'
    if ((connectionState as ConnectionState) !== 'connected') {
      await handleConnectAgent()
    }

    if ((connectionState as ConnectionState) === 'connected') {
      await handleLoadMessages()
    } else {
      playState = 'connection_error'
    }
  }

  async function loadProfiles() {
    try {
      const resp = await listAgentProfiles(50, '')
      profiles = resp.agentProfiles
      log('info', 'agent', `Loaded ${profiles.length} agent profiles`)
    } catch (e: unknown) {
      log('warn', 'agent', `Failed to load profiles: ${String(e)}`)
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

  async function handleConnectAgent() {
    if (!selectedSession) return
    try {
      connectionState = 'connecting'
      await connectAgent(selectedSession.sessionId)
      connectionState = 'connected'
      log('info', 'agent', 'Agent connected via WebSocket')
    } catch (e: unknown) {
      connectionState = 'error'
      error = String(e)
      log('error', 'agent', `Connect agent failed: ${String(e)}`)
    }
  }

  function senderFromString(raw: string): FrameSender {
    if (raw === 'FRAME_SENDER_USER') return FrameSender.USER
    if (raw === 'FRAME_SENDER_AGENT') return FrameSender.AGENT
    if (raw === 'FRAME_SENDER_SYSTEM') return FrameSender.SYSTEM
    return FrameSender.SYSTEM
  }

  function typeFromString(raw: string): 'thinking' | 'text' | 'warn' {
    if (raw === 'thinking' || raw === 'text' || raw === 'warn') return raw
    return 'text'
  }

  async function handleLoadMessages() {
    if (!selectedSession) return
    playState = 'loading_messages'
    messagesError = null
    try {
      const entries = (await listMessages(selectedSession.sessionId)) ?? []
      chatMessages = entries.map(entry => {
        if (entry.type === 'image' && entry.imageData) {
          return {
            messageId: entry.messageId,
            sender: senderFromString(entry.sender),
            type: 'image',
            content: '',
            timestamp: entry.createTime || new Date().toISOString(),
            imageUrl: `data:image/png;base64,${entry.imageData}`,
          }
        }
        return {
          messageId: entry.messageId,
          sender: senderFromString(entry.sender),
          type: typeFromString(entry.type),
          content: entry.content,
          timestamp: entry.createTime || new Date().toISOString(),
        }
      })
      playState = 'chat_ready'
      log('info', 'chat', `Loaded ${entries.length} messages from history`)
    } catch (e: unknown) {
      const errStr = String(e)
      messagesError = errStr
      playState = 'chat_ready'
      chatMessages = []
      log('warn', 'chat', `Failed to load messages: ${errStr}`)
    }
  }

  function handleAgentFrame(frame: AgentFrame & { wait?: { reason?: string } }) {
    if (frame.thinking) {
      const thinkingContent = frame.thinking.content || ''
      if (!thinkingContent) return
      const last = chatMessages[chatMessages.length - 1]
      if (last && last.type === 'thinking' && last.sender === FrameSender.AGENT
          && last.agentProfileName === frame.agentProfileName) {
        last.content += thinkingContent
        chatMessages = [...chatMessages]
      } else {
        chatMessages = [...chatMessages, {
          messageId: frame.frameId,
          sender: FrameSender.AGENT,
          type: 'thinking',
          content: thinkingContent,
          timestamp: frame.createTime || new Date().toISOString(),
          agentProfileName: frame.agentProfileName,
        }]
      }
    } else if (frame.text) {
      const textContent = frame.text.content || ''
      if (!textContent) return
      const last = chatMessages[chatMessages.length - 1]
      if (last && last.type === 'text' && last.sender === FrameSender.AGENT
          && last.agentProfileName === frame.agentProfileName) {
        last.content += textContent
        chatMessages = [...chatMessages]
      } else {
        chatMessages = [...chatMessages, {
          messageId: frame.frameId,
          sender: FrameSender.AGENT,
          type: 'text',
          content: textContent,
          timestamp: frame.createTime || new Date().toISOString(),
          agentProfileName: frame.agentProfileName,
        }]
      }
    } else if (frame.warn) {
      chatMessages = [...chatMessages, {
        messageId: frame.frameId,
        sender: FrameSender.SYSTEM,
        type: 'warn',
        content: frame.warn.message,
        timestamp: frame.createTime || new Date().toISOString(),
      }]
    } else if (frame.userTurn) {
      const image = frame.userTurn.image
      if (image && image.data) {
        const encoding = (image.encoding || 'png').toLowerCase()
        const imageUrl = `data:image/${encoding};base64,${image.data}`
        chatMessages = [...chatMessages, {
          messageId: frame.frameId,
          sender: FrameSender.USER,
          type: 'image',
          content: '',
          timestamp: frame.createTime || new Date().toISOString(),
          imageUrl,
        }]
      }
    } else if (frame.operation) {
      chatMessages = [...chatMessages, {
        messageId: frame.frameId,
        sender: FrameSender.AGENT,
        type: 'operation',
        content: '',
        timestamp: frame.createTime || new Date().toISOString(),
        agentProfileName: frame.agentProfileName,
        operation: frame.operation,
      }]
    } else if (frame.operationResult) {
      chatMessages = [...chatMessages, {
        messageId: frame.frameId,
        sender: FrameSender.SYSTEM,
        type: 'operation_result',
        content: '',
        timestamp: frame.createTime || new Date().toISOString(),
        operationResult: frame.operationResult,
      }]
    } else if (frame.wait) {
      processing = false
      if (playState === 'processing') playState = 'chat_ready'
    }
  }

  async function handleSendChatText(text: string) {
    if (!selectedSession) return
    // Auto-connect fallback if WS dropped (sendUserTurn relies on the backend connection)
    const wasConnected = connectionState === 'connected'
    if (!wasConnected) {
      playState = 'connecting'
      await handleConnectAgent()
    }
    const nowConnected = connectionState === 'connected'
    if (!wasConnected && nowConnected) {
      await handleLoadMessages()
    } else if (!nowConnected) {
      playState = 'connection_error'
      messagesError = 'Connection failed. Retry to send your message.'
      return
    }
    const optimisticIds: string[] = []
    try {
      if (text.trim()) {
        const msgId = crypto.randomUUID()
        optimisticIds.push(msgId)
        chatMessages = [...chatMessages, {
          messageId: msgId,
          sender: FrameSender.USER,
          type: 'text',
          content: text,
          timestamp: new Date().toISOString(),
        }]
      }
      if (pendingScreenshot) {
        const imgId = crypto.randomUUID()
        optimisticIds.push(imgId)
        chatMessages = [...chatMessages, {
          messageId: imgId,
          sender: FrameSender.USER,
          type: 'image',
          content: '',
          timestamp: new Date().toISOString(),
          imageUrl: pendingScreenshot.dataUrl,
        }]
      }
      processing = true
      playState = 'processing'
      queueCount++
      const screenshotData = pendingScreenshot?.data ?? ''
      const screenshotWidthPx = pendingScreenshot?.widthPx ?? 0
      const screenshotHeightPx = pendingScreenshot?.heightPx ?? 0
      pendingScreenshot = null
      queueCount = Math.max(0, queueCount - 1)
      await sendUserTurn(
        selectedSession.sessionId,
        text,
        screenshotData,
        screenshotWidthPx,
        screenshotHeightPx,
        selectedProfile,
      )
      log('info', 'chat', `Sent to agent: ${text.substring(0, 60)}`)
    } catch (e: unknown) {
      if (optimisticIds.length > 0) {
        const idSet = new Set(optimisticIds)
        chatMessages = chatMessages.filter(m => !idSet.has(m.messageId))
      }
      error = String(e)
      processing = false
      queueCount = Math.max(0, queueCount - 1)
      if (playState === 'processing') playState = 'chat_ready'
      log('error', 'chat', `Send failed: ${String(e)}`)
    }
  }

  function handleSelectProfile(profileName: string) {
    selectedProfile = profileName
  }

  async function handleBackToSessions() {
    error = null
    messagesError = null
    if (connectionState === 'connected' || connectionState === 'connecting') {
      try {
        await closeAgent()
      } catch {
        // ignore close errors on teardown
      }
      connectionState = 'disconnected'
    }
    resetPlayPageState()
    selectedSession = null
    agent = null
    chatMessages = []
    playState = 'connecting'
    page = 'sessions'
  }

  async function handleDeleteSession() {
    if (!selectedSession) return
    try {
      loading = true
      error = null
      if (connectionState === 'connected') {
        try {
          await closeAgent()
        } catch {
          // ignore close errors
        }
        connectionState = 'disconnected'
      }
      resetPlayPageState()
      await deleteSession(selectedSession.sessionId)
      log('info', 'sessions', `Session deleted: ${selectedSession.sessionId}`)
      selectedSession = null
      agent = null
      chatMessages = []
      page = 'sessions'
      await handleRefresh()
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Delete session failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  // --- Log handler ---
  function handleClearLogs() {
    logEntries = []
  }

  // --- Profile management handlers ---
  async function handleEnterProfiles() {
    profileMgmtLoading = true
    profileMgmtError = null
    try {
      const resp = await listAgentProfiles(100, '')
      managedProfiles = resp.agentProfiles
    } catch (err) {
      profileMgmtError = err instanceof Error ? err.message : 'Failed to load profiles'
    } finally {
      profileMgmtLoading = false
    }
    page = 'profiles'
  }

  async function handleRefreshProfiles() {
    profileMgmtLoading = true
    profileMgmtError = null
    try {
      const resp = await listAgentProfiles(100, '')
      managedProfiles = resp.agentProfiles
    } catch (err) {
      profileMgmtError = err instanceof Error ? err.message : 'Failed to load profiles'
    } finally {
      profileMgmtLoading = false
    }
  }

  async function handleCreateProfile(req: CreateAgentProfileRequest) {
    await createAgentProfile(req)
    await handleRefreshProfiles()
    const resp = await listAgentProfiles(50, '')
    profiles = resp.agentProfiles
  }

  async function handleDeleteProfile(agentProfileName: string) {
    await deleteAgentProfile(agentProfileName)
    await handleRefreshProfiles()
    const resp = await listAgentProfiles(50, '')
    profiles = resp.agentProfiles
  }

  // --- Window + Screenshot handlers ---
  async function handleLoadWindows() {
    try {
      windows = await listWindows()
    } catch (e: unknown) {
      log('warn', 'windows', `Failed to list windows: ${String(e)}`)
    }
  }

  async function handleCaptureScreenshot() {
    if (selectedWindowHandle == null) return
    capturing = true
    try {
      await bindWindow(selectedWindowHandle)
      const img = await captureScreenshot()
      pendingScreenshot = {
        dataUrl: 'data:image/png;base64,' + img.data,
        data: img.data,
        widthPx: img.widthPx,
        heightPx: img.heightPx,
      }
    } catch (e: unknown) {
      error = String(e)
      log('error', 'screenshot', `Capture failed: ${String(e)}`)
    } finally {
      capturing = false
    }
  }

  function handleRemoveScreenshot() {
    pendingScreenshot = null
  }

  function handleZoom(url: string) {
    zoomedImageUrl = url
  }

  async function handleUpdateProfile(agentProfileName: string, profile: AgentProfile, updateMaskPaths: string[]) {
    await updateAgentProfile(agentProfileName, profile, updateMaskPaths)
    const resp = await listAgentProfiles(100, '')
    managedProfiles = resp.agentProfiles
    profiles = resp.agentProfiles
    // Auto-refresh (FR-026) — ISOLATED error handling
    if (selectedSession && connectionState === 'connected' && playState !== 'processing') {
      try {
        await refreshAgent(selectedSession.sessionId)
      } catch (e: unknown) {
        log('warn', 'agent', `refresh failed (may be in-flight): ${String(e)}`)
      }
    }
  }

  async function handleRefreshAgent() {
    if (!selectedSession) return
    refreshing = true
    try {
      await refreshAgent(selectedSession.sessionId)
      log('info', 'agent', 'Agent refreshed')
    } catch (e: unknown) {
      log('error', 'agent', `Refresh agent failed: ${String(e)}`)
    } finally {
      refreshing = false
    }
  }

  function handleBackFromProfiles() {
    if (selectedSession) {
      page = 'chat'
    } else {
      page = 'sessions'
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
    <button class="btn btn-primary" onclick={handleApplyConfig} disabled={loading}>Apply Config</button>
    <button class="btn" onclick={handleEnterProfiles} disabled={loading}>Agent Profiles</button>
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
  {:else if page === 'chat'}
    <div class="chat-layout">
      <AgentSidebar
        {agent}
        {connectionState}
        {profiles}
        {playState}
        selectedProfile={selectedProfile}
        onSelectProfile={handleSelectProfile}
        onDeleteSession={handleDeleteSession}
        onBack={handleBackToSessions}
        onRefresh={handleRefreshAgent}
        {refreshing}
        {loading}
      />
      <div class="chat-main">
        <div class="chat-top-bar">
          <span class="session-label">Session: <strong>{selectedSession?.sessionId ?? ''}</strong></span>
          <select class="window-select" data-testid="window-select" bind:value={selectedWindowHandle}>
            <option value={undefined} disabled selected={selectedWindowHandle == null}>Select window...</option>
            {#each windows as w}
              <option value={w.handle}>{w.title}</option>
            {/each}
          </select>
          <button class="btn btn-small" data-testid="capture-btn" onclick={handleCaptureScreenshot} disabled={selectedWindowHandle == null || capturing}>
            {capturing ? 'Capturing…' : 'Capture Screenshot'}
          </button>
          {#if playState === 'connection_error'}
            <span class="chat-error" data-testid="chat-connection-error">{messagesError ?? 'Connection failed'}</span>
          {:else if error}
            <span class="chat-error">{error}</span>
          {/if}
        </div>
        <ChatView
          messages={chatMessages}
          {processing}
          {queueCount}
          loadingMessages={playState === 'loading_messages'}
          messagesError={messagesError}
          onSend={handleSendChatText}
          onZoom={handleZoom}
          pendingScreenshot={pendingScreenshot ? { dataUrl: pendingScreenshot.dataUrl, widthPx: pendingScreenshot.widthPx, heightPx: pendingScreenshot.heightPx } : null}
          onRemoveScreenshot={handleRemoveScreenshot}
        />
      </div>
    </div>
  {:else if page === 'profiles'}
    <ProfileManagement
      profiles={managedProfiles}
      loading={profileMgmtLoading}
      error={profileMgmtError}
      onCreate={handleCreateProfile}
      onDelete={handleDeleteProfile}
      onRefresh={handleRefreshProfiles}
      onUpdate={handleUpdateProfile}
      onBack={handleBackFromProfiles}
    />
  {/if}

  <!-- Log Panel (bottom, always visible) -->
  <LogPanel logs={logEntries} onclear={handleClearLogs} />

  {#if zoomedImageUrl}
    <ScreenshotModal imageUrl={zoomedImageUrl} onClose={() => zoomedImageUrl = null} />
  {/if}
</div>

<style>
  .chat-layout {
    display: flex;
    gap: 8px;
    height: 100%;
    overflow: hidden;
  }

  .chat-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .chat-top-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
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

  .chat-error {
    margin-left: auto;
    font-size: 12px;
    color: #ff6b6b;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
