<script lang="ts">
  import { onMount } from 'svelte'
  import type { Session, Agent, AgentFrame, Config, AgentProfile, CreateAgentProfileRequest, AgentOperationFrame, AgentOperationResultFrame } from './api'
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
      chatMessages = entries.map(entry => ({
        messageId: entry.messageId,
        sender: senderFromString(entry.sender),
        type: typeFromString(entry.type),
        content: entry.content,
        timestamp: entry.createTime || new Date().toISOString(),
      }))
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
      const screenshot = frame.userTurn.screenshot
      if (screenshot && screenshot.data) {
        const encoding = screenshot.encoding || 'png'
        const imageUrl = `data:image/${encoding};base64,${screenshot.data}`
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
    try {
      chatMessages = [...chatMessages, {
        messageId: crypto.randomUUID(),
        sender: FrameSender.USER,
        type: 'text',
        content: text,
        timestamp: new Date().toISOString(),
      }]
      processing = true
      playState = 'processing'
      queueCount++
      await sendUserTurn(selectedSession.sessionId, text, [], 0, 0, selectedProfile)
      queueCount = Math.max(0, queueCount - 1)
      log('info', 'chat', `Sent text to agent: ${text.substring(0, 60)}`)
    } catch (e: unknown) {
      error = String(e)
      processing = false
      queueCount = Math.max(0, queueCount - 1)
      if (playState === 'processing') playState = 'chat_ready'
      log('error', 'chat', `Send text failed: ${String(e)}`)
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
  }

  async function handleDeleteProfile(agentProfileName: string) {
    await deleteAgentProfile(agentProfileName)
    await handleRefreshProfiles()
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
        {loading}
      />
      <div class="chat-main">
        <div class="chat-top-bar">
          <span class="session-label">Session: <strong>{selectedSession?.sessionId ?? ''}</strong></span>
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
      onBack={handleBackToSessions}
    />
  {/if}

  <!-- Log Panel (bottom, always visible) -->
  <LogPanel logs={logEntries} onclear={handleClearLogs} />
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
