<script lang="ts">
  import { onMount } from 'svelte'
  import type { Session, Agent, AgentFrame, Config, AgentProfile, CreateAgentProfileRequest, MessagePart, FlowPart, WindowRef, CapturedImage, ChatStreamHandoff } from './api'
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
    openChatStream,
    closeChatStream,
    setDebugMode,
    confirmToolResult,
  } from './api'
  import { openChatEventSource, makeDeduper } from './chat-stream'
  import type { ChunkState, Deduper } from './chat-stream'
  import { log, logDebug, setDebugEnabled, setLogSink } from './logger'
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

  // ChatEntry is one rendered chat row. A content entry carries one or more
  // Parts (a content frame's PartBlock.parts); a warn entry carries a control
  // signal rendered as a warning bubble. mergeKind is an internal hint used to
  // fold consecutive streaming text/thinking chunks into a single bubble.
  type ChatEntry = {
    messageId: string
    sender: FrameSender
    timestamp: string
    agentProfileName?: string
    parts?: MessagePart[]
    warnMessage?: string
    mergeKind?: 'text' | 'thinking' | 'mixed'
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

  // --- Debug mode (FR-001): developer-facing verbose-logging toggle. Not
  // persisted; resets to OFF on page/session exit (FR-002). The frontend owns
  // the flag and mirrors it to the Go backend via the SetDebugMode bound method
  // (specs/022-desktop-debug-mode/research.md D1).
  let debugMode = $state(false)

  // --- Held tool-result IDs (US2): the set of toolIDs currently held by the
  // Go backend for debug confirmation. Populated by game:debug:result-held /
  // result-released events (contracts/debug-control-plane.md §2.3). Passed to
  // ChatView so it renders a "Confirm" control on matching tool-result bubbles
  // (FR-008). Reactive via $state + Set reassignment.
  let heldToolIds = $state<Set<string>>(new Set())

  // --- SSE chat push state ---
  // The chat dialog is delivered over a renderer-initiated EventSource (spec
  // 016) instead of the host→webview `game:frame` channel, which silently
  // dropped frames once the desktop window lost foreground. History replay and
  // live streaming share this single channel.
  let chatStreamHandoff: ChatStreamHandoff | null = $state(null)
  let currentEventSource: EventSource | null = $state(null)
  let openingPromise: Promise<void> | null = $state(null)
  let deduper = $state(makeDeduper())
  let chunkState: Map<string, ChunkState> = $state(new Map())
  let consecutiveErrors = $state(0)
  const ERROR_THRESHOLD = 3

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
    // Defensively clear the typing indicator on session entry. The connect
    // probe then refines it against the agent's real working state
    // (contracts/agent-desktop-channel-contract.md §1).
    processing = false
    // FR-002: debug mode is not persisted; reset to OFF on page/session exit
    // and notify both layers so they stay in sync.
    if (debugMode) {
      debugMode = false
      applyDebugMode()
    }
  }

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

  // handleConfirm releases a held tool result so the Go backend sends it to the
  // agent (FR-009). Called by ChatView's "Confirm" button via the onConfirm
  // callback prop (contracts/debug-control-plane.md §3).
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
      handleRefresh()
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

    // Debug hold events: the Go backend emits game:debug:result-held /
    // result-released when a tool result begins/ends being held for
    // confirmation (contracts/debug-control-plane.md §2). Reassigning a new
    // Set triggers Svelte 5 $state reactivity (contract §2.3).
    const runtime = window.runtime
    if (runtime?.EventsOn) {
      runtime.EventsOn('game:debug:result-held', (payload: unknown) => {
        const p = payload as { toolId?: string }
        if (p?.toolId) {
          heldToolIds = new Set(heldToolIds).add(p.toolId)
        }
      })
      runtime.EventsOn('game:debug:result-released', (payload: unknown) => {
        const p = payload as { toolId?: string }
        if (p?.toolId) {
          const next = new Set(heldToolIds)
          next.delete(p.toolId)
          heldToolIds = next
        }
      })
    }

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
      await openSseStream()
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
      const status = await connectAgent(selectedSession.sessionId)
      connectionState = 'connected'
      // Reconcile the typing indicator against the agent's reported working
      // state (contracts/agent-desktop-channel-contract.md §1): ACTIVE means a
      // turn is genuinely still running, so keep the indicator on; anything
      // else (IDLE/UNSPECIFIED) clears it and returns the page to ready.
      if (status === 'STATUS_SIGNAL_STATUS_ACTIVE') {
        processing = true
      } else {
        processing = false
        playState = 'chat_ready'
      }
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

  // resolveSender normalizes a frame/message sender, which arrives as the
  // protojson enum name string (or, defensively, as a numeric enum).
  function resolveSender(sender: FrameSender | string | undefined): FrameSender {
    if (typeof sender === 'number') return sender
    if (typeof sender === 'string') return senderFromString(sender)
    return FrameSender.SYSTEM
  }

  // homogeneousStreamKind classifies a content frame's parts for the streaming
  // merge decision: a frame of only TextParts or only ThinkingParts may fold
  // into the preceding agent bubble; anything mixed starts a new entry.
  function homogeneousStreamKind(parts: MessagePart[]): 'text' | 'thinking' | 'mixed' {
    if (parts.length === 0) return 'mixed'
    if (parts.every(p => p.text != null)) return 'text'
    if (parts.every(p => p.thinking != null)) return 'thinking'
    return 'mixed'
  }

  // wireChatEventSource builds an EventSource for the chat push stream from a
  // handoff and wires open/frame/error handlers. playState transitions to
  // 'chat_ready' on open (R13 — even for an empty backlog on a first-ever
  // session, since history replays as early chat events). After ERROR_THRESHOLD
  // consecutive errors the stream is re-opened with a fresh token (R10).
  function wireChatEventSource(handoff: ChatStreamHandoff, sessionID: string): EventSource {
    return openChatEventSource(
      handoff.endpoint,
      sessionID,
      handoff.token,
      chunkState,
      deduper,
      {
        onOpen() {
          playState = 'chat_ready'
          consecutiveErrors = 0
        },
        onFrame(frame) {
          handleAgentFrame(frame)
        },
        onError() {
          consecutiveErrors++
          if (consecutiveErrors < ERROR_THRESHOLD) return
          consecutiveErrors = 0
          if (currentEventSource) {
            currentEventSource.close()
            currentEventSource = null
          }
          // R10: re-open with a fresh token. The deduper is intentionally kept
          // so events replayed by the fresh EventSource (which carries no
          // Last-Event-ID) that were already applied are ignored rather than
          // duplicated.
          void reopenChatStream(sessionID)
        },
      },
    )
  }

  // openSseStream opens the chat push stream for the selected session. History
  // replay and live streaming both arrive over this single channel, replacing
  // the one-shot listMessages history fetch. Concurrent opens are collapsed
  // into one in-flight open (C10).
  async function openSseStream(): Promise<void> {
    if (openingPromise) return openingPromise
    const p = doOpenSseStream()
    openingPromise = p
    try {
      await p
    } finally {
      if (openingPromise === p) openingPromise = null
    }
  }

  async function doOpenSseStream(): Promise<void> {
    if (!selectedSession) return
    // Close any existing stream and reset state for a fresh session entry.
    if (currentEventSource) {
      currentEventSource.close()
      currentEventSource = null
    }
    chunkState = new Map()
    deduper = makeDeduper()
    consecutiveErrors = 0
    try {
      const handoff = await openChatStream(selectedSession.sessionId)
      chatStreamHandoff = handoff
      currentEventSource = wireChatEventSource(handoff, selectedSession.sessionId)
    } catch (e: unknown) {
      log('error', 'chat', `Open SSE stream failed: ${String(e)}`)
      playState = 'connection_error'
    }
  }

  // reopenChatStream re-establishes the stream after repeated errors (R10).
  // Partial chunk groups are cleared (the fresh EventSource replays them
  // whole); dedup state is preserved so already-applied events are skipped.
  async function reopenChatStream(sessionID: string): Promise<void> {
    chunkState = new Map()
    try {
      const handoff = await openChatStream(sessionID)
      chatStreamHandoff = handoff
      currentEventSource = wireChatEventSource(handoff, sessionID)
    } catch (e: unknown) {
      log('error', 'chat', `SSE reopen failed: ${String(e)}`)
      playState = 'connection_error'
    }
  }

  // closeSseStream tears down the chat push stream on session leave (F5/F8).
  function closeSseStream(): void {
    if (currentEventSource) {
      currentEventSource.close()
      currentEventSource = null
    }
    // F8: evict any partial chunk groups so a never-completing group does
    // not leak across sessions.
    chunkState = new Map()
    chatStreamHandoff = null
    consecutiveErrors = 0
  }

  // handleMessageParts renders a messageParts frame. One frameId maps to one
  // chat entry containing ALL of the frame's parts (a user-turn frame carrying
  // [TextPart, ImagePart] is a single grouped entry, never split per-part).
  //
  // Streaming exception: the agent emits many small text/thinking chunks as
  // separate messageParts frames. To preserve the legacy single-bubble
  // streaming UX, consecutive agent messageParts frames that are purely
  // TextPart (or purely ThinkingPart) and share the same agentProfileName fold
  // into the preceding entry by concatenating content onto the trailing
  // same-kind part. Every other messageParts frame (image, tool_call,
  // tool_result, mixed) starts a new entry. (data-model.md §4; spec 023 FR-005.)
  function handleMessageParts(frame: AgentFrame, block: { parts?: MessagePart[] }, timestamp: string) {
    const incomingParts = block.parts ?? []
    // Graceful degradation: a messageParts frame with zero parts is a no-op.
    if (incomingParts.length === 0) return

    const sender = resolveSender(frame.sender)
    const profile = frame.agentProfileName
    const kind = homogeneousStreamKind(incomingParts)

    if (sender === FrameSender.AGENT && (kind === 'text' || kind === 'thinking')) {
      const last = chatMessages[chatMessages.length - 1]
      if (last && last.sender === FrameSender.AGENT
          && last.agentProfileName === profile
          && last.mergeKind === kind
          && last.parts && last.parts.length > 0) {
        const trailing = last.parts[last.parts.length - 1]
        const joined = incomingParts
          .map(p => (kind === 'text' ? p.text?.content : p.thinking?.content) ?? '')
          .join('')
        if (kind === 'text' && trailing.text) {
          trailing.text.content += joined
        } else if (kind === 'thinking' && trailing.thinking) {
          trailing.thinking.content += joined
        }
        chatMessages = [...chatMessages]
        return
      }
    }

    chatMessages = [...chatMessages, {
      messageId: frame.frameId ?? crypto.randomUUID(),
      sender,
      timestamp,
      agentProfileName: profile,
      parts: incomingParts,
      mergeKind: kind,
    }]
  }

  function handleAgentFrame(frame: AgentFrame) {
    const timestamp = frame.createTime || new Date().toISOString()
    // FR-004 frontend: surface every inbound chat frame at debug level. A no-op
    // when debug is off (logDebug short-circuits), so this is safe on the hot
    // SSE path.
    logDebug('frontend', 'inbound chat frame', {
      frame_id: frame.frameId,
      sender: frame.sender,
    })
    // A frame carries exactly one payload (protojson flattens the oneof):
    // messageParts (display — rendered) OR flowParts (control — drives
    // execution / turn state, never a chat bubble).
    if (frame.messageParts) {
      handleMessageParts(frame, frame.messageParts, timestamp)
    } else if (frame.flowParts) {
      for (const fp of frame.flowParts.parts ?? []) {
        if (fp.wait) {
          processing = false
          if (playState === 'processing') playState = 'chat_ready'
        } else if (fp.warn) {
          chatMessages = [...chatMessages, {
            messageId: frame.frameId ?? crypto.randomUUID(),
            sender: FrameSender.SYSTEM,
            timestamp,
            warnMessage: fp.warn.message ?? '',
          }]
        }
        // Operation FlowParts (mouse/keyboard) and status FlowParts are
        // executed by the Go backend / are lifecycle no-ops here — they never
        // render as conversation entries (spec 023 FR-005).
      }
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
    if (!nowConnected) {
      playState = 'connection_error'
      messagesError = 'Connection failed. Retry to send your message.'
      return
    }
    // The SSE chat stream auto-reconnects independently of the agent
    // WebSocket; re-establishing the WS path needs no history re-fetch here.
    const optimisticIds: string[] = []
    try {
      // Optimistic user turn: mirror the backend's single user-turn frame,
      // which carries [TextPart, ImagePart] as one PartBlock. One local id →
      // one entry holding all submitted parts (text and/or screenshot).
      const optimisticParts: MessagePart[] = []
      if (text.trim()) optimisticParts.push({ text: { content: text } })
      if (pendingScreenshot) {
        optimisticParts.push({
          image: { data: pendingScreenshot.data, encoding: 'IMAGE_ENCODING_PNG' },
        })
      }
      if (optimisticParts.length > 0) {
        const msgId = crypto.randomUUID()
        optimisticIds.push(msgId)
        chatMessages = [...chatMessages, {
          messageId: msgId,
          sender: FrameSender.USER,
          timestamp: new Date().toISOString(),
          parts: optimisticParts,
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
    const sessionId = selectedSession?.sessionId
    // F5 ordering: tear down the SSE stream first, then the agent, then the
    // chat-stream resource on the backend.
    closeSseStream()
    if (connectionState === 'connected' || connectionState === 'connecting') {
      try {
        await closeAgent()
      } catch {
        // ignore close errors on teardown
      }
      connectionState = 'disconnected'
    }
    if (sessionId) {
      try {
        await closeChatStream(sessionId)
      } catch {
        // ignore close errors on teardown
      }
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
    const sessionId = selectedSession.sessionId
    try {
      loading = true
      error = null
      // F5 ordering: SSE stream → agent → chat-stream resource.
      closeSseStream()
      if (connectionState === 'connected') {
        try {
          await closeAgent()
        } catch {
          // ignore close errors
        }
        connectionState = 'disconnected'
      }
      try {
        await closeChatStream(sessionId)
      } catch {
        // ignore close errors
      }
      resetPlayPageState()
      await deleteSession(sessionId)
      log('info', 'sessions', `Session deleted: ${sessionId}`)
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
          <label class="debug-toggle" data-testid="debug-toggle" title="Toggle debug-level log output (FR-001)">
            <input type="checkbox" bind:checked={debugMode} onchange={applyDebugMode} />
            <span>Debug</span>
          </label>
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
          {heldToolIds}
          onConfirm={handleConfirm}
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
