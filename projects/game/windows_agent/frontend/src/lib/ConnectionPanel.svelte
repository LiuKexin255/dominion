<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  const sessionHost = 'https://game.liukexin.com';

  let status: any = {
    state: 'Disconnected',
    streamingState: 'Idle',
    sessionId: '',
    boundWindow: null,
    mediaSegCount: 0,
    lastError: '',
    streamingLastError: '',
    ffmpegRunning: false,
    helperRunning: false,
    connectedAt: '',
    sessionName: '',
    sessionType: '',
    runtimeId: '',
    streamingStartedAt: '',
    sessionServiceState: 'unknown',
    sessionServiceError: '',
  };
  let sessions: any[] = [];
  let showCreateDialog = false;
  let createType = 'SESSION_TYPE_SAOLEI';
  let creating = false;
  let loadingSessions = false;
  let errorMessage = '';

  $: isConnected = status.state === 'Connected';

  function wailsReady(): boolean {
    return !!(window as any).go?.app?.App;
  }

  function onErrorOccurred(data: any) {
    errorMessage = typeof data === 'string' ? data : (data?.message ?? String(data));
  }

  onMount(async () => {
    window.runtime.EventsOn('status:changed', (s: any) => {
      status = s;
    });
    window.runtime.EventsOn('error:occurred', onErrorOccurred);

    if (!wailsReady()) {
      errorMessage = 'Wails runtime not ready — retrying...';
      for (let i = 0; i < 20; i++) {
        await new Promise(r => setTimeout(r, 200));
        if (wailsReady()) break;
      }
    }

    if (!wailsReady()) {
      errorMessage = 'Wails runtime failed to initialize. Check build configuration.';
      return;
    }

    errorMessage = '';
    try {
      const s = await window.go.app.App.GetStatus();
      if (s) status = s;
    } catch {
      // GetStatus may fail before Wails is fully initialized
    }
    await refreshSessions();
  });

  onDestroy(() => {
    window.runtime.EventsOff('status:changed');
    window.runtime.EventsOff('error:occurred');
  });

  async function refreshSessions() {
    loadingSessions = true;
    errorMessage = '';
    try {
      const result = await window.go.app.App.ListSessions();
      sessions = result ?? [];
    } catch (e: any) {
      errorMessage = `刷新失败: ${e?.message ?? e}`;
    } finally {
      loadingSessions = false;
    }
  }

  async function handleConnect(session: any) {
    errorMessage = '';
    try {
      await window.go.app.App.ConnectSession(session);
    } catch (e: any) {
      errorMessage = `连接失败: ${e?.message ?? e}`;
    }
  }

  async function handleDelete(name: string) {
    errorMessage = '';
    if (
      status.sessionName === name &&
      !confirm('删除当前连接的 session 将先断开连接，确认继续？')
    )
      return;
    try {
      await window.go.app.App.DeleteSession(name);
      await refreshSessions();
    } catch (e: any) {
      errorMessage = `删除失败: ${e?.message ?? e}`;
    }
  }

  async function handleCreate() {
    creating = true;
    errorMessage = '';
    try {
      await window.go.app.App.CreateSession(createType);
      showCreateDialog = false;
      await refreshSessions();
    } catch (e: any) {
      errorMessage = `创建失败: ${e?.message ?? e}`;
    } finally {
      creating = false;
    }
  }

  async function handleDisconnect() {
    errorMessage = '';
    try {
      await window.go.app.App.Disconnect();
    } catch (e: any) {
      errorMessage = `断开失败: ${e?.message ?? e}`;
    }
  }

  function stateColor(s: string): string {
    switch (s) {
      case 'Disconnected':
        return '#94a3b8';
      case 'Connected':
      case 'Bound':
        return '#22c55e';
      case 'Streaming':
        return '#38bdf8';
      case 'Error':
        return '#ef4444';
      default:
        return '#94a3b8';
    }
  }

  function stateLabel(s: string): string {
    switch (s) {
      case 'Disconnected':
        return '未连接';
      case 'Connected':
        return '已连接';
      case 'Bound':
        return '已选择窗口';
      case 'Streaming':
        return '传输中';
      case 'Error':
        return '错误';
      default:
        return s;
    }
  }

  function svcStateColor(s: string): string {
    switch (s) {
      case 'ok': return '#22c55e';
      case 'error': return '#ef4444';
      default: return '#94a3b8';
    }
  }

  function svcStateLabel(s: string): string {
    switch (s) {
      case 'ok': return '已连接';
      case 'error': return '不可达';
      default: return '未知';
    }
  }
</script>

<div class="panel">
  <h2>连接</h2>

  <!-- Session Host -->
  <div class="info-row">
    <span class="label">Session 服务</span>
    <span class="value">
      <span class="status-dot" style="background:{svcStateColor(status.sessionServiceState)}"></span>
      {sessionHost}
      <span class="svc-state">{svcStateLabel(status.sessionServiceState)}</span>
    </span>
  </div>
  {#if status.sessionServiceState === 'error' && status.sessionServiceError}
    <div class="error-msg">{status.sessionServiceError}</div>
  {/if}

  <!-- Agent Status -->
  <div class="info-row">
    <span class="label">Agent 状态</span>
    <span class="value"
      ><span class="status-dot" style="background:{stateColor(status.state)}"></span
      >{stateLabel(status.state)}</span
    >
  </div>

  {#if status.lastError}
    <div class="error-msg">{status.lastError}</div>
  {/if}

  {#if errorMessage}
    <div class="error-msg">{errorMessage}</div>
  {/if}

  <!-- Connected Session Info -->
  {#if isConnected && status.sessionName}
    <div class="session-info">
      <div class="info-row">
        <span class="label">Session</span>
        <span class="value">{status.sessionName}</span>
      </div>
      <div class="info-row">
        <span class="label">类型</span>
        <span class="value">{status.sessionType}</span>
      </div>
      <div class="info-row">
        <span class="label">Runtime</span>
        <span class="value mono">{status.runtimeId}</span>
      </div>
    </div>
  {/if}

  <!-- Action Buttons -->
  <div class="actions">
    <button on:click={refreshSessions} disabled={loadingSessions}>刷新列表</button>
    <button on:click={() => (showCreateDialog = true)}>新增 Session</button>
    {#if isConnected}
      <button on:click={handleDisconnect} class="danger">断开</button>
    {/if}
  </div>

  <!-- Create Dialog -->
  {#if showCreateDialog}
    <div class="dialog">
      <select bind:value={createType}>
        <option value="SESSION_TYPE_SAOLEI">SAOLEI</option>
      </select>
      <button on:click={handleCreate} disabled={creating}>
        {creating ? '创建中…' : '确认创建'}
      </button>
      <button on:click={() => (showCreateDialog = false)}>取消</button>
    </div>
  {/if}

  <!-- Session List Table -->
  <table>
    <thead>
      <tr>
        <th>名称</th>
        <th>类型</th>
        <th>状态</th>
        <th>Gateway</th>
        <th>创建时间</th>
        <th>操作</th>
      </tr>
    </thead>
    <tbody>
      {#each sessions as session (session.name)}
        <tr>
          <td class="mono">{session.name}</td>
          <td>{session.type}</td>
          <td>{session.status}</td>
          <td class="mono">{session.runtimeId}</td>
          <td>{session.createTime?.slice(0, 10)}</td>
          <td class="actions-cell">
            <button on:click={() => handleConnect(session)} disabled={isConnected && status.sessionName === session.name}>连接</button>
            <button on:click={() => handleDelete(session.name)} class="danger">删除</button>
          </td>
        </tr>
      {:else}
        <tr>
          <td colspan="6" class="empty">暂无 session</td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  h2 {
    margin: 0 0 0.75rem;
    font-size: 0.95rem;
    font-weight: 700;
    color: #e2e8f0;
  }

  .info-row {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.4rem;
    font-size: 0.82rem;
  }

  .label {
    color: #94a3b8;
    min-width: 5rem;
  }

  .value {
    color: #e2e8f0;
  }

  .mono {
    font-family: 'Courier New', monospace;
    font-size: 0.78rem;
  }

  .status-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-right: 0.4rem;
    vertical-align: middle;
  }

  .svc-state {
    color: #94a3b8;
    margin-left: 0.4rem;
    font-size: 0.72rem;
  }

  .error-msg {
    font-size: 0.78rem;
    color: #ef4444;
    margin-bottom: 0.5rem;
    word-break: break-all;
  }

  .session-info {
    padding: 0.5rem;
    border: 1px solid #334155;
    border-radius: 4px;
    margin-bottom: 0.75rem;
    background: rgba(56, 189, 248, 0.05);
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.75rem;
  }

  button {
    font-family: inherit;
    font-size: 0.78rem;
    padding: 0.4rem 0.75rem;
    border-radius: 4px;
    border: 1px solid #475569;
    background: #334155;
    color: #e2e8f0;
    cursor: pointer;
  }

  button:hover:not(:disabled) {
    background: #475569;
  }

  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  button.danger {
    border-color: #7f1d1d;
    background: #450a0a;
    color: #fca5a5;
  }

  button.danger:hover:not(:disabled) {
    background: #7f1d1d;
  }

  .dialog {
    display: flex;
    gap: 0.5rem;
    padding: 0.75rem;
    border: 1px solid #334155;
    border-radius: 4px;
    margin-bottom: 0.75rem;
    background: #0f172a;
  }

  select {
    font-family: inherit;
    font-size: 0.78rem;
    padding: 0.4rem;
    border-radius: 4px;
    border: 1px solid #475569;
    background: #1e293b;
    color: #e2e8f0;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.78rem;
  }

  th {
    text-align: left;
    padding: 0.4rem 0.5rem;
    color: #94a3b8;
    border-bottom: 1px solid #334155;
    font-weight: 600;
  }

  td {
    padding: 0.35rem 0.5rem;
    color: #cbd5e1;
    border-bottom: 1px solid #1e293b;
  }

  .actions-cell {
    display: flex;
    gap: 0.3rem;
  }

  .actions-cell button {
    padding: 0.2rem 0.5rem;
    font-size: 0.72rem;
  }

  .empty {
    text-align: center;
    color: #64748b;
    padding: 1rem;
  }
</style>
