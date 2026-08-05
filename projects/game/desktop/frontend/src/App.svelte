<script lang="ts">
  import { onMount } from 'svelte'
  import type { Session, Team, TeamAgent, TeamProfile, CreateTeamProfileRequest, TeamFrame, Config, MessagePart, FlowPart, WindowRef, CapturedImage, ChatStreamHandoff, HeldOperation, DebugResultHeldPayload, DebugResultReleasedPayload } from './api'
  import { MessageRole, TEMPLATE_SAOLEI, TEMPLATES } from './api'
  import {
    setConfig,
    createSession,
    listSessions,
    deleteSession,
    getTeam,
    createTeam,
    connect,
    closeAgent,
    refreshTeam,
    listWindows,
    setSelectedWindow,
    captureScreenshot,
    sendUserTurn,
    listMessages,
    openChatStream,
    closeChatStream,
    listTeamProfiles,
    createTeamProfile,
    updateTeamProfile,
    deleteTeamProfile,
    setDebugMode,
    confirmToolResult,
  } from './api'
  import { openChatEventSource, makeDeduper } from './chat-stream'
  import type { ChunkState, Deduper } from './chat-stream'
  import { log, logDebug, setDebugEnabled, setLogSink } from './logger'
  import type { LogEntry } from './logger'
  import SessionList from './components/SessionList.svelte'
  import ChatView from './components/ChatView.svelte'
  import OperationConfirmDrawer from './components/OperationConfirmDrawer.svelte'
  import TeamSidebar from './components/TeamSidebar.svelte'
  import ProfileManagement from './components/ProfileManagement.svelte'
  import LogPanel from './components/LogPanel.svelte'
  import ScreenshotModal from './components/ScreenshotModal.svelte'
  import ProfileSelectDialog from './components/ProfileSelectDialog.svelte'

  // --- Page state ---
  // The template is the TOP-LEVEL control plane: it is a local constant and
  // switching it MUST NOT issue any template-list API request (FR-024).
  // Session listing/creation are scoped to the active template; the profiles
  // page is the TeamProfile config surface, specialized per template (FR-026/
  // FR-029 — specs/031-team-template-mode/spec.md).
  let page = $state<'sessions' | 'chat' | 'profiles'>('sessions')
  let template = $state(TEMPLATE_SAOLEI)

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
  // `agent` (D12) names the team agent the entry belongs to (replaces the
  // former agentProfileName) — the per-tab routing dimension (FR-025).
  type ChatEntry = {
    messageId: string
    role: MessageRole
    timestamp: string
    agent?: string
    parts?: MessagePart[]
    warnMessage?: string
    mergeKind?: 'text' | 'thinking' | 'mixed'
  }

  // --- App-level state ---
  let selectedSession: Session | null = $state(null)
  let sessions: Session[] = $state([])
  let team: Team | null = $state(null)
  let connectionState: ConnectionState = $state('disconnected')
  let logEntries: LogEntry[] = $state([])
  let loading = $state(false)
  let error: string | null = $state(null)

  // --- Team / multi-tab chat state ---
  // teamAgents comes from getTeam().agents (typed TeamAgent[]) — the desktop
  // does NOT hardcode agent names; the tab set is driven by the backend
  // (FR-025). selectedAgent is the active tab; user input routes to it and
  // only when it accepts user input (FR-032).
  let teamAgents: TeamAgent[] = $state([])
  let selectedAgent = $state('')
  // Per-agent message buckets — frames are routed by TeamFrame.agent.
  let chatMessages: Record<string, ChatEntry[]> = $state({})
  // Dedupe between the listMessages history fetch and the chat-stream seed
  // replay (same messageId/frameId): the Go chatstream seeds the session
  // stream with ONE agent's partition and re-delivers it on connect
  // (desktop/internal/chatstream/stream.go), which would otherwise duplicate
  // the history rows for that agent. Reset on session entry.
  let renderedMessageIds: Set<string> = new Set()
  // The agent the session chat stream was opened (seeded) with. Seed-replay
  // frames carry NO agent field (Go SeedFromHistory omits it), so they are
  // routed to this agent.
  let seedAgent = $state('')

  let processing = $state(false)
  let queueCount = $state(0)
  let pendingMessageIds: string[] = $state([])
  let playState = $state<PlayState>('connecting')
  let messagesError = $state<string | null>(null)

  // --- Debug mode (FR-001): developer-facing verbose-logging toggle. Not
  // persisted; resets to OFF on page/session exit (FR-002). The frontend owns
  // the flag and mirrors it to the Go backend via the SetDebugMode bound method
  // (specs/022-desktop-debug-mode/research.md D1).
  let debugMode = $state(false)

  // --- Held operations awaiting confirmation (US4 debug drawer,
  // contracts/debug-drawer-contract.md §3.1). The session-top Confirm drawer
  // is driven entirely by the operation channel: each entry's `toolId` is the
  // bridge-minted operation id (NOT any conversation tool_call.id — decoupled
  // per research.md D10/D11). Populated by the EXTENDED `game:debug:result-held`
  // payload and removed on `result-released`. Arrival order is preserved so
  // multiple simultaneous holds stack in the drawer (§5). Reactive via $state.
  let heldOperations = $state<HeldOperation[]>([])

  // --- SSE chat push state ---
  // The chat dialog is delivered over a renderer-initiated EventSource (spec
  // 016) instead of the host→webview `game:frame` channel, which silently
  // dropped frames once the desktop window lost foreground. History replay and
  // live streaming share this single channel. The Go chatstream is per-session
  // (one stream, token rotation on every open), so the desktop opens ONE
  // stream per session (seeded by the first team agent) and routes inbound
  // frames by TeamFrame.agent into per-agent tabs.
  let chatStreamHandoff: ChatStreamHandoff | null = $state(null)
  let currentEventSource: EventSource | null = $state(null)
  let openingPromise: Promise<void> | null = $state(null)
  let deduper = $state(makeDeduper())
  let chunkState: Map<string, ChunkState> = $state(new Map())
  let consecutiveErrors = $state(0)
  const ERROR_THRESHOLD = 3

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

  // --- TeamProfile management state (Phase 7 T028) ---
  // The profiles page manages TeamProfiles of the CURRENT template (the page
  // is specialized per template — FR-026/FR-029; managedProfiles is scoped to
  // `template`, which always equals the template used to enter the page).
  let managedProfiles: TeamProfile[] = $state([])
  let profileMgmtLoading = $state(false)
  let profileMgmtError: string | null = $state(null)

  // Profile selection dialog state — shown when entering a session whose Team
  // does not exist yet, so the user picks the TeamProfile to create it with.
  let showProfileSelect = $state(false)
  let profileSelectProfiles: TeamProfile[] = $state([])
  let profileSelectLoading = $state(false)
  let profileSelectError: string | null = $state(null)

  function resetPlayPageState() {
    pendingScreenshot = null
    selectedWindowHandle = undefined
    windows = []
    // Defensively clear the typing indicator on session entry. The connect
    // probe then refines it against the agent's real working state
    // (contracts/agent-desktop-channel-contract.md §1).
    processing = false
    // Phase 5: the pending-queue count is backend-driven by QueueSignal
    // (T011). Re-entry MUST start from zero so the indicator reflects only
    // messages genuinely still pending for this session
    // (specs/030-queued-chat-input/spec.md edge case:
    // "Session re-entry with a non-empty queue" — no stale count from a prior
    // view). The backend replays the real depth if a turn is in flight.
    queueCount = 0
    pendingMessageIds = []
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
    // Per-session chat state (multi-tab).
    chatMessages = {}
    renderedMessageIds = new Set()
  }

  // Push the selected window handle to the backend on every dropdown change.
  // Selecting a window is sufficient to make it the target for every
  // screenshot and operation — there is no separate "bind" step (spec 025
  // FR-001/FR-006, contracts/window-select-contract.md §2.1). Re-selecting a
  // different window retargets subsequent ops (FR-004). The undefined initial
  // value and the resetPlayPageState clear are skipped (no selection).
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
    // result-released when an operation result begins/ends being held for
    // confirmation (contracts/debug-drawer-contract.md §2/§3.1). The held
    // payload is extended with the operation descriptor (kind/summary/details)
    // so the session-top drawer can render the request content. `toolId` is
    // the operation-channel id (decoupled from the conversation tool_call.id).
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

  // handleTemplateChange switches the top-level template control plane. The
  // template list is a LOCAL constant (TEMPLATES) — switching performs NO
  // network request (FR-024). Sessions are template-scoped, so switching
  // returns to the sessions page and re-lists for the new template.
  function handleTemplateChange() {
    selectedSession = null
    team = null
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

  async function handleCreate() {
    try {
      loading = true
      error = null
      const session = await createSession(template)
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
      await deleteSession(template, sessionId)
      log('info', 'sessions', `Session deleted: ${sessionId}`)
      await handleRefresh()
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Delete failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  function agentAcceptsInput(agentName: string): boolean {
    const a = teamAgents.find(a => a.name === agentName)
    return a?.acceptsUserInput ?? false
  }

  async function handleSelectSession(session: Session) {
    resetPlayPageState()
    selectedSession = session
    team = null
    teamAgents = []
    selectedAgent = ''
    seedAgent = ''
    error = null
    messagesError = null
    connectionState = 'disconnected'

    page = 'chat'
    handleLoadWindows()
    playState = 'connecting'

    // Team must exist before connect (FR-033). When the session has no Team
    // yet, the user picks the TeamProfile to create it with (replacing the
    // former hardcoded `default` profile auto-creation).
    try {
      const t = await getTeam(template, session.sessionId)
      await continueSessionEntry(t)
    } catch (e) {
      // GetTeam failed (typically NOT_FOUND — team not yet created). Open the
      // profile selection dialog; creation happens on user confirm.
      log('info', 'team', `GetTeam failed (${String(e)}); opening profile selection`)
      showProfileSelect = true
      await loadProfilesForSelect()
    }
  }

  // continueSessionEntry finishes the session-entry flow once the Team exists:
  // wires the agent tabs (FR-025), connects, and seeds the chat stream. Called
  // both when the Team already exists and after the user picks a TeamProfile
  // to create it.
  async function continueSessionEntry(t: Team) {
    team = t
    // Tab set comes from Team.agents — never hardcoded (FR-025).
    teamAgents = t.agents ?? []
    if (teamAgents.length === 0) {
      playState = 'connection_error'
      messagesError = 'Team has no agents.'
      return
    }
    // Default tab = first team agent. The session chat stream is seeded with
    // this agent's message partition.
    selectedAgent = teamAgents[0].name
    seedAgent = teamAgents[0].name

    // The casts widen the $state type: TS narrows `connectionState` to the
    // literal 'disconnected' after the assignment above (control-flow
    // analysis), so a bare comparison with 'connected' would be flagged as
    // TS2367 (no overlap). The explicit ConnectionState cast restores the
    // declared union type for the comparison.
    if ((connectionState as ConnectionState) !== 'connected') {
      await handleConnect()
    }

    if ((connectionState as ConnectionState) === 'connected') {
      // The stream delivers the seeded agent's history replay + live frames;
      // per-agent history for the remaining tabs is fetched separately.
      await openSseStream()
      await loadAgentHistories()
    } else {
      playState = 'connection_error'
    }
  }

  // handleProfileSelected creates the session's Team with the chosen
  // TeamProfile (full resource name from the dialog), then continues the entry
  // flow. A concurrent create (multi-tab) resolves via a re-read — CreateTeam
  // with the same profile is idempotent (api-contract §2.2).
  async function handleProfileSelected(profileFullName: string) {
    if (!selectedSession) return
    showProfileSelect = false
    const tpl = template
    const sessionId = selectedSession.sessionId
    try {
      const t = await createTeam(tpl, sessionId, profileFullName)
      await continueSessionEntry(t)
    } catch (e) {
      log('warn', 'team', `CreateTeam failed (${String(e)}); re-reading team`)
      try {
        const t = await getTeam(tpl, sessionId)
        await continueSessionEntry(t)
      } catch {
        playState = 'connection_error'
        messagesError = 'Failed to create team. Retry to enter the session.'
      }
    }
  }

  // handleProfileSelectCancel aborts session entry and returns to the sessions
  // list (the Team is not created).
  function handleProfileSelectCancel() {
    showProfileSelect = false
    profileSelectProfiles = []
    profileSelectError = null
    void handleBackToSessions()
  }

  // handleProfileSelectGoToProfiles leaves the aborted entry for the Team
  // Profile management page so the user can create a profile first; they
  // re-enter the session afterwards.
  async function handleProfileSelectGoToProfiles() {
    showProfileSelect = false
    profileSelectProfiles = []
    profileSelectError = null
    selectedSession = null
    team = null
    teamAgents = []
    selectedAgent = ''
    seedAgent = ''
    await handleEnterProfiles()
  }

  // loadAgentHistories fetches each team agent's message partition
  // (ListMessages is per-agent, FR-005) into its tab bucket. Entries already
  // rendered from the stream seed replay (same messageId/frameId) are skipped.
  async function loadAgentHistories() {
    if (!selectedSession) return
    const tpl = template
    const sessionId = selectedSession.sessionId
    for (const agent of teamAgents) {
      try {
        // Defensive: an empty agent partition may come back as null (Wails
        // serializes a nil Go slice as null); iterating it would throw.
        const msgs = (await listMessages(tpl, sessionId, agent.name)) ?? []
        const entries: ChatEntry[] = []
        for (const m of msgs) {
          const mid = m.messageId
          if (mid && renderedMessageIds.has(mid)) continue
          if (mid) renderedMessageIds.add(mid)
          const parts = m.content?.parts ?? []
          if (parts.length === 0) continue
          entries.push({
            messageId: mid ?? crypto.randomUUID(),
            role: resolveRole(m.role),
            timestamp: m.createTime ?? '',
            agent: agent.name,
            parts,
          })
        }
        if (entries.length > 0) {
          chatMessages = {
            ...chatMessages,
            [agent.name]: [...(chatMessages[agent.name] ?? []), ...entries],
          }
        }
        log('info', 'chat', `Loaded ${entries.length} history messages for agent ${agent.name}`)
      } catch (e: unknown) {
        log('warn', 'chat', `Failed to load history for agent ${agent.name}: ${String(e)}`)
      }
    }
  }

  async function handleConnect() {
    if (!selectedSession) return
    try {
      connectionState = 'connecting'
      const status = await connect(template, selectedSession.sessionId)
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
      log('info', 'team', 'Session connected via WebSocket')
    } catch (e: unknown) {
      connectionState = 'error'
      error = String(e)
      log('error', 'team', `Connect failed: ${String(e)}`)
    }
  }

  function roleFromString(raw: string): MessageRole {
    if (raw === 'MESSAGE_ROLE_USER') return MessageRole.USER
    if (raw === 'MESSAGE_ROLE_AGENT') return MessageRole.AGENT
    return MessageRole.AGENT
  }

  // resolveRole normalizes a frame/message role, which arrives as the
  // protojson enum name string (or, defensively, as a numeric enum). The
  // default is AGENT: live messageParts frames are always agent-produced, and
  // flowParts control frames never render as conversation entries
  // (specs/035-proto-contract-refine/contracts/frame-split.md §3.2).
  function resolveRole(role: MessageRole | string | undefined): MessageRole {
    if (typeof role === 'number') return role
    if (typeof role === 'string') return roleFromString(role)
    return MessageRole.AGENT
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

  // frameBucketAgent resolves the per-agent tab a frame belongs to (D12
  // routing). Live frames carry TeamFrame.agent; seeded history replay frames
  // carry no agent field (Go SeedFromHistory omits it), so they fall back to
  // seedAgent. A frame whose agent is NOT in Team.agents (unknown agent edge
  // case, desktop-contract §2.2) is routed into the default (first) tab with a
  // warning — never dropped, never crashes.
  function frameBucketAgent(frame: TeamFrame): string {
    const raw = frame.agent ?? seedAgent
    if (raw && teamAgents.some(a => a.name === raw)) return raw
    const fallback = teamAgents[0]?.name ?? ''
    if (raw && raw !== fallback) {
      log('warn', 'chat', `Frame from unknown agent "${raw}" routed to default tab "${fallback}"`)
    }
    return fallback
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
      // The stream is seeded with the first team agent's partition (the Go
      // chatstream is per-session — see api.ts openChatStream).
      const handoff = await openChatStream(selectedSession.sessionId, seedAgent)
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
      const handoff = await openChatStream(sessionID, seedAgent)
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

  // handleMessageParts renders a messageParts frame into the tab bucket of the
  // frame's agent (D12 routing). One frameId maps to one chat entry containing
  // ALL of the frame's parts (a user-turn frame carrying [TextPart, ImagePart]
  // is a single grouped entry, never split per-part).
  //
  // Streaming exception: the agent emits many small text/thinking chunks as
  // separate messageParts frames. To preserve the legacy single-bubble
  // streaming UX, consecutive agent messageParts frames that are purely
  // TextPart (or purely ThinkingPart) and share the same agent fold into the
  // preceding entry by concatenating content onto the trailing same-kind part.
  // Every other messageParts frame (image, tool_call, tool_result, mixed)
  // starts a new entry. (data-model.md §4; spec 023 FR-005.)
  function handleMessageParts(frame: TeamFrame, block: { parts?: MessagePart[] }, timestamp: string) {
    const incomingParts = block.parts ?? []
    // Graceful degradation: a messageParts frame with zero parts is a no-op.
    if (incomingParts.length === 0) return

    const agent = frameBucketAgent(frame)
    if (!agent) return

    // Dedupe history-vs-seed overlap (see renderedMessageIds).
    if (frame.frameId && renderedMessageIds.has(frame.frameId)) return
    if (frame.frameId) renderedMessageIds.add(frame.frameId)

    // Real-time messageParts frames are always agent-produced (role AGENT);
    // history seed-replay frames carry their Message.role copy
    // (specs/035-proto-contract-refine/research.md R3).
    const role = resolveRole(frame.role)
    const kind = homogeneousStreamKind(incomingParts)
    const list = chatMessages[agent] ?? []

    if (role === MessageRole.AGENT && (kind === 'text' || kind === 'thinking')) {
      const last = list[list.length - 1]
      if (last && last.role === MessageRole.AGENT
          && last.agent === agent
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
        chatMessages = { ...chatMessages, [agent]: [...list] }
        return
      }
    }

    chatMessages = {
      ...chatMessages,
      [agent]: [...list, {
        messageId: frame.frameId ?? crypto.randomUUID(),
        role,
        timestamp,
        agent,
        parts: incomingParts,
        mergeKind: kind,
      }],
    }
  }

  function handleAgentFrame(frame: TeamFrame) {
    const timestamp = frame.createTime || new Date().toISOString()
    // FR-004 frontend: surface every inbound chat frame at debug level. A no-op
    // when debug is off (logDebug short-circuits), so this is safe on the hot
    // SSE path.
    logDebug('frontend', 'inbound chat frame', {
      frame_id: frame.frameId,
      role: frame.role,
      agent: frame.agent,
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
          // The backend TurnLoop emits `wait` only when the per-session queue
          // is fully drained (specs/030-queued-chat-input/contracts/
          // queue-channel-contract.md §3), after a QueueSignal(0). The count
          // is therefore already 0 here; this clear is defensive (it guards a
          // stream that somehow reached `wait` without the trailing
          // QueueSignal, e.g. an older agent).
          queueCount = 0
          pendingMessageIds = []
          if (playState === 'processing') playState = 'chat_ready'
        } else if (fp.warn) {
          const agent = frameBucketAgent(frame)
          chatMessages = {
            ...chatMessages,
            [agent]: [...(chatMessages[agent] ?? []), {
              messageId: frame.frameId ?? crypto.randomUUID(),
              // Control-signal warn entry: role is unused by ChatView (warn
              // bubbles render from warnMessage only); AGENT is the server-side
              // origin of the signal.
              role: MessageRole.AGENT,
              timestamp,
              warnMessage: fp.warn.message ?? '',
            }],
          }
        } else if (fp.queue) {
          // Phase 5 (T011): the pending-queue count is now BACKEND-DRIVEN by
          // QueueSignal.queued_count (replacing the Phase 3 optimistic ++).
          // On every depth change the agent pushes this signal
          // (specs/030-queued-chat-input/contracts/queue-channel-contract.md §2).
          // Consume transition (specs/030-queued-chat-input/spec.md FR-009):
          // when the depth drops, the drained messages are consumed by the
          // next turn — transition them out of the pending list so they lose
          // the pending visual mark and render as normal user turns. The
          // TurnLoop drains ALL buffered messages into one combined turn (depth
          // N→0), so a decrease to 0 clears the entire pending list.
          const depth = fp.queue.queuedCount ?? 0
          queueCount = depth
          while (pendingMessageIds.length > depth) {
            pendingMessageIds.shift()
          }
        }
        // Operation FlowParts (mouse/keyboard) and status FlowParts are
        // executed by the Go backend / are lifecycle no-ops here — they never
        // render as conversation entries (spec 023 FR-005).
      }
    }
  }

  async function handleSendChatText(text: string) {
    if (!selectedSession) return
    // FR-032: input routes to the currently selected agent, and only when it
    // accepts user input (saolei: player; planner tabs are blocked in the UI —
    // this guard is the second line of defense).
    const targetAgent = selectedAgent
    if (!targetAgent || !agentAcceptsInput(targetAgent)) {
      messagesError = 'This agent does not accept user input.'
      return
    }
    // Auto-connect fallback if WS dropped (sendUserTurn relies on the backend
    // connection). The Team already exists from session entry (FR-033) —
    // reconnect needs no create-if-missing here; handleConnect's error path
    // covers a missing-team failure.
    const wasConnected = connectionState === 'connected'
    if (!wasConnected) {
      playState = 'connecting'
      await handleConnect()
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
    // queued === true means an agent turn is already in flight, so this
    // submission is buffered by the backend TurnLoop
    // (specs/030-queued-chat-input/spec.md FR-002) and rendered in a pending
    // visual state (specs/030-queued-chat-input/spec.md FR-008). Phase 5 (T011)
    // drives the queue COUNT from the backend QueueSignal (no optimistic ++);
    // the frontend still tracks WHICH messages are pending optimistically
    // (pendingMessageIds) so it can visually mark them pending
    // (specs/030-queued-chat-input/spec.md FR-008) and transition them on
    // consume (specs/030-queued-chat-input/spec.md FR-009).
    // (specs/030-queued-chat-input/spec.md FR-010 — removing a queued message
    // — is intentionally NOT implemented: once submitted the message cannot be
    // removed from the backend queue.)
    // The frame is always sent immediately — `SendUserTurn` stays non-blocking
    // (specs/015-desktop-agent-refinement/spec.md) and the backend decides
    // buffer-vs-run.
    const queued = processing
    try {
      // Optimistic user turn: mirror the backend's single user-turn frame,
      // which carries [TextPart, ImagePart] as one PartBlock. One local id →
      // one entry holding all submitted parts (text and/or screenshot). The
      // entry lands in the target agent's tab bucket.
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
        chatMessages = {
          ...chatMessages,
          [targetAgent]: [...(chatMessages[targetAgent] ?? []), {
            messageId: msgId,
            role: MessageRole.USER,
            timestamp: new Date().toISOString(),
            agent: targetAgent,
            parts: optimisticParts,
          }],
        }
        // Track the pending message id (FIFO) so ChatView can visually mark it
        // pending (specs/030-queued-chat-input/spec.md FR-008) and so a later
        // QueueSignal depth drop can transition it to normal
        // (specs/030-queued-chat-input/spec.md FR-009). The count itself is
        // backend-driven.
        if (queued) {
          pendingMessageIds = [...pendingMessageIds, msgId]
        }
      }
      // `processing` stays true across the queued-turn boundary and is cleared
      // only by the terminal `wait` (specs/030-queued-chat-input/spec.md
      // FR-002): the backend emits `wait` solely on full drain, so re-asserting
      // true here is a no-op when queued and the correct "turn started" signal
      // otherwise.
      processing = true
      playState = 'processing'
      const screenshotData = pendingScreenshot?.data ?? ''
      const screenshotWidthPx = pendingScreenshot?.widthPx ?? 0
      const screenshotHeightPx = pendingScreenshot?.heightPx ?? 0
      pendingScreenshot = null
      await sendUserTurn(
        template,
        selectedSession.sessionId,
        text,
        screenshotData,
        screenshotWidthPx,
        screenshotHeightPx,
        targetAgent,
      )
      log('info', 'chat', `Sent to agent ${targetAgent}: ${text.substring(0, 60)}`)
    } catch (e: unknown) {
      if (optimisticIds.length > 0) {
        const idSet = new Set(optimisticIds)
        const bucket = chatMessages[targetAgent] ?? []
        chatMessages = {
          ...chatMessages,
          [targetAgent]: bucket.filter(m => !idSet.has(m.messageId)),
        }
        // Roll back the pending-tracking entry for the failed submission too.
        pendingMessageIds = pendingMessageIds.filter(id => !idSet.has(id))
      }
      error = String(e)
      // A queued submission failing MUST NOT disturb the in-flight turn:
      // leave `processing` true so the typing indicator still reflects the
      // still-running turn. The backend QueueSignal is authoritative for the
      // count, so no local count rollback is needed (the backend never grew
      // the buffer for a send that failed to arrive). Only a first-message
      // (idle) send failure returns the page to ready.
      if (!queued) {
        processing = false
        if (playState === 'processing') playState = 'chat_ready'
      }
      log('error', 'chat', `Send failed: ${String(e)}`)
    }
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
    team = null
    teamAgents = []
    selectedAgent = ''
    seedAgent = ''
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
      await deleteSession(template, sessionId)
      log('info', 'sessions', `Session deleted: ${sessionId}`)
      selectedSession = null
      team = null
      teamAgents = []
      selectedAgent = ''
      seedAgent = ''
      page = 'sessions'
      await handleRefresh()
    } catch (e: unknown) {
      error = String(e)
      log('error', 'sessions', `Delete session failed: ${String(e)}`)
    } finally {
      loading = false
    }
  }

  // --- Team handlers ---
  // handleRefreshTeam triggers RefreshTeam (FR-018): the backend clears the
  // session's short-term memory. The local per-agent buckets and dedup state
  // are reset and histories are re-read so the tabs reflect the cleared state
  // (the strategy long-term memory is unaffected).
  async function handleRefreshTeam() {
    if (!selectedSession) return
    refreshing = true
    try {
      await refreshTeam(template, selectedSession.sessionId)
      log('info', 'team', 'Team refreshed (short-term memory cleared, FR-018)')
      chatMessages = {}
      renderedMessageIds = new Set()
      await loadAgentHistories()
    } catch (e: unknown) {
      log('error', 'team', `Refresh team failed: ${String(e)}`)
    } finally {
      refreshing = false
    }
  }

  // --- Log handler ---
  function handleClearLogs() {
    logEntries = []
  }

  // --- TeamProfile management handlers (Phase 7 T028) ---
  // The profiles page manages TeamProfiles of the CURRENT template via the
  // TeamProfile Wails bindings (T024 — projects/game/desktop/frontend/src/
  // api.ts); the page is specialized per template (FR-026/FR-029).
  // loadProfilesForSelect feeds the session-entry profile selection dialog
  // (same single-page list shape as the profiles page).
  async function loadProfilesForSelect() {
    profileSelectLoading = true
    profileSelectError = null
    try {
      const resp = await listTeamProfiles(template, 100, '')
      profileSelectProfiles = resp.teamProfiles
    } catch (err) {
      profileSelectError = err instanceof Error ? err.message : 'Failed to load team profiles'
    } finally {
      profileSelectLoading = false
    }
  }

  async function handleEnterProfiles() {
    profileMgmtLoading = true
    profileMgmtError = null
    try {
      const resp = await listTeamProfiles(template, 100, '')
      managedProfiles = resp.teamProfiles
    } catch (err) {
      profileMgmtError = err instanceof Error ? err.message : 'Failed to load team profiles'
    } finally {
      profileMgmtLoading = false
    }
    page = 'profiles'
  }

  async function handleRefreshProfiles() {
    profileMgmtLoading = true
    profileMgmtError = null
    try {
      const resp = await listTeamProfiles(template, 100, '')
      managedProfiles = resp.teamProfiles
    } catch (err) {
      profileMgmtError = err instanceof Error ? err.message : 'Failed to load team profiles'
    } finally {
      profileMgmtLoading = false
    }
  }

  async function handleCreateTeamProfile(req: CreateTeamProfileRequest) {
    await createTeamProfile(template, req)
    await handleRefreshProfiles()
  }

  async function handleUpdateTeamProfile(profileName: string, profile: TeamProfile, updateMaskPaths: string[]) {
    await updateTeamProfile(template, profileName, profile, updateMaskPaths)
    await handleRefreshProfiles()
  }

  async function handleDeleteTeamProfile(profileName: string) {
    await deleteTeamProfile(template, profileName)
    await handleRefreshProfiles()
  }

  function handleBackFromProfiles() {
    page = 'sessions'
  }

  // --- Window + Screenshot handlers ---
  async function handleLoadWindows() {
    try {
      windows = await listWindows()
    } catch (e: unknown) {
      log('warn', 'windows', `Failed to list windows: ${String(e)}`)
    }
  }

  // handleCaptureScreenshot captures the selected window and attaches the
  // screenshot to the next user message. The selected window is used directly
  // — there is no separate "bind" step (spec 025 FR-001/FR-006). The Capture
  // affordance remains a user-initiated "attach a screenshot to my next
  // message" action only; it is NOT a prerequisite for operations.
  async function handleCaptureScreenshot() {
    if (selectedWindowHandle == null) return
    capturing = true
    try {
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
        <!-- Navigation entry to the profiles page (Phase 7 T028): loads the
             current template's TeamProfile list, then opens the page
             (FR-026/FR-029). -->
        <button
          class="btn btn-small"
          data-testid="team-profiles-btn"
          onclick={handleEnterProfiles}
          disabled={loading || profileMgmtLoading}
        >
          Team Profiles
        </button>
      </div>
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
    </div>
  {:else if page === 'chat'}
    <div class="chat-layout">
      <TeamSidebar
        {team}
        {connectionState}
        selectedAgent={selectedAgent}
        onSelectAgent={(name) => (selectedAgent = name)}
        onDeleteSession={handleDeleteSession}
        onBack={handleBackToSessions}
        onRefresh={handleRefreshTeam}
        {refreshing}
        {loading}
      />
      <div class="chat-main">
        <div class="chat-top-bar">
          <span class="session-label">Session: <strong>{selectedSession?.sessionId ?? ''}</strong></span>
          <!-- Multi-tab conversation: one tab per team agent, driven by
               Team.agents — never hardcoded (FR-025). -->
          <div class="agent-tabs" data-testid="agent-tabs">
            {#each teamAgents as agent (agent.name)}
              <button
                class="agent-tab"
                class:active={agent.name === selectedAgent}
                onclick={() => (selectedAgent = agent.name)}
                data-testid="agent-tab"
              >
                {agent.name}
                {#if !agent.acceptsUserInput}
                  <span class="tab-badge observe" title="Does not accept user input (FR-032)">observe</span>
                {/if}
              </button>
            {/each}
          </div>
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
        <OperationConfirmDrawer
          heldOperations={heldOperations}
          onConfirm={handleConfirm}
        />
        <ChatView
          messages={chatMessages[selectedAgent] ?? []}
          {processing}
          {queueCount}
          pendingMessageIds={pendingMessageIds}
          loadingMessages={playState === 'loading_messages'}
          messagesError={messagesError}
          onSend={handleSendChatText}
          onZoom={handleZoom}
          pendingScreenshot={pendingScreenshot ? { dataUrl: pendingScreenshot.dataUrl, widthPx: pendingScreenshot.widthPx, heightPx: pendingScreenshot.heightPx } : null}
          onRemoveScreenshot={handleRemoveScreenshot}
          inputEnabled={agentAcceptsInput(selectedAgent)}
        />
      </div>
    </div>
  {:else if page === 'profiles'}
    <!-- TeamProfile config page, specialized per template (FR-026/FR-029):
         saolei renders only the player/planner model form (FR-027) — the
         typed oneof variant drives the form (contracts/desktop-contract.md §3). -->
    <ProfileManagement
      {template}
      profiles={managedProfiles}
      loading={profileMgmtLoading}
      error={profileMgmtError}
      onCreate={handleCreateTeamProfile}
      onDelete={handleDeleteTeamProfile}
      onRefresh={handleRefreshProfiles}
      onUpdate={handleUpdateTeamProfile}
      onBack={handleBackFromProfiles}
    />
  {/if}

  <!-- Log Panel (bottom, always visible) -->
  <LogPanel logs={logEntries} onclear={handleClearLogs} />

  {#if zoomedImageUrl}
    <ScreenshotModal imageUrl={zoomedImageUrl} onClose={() => zoomedImageUrl = null} />
  {/if}

  {#if showProfileSelect}
    <ProfileSelectDialog
      profiles={profileSelectProfiles}
      loading={profileSelectLoading}
      error={profileSelectError}
      onSelect={handleProfileSelected}
      onCancel={handleProfileSelectCancel}
      onRefresh={loadProfilesForSelect}
      onGoToProfiles={handleProfileSelectGoToProfiles}
    />
  {/if}
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
    flex-wrap: wrap;
  }

  .session-label {
    font-size: 12px;
    color: #a0a0b0;
  }

  .session-label strong {
    color: #e0e0e0;
  }

  .agent-tabs {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .agent-tab {
    padding: 4px 10px;
    font-size: 12px;
    background: #0f3460;
    border: 1px solid #1a3a6e;
    border-radius: 4px;
    color: #a0a0b0;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .agent-tab:hover:not(:disabled) {
    background: #1a4a80;
    border-color: #4a9eff;
    color: #e0e0e0;
  }

  .agent-tab.active {
    background: rgba(74, 158, 255, 0.15);
    border-color: #4a9eff;
    color: #e0e0e0;
  }

  .tab-badge.observe {
    font-size: 9px;
    padding: 0 4px;
    border-radius: 3px;
    background: rgba(255, 184, 108, 0.12);
    color: #ffb86c;
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
