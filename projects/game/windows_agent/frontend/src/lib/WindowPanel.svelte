<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let windows: WindowInfo[] = [];
  let loading = false;
  let status: AgentStatus = {
    state: 'Disconnected',
    sessionId: '',
    boundWindow: null,
    mediaSegCount: 0,
    lastError: '',
    ffmpegRunning: false,
    helperRunning: false,
    connectedAt: '',
    sessionName: '',
    sessionType: '',
    runtimeId: '',
    streamingStartedAt: ''
  };
  let actionLoading = false;

  const connectedStates = new Set(['Connected', 'Bound', 'Streaming']);
  $: isConnected = connectedStates.has(status.state);
  $: canStart =
    (status.state === 'Connected' || status.state === 'Bound') &&
    status.boundWindow !== null &&
    status.streamingState !== 'Streaming';
  $: canStop = status.streamingState === 'Streaming';
  $: transportLabel =
    status.state === 'Streaming'
      ? '传输中'
      : status.boundWindow
        ? isConnected
          ? '已连接未传输'
          : '已选择'
        : isConnected
          ? '已连接未选择'
          : '未选择';

  onMount(async () => {
    window.runtime.EventsOn('window:list', (list: WindowInfo[]) => {
      windows = list ?? [];
    });
    window.runtime.EventsOn('status:changed', (s: AgentStatus) => {
      status = s;
    });
    try {
      const s = await window.go.app.App.GetStatus();
      if (s) status = s;
    } catch {
      // GetStatus may fail before Wails is fully initialized
    }
    await refresh();
  });

  onDestroy(() => {
    window.runtime.EventsOff('window:list');
    window.runtime.EventsOff('status:changed');
  });

  async function refresh() {
    loading = true;
    try {
      const result = await window.go.app.App.EnumerateWindows();
      windows = result ?? [];
    } catch (e: any) {
      console.error('EnumerateWindows failed:', e);
    } finally {
      loading = false;
    }
  }

  async function bind(hwnd: number) {
    try {
      await window.go.app.App.BindWindow(hwnd);
    } catch (e: any) {
      console.error('BindWindow failed:', e);
    }
  }

  async function clearWindow() {
    try {
      await window.go.app.App.ClearWindow();
    } catch (e: any) {
      console.error('ClearWindow failed:', e);
    }
  }

  async function startCapture() {
    actionLoading = true;
    try {
      await window.go.app.App.StartCapture();
    } catch (e: any) {
      console.error('StartCapture failed:', e);
    } finally {
      actionLoading = false;
    }
  }

  async function stopCapture() {
    actionLoading = true;
    try {
      await window.go.app.App.StopCapture();
    } catch (e: any) {
      console.error('StopCapture failed:', e);
    } finally {
      actionLoading = false;
    }
  }
</script>

<div class="panel window-panel">
  <h2>窗口捕获</h2>

  <!-- Transport Status -->
  <div class="status-bar">
    <span class="status-label">传输状态</span>
    <span
      class="status-value"
      class:streaming={status.state === 'Streaming'}
      class:error={status.state === 'Error'}
    >
      {transportLabel}
    </span>
  </div>

  <!-- Bound Window Card -->
  {#if status.boundWindow}
    <div class="window-card">
      <div class="card-row">
        <span class="label">标题</span>
        <span>{status.boundWindow.title || '(untitled)'}</span>
      </div>
      <div class="card-row">
        <span class="label">HWND</span>
        <span class="mono">0x{status.boundWindow.hwnd.toString(16).toUpperCase()}</span>
      </div>
      {#if status.boundWindow.className}
        <div class="card-row">
          <span class="label">类名</span>
          <span class="mono">{status.boundWindow.className}</span>
        </div>
      {/if}
      {#if status.boundWindow.processId}
        <div class="card-row">
          <span class="label">PID</span>
          <span class="mono">{status.boundWindow.processId}</span>
        </div>
      {/if}
      {#if status.boundWindow.rect}
        <div class="card-row">
          <span class="label">区域</span>
          <span class="mono"
            >{status.boundWindow.rect.left},{status.boundWindow.rect.top}
            {status.boundWindow.rect.right - status.boundWindow.rect.left}x{status.boundWindow.rect.bottom -
              status.boundWindow.rect.top}</span
          >
        </div>
      {/if}
    </div>
  {/if}

  <!-- Action Buttons -->
  <div class="actions">
    <button on:click={refresh} disabled={loading}>刷新窗口列表</button>
    {#if status.boundWindow}
      <button on:click={clearWindow} class="warning" disabled={actionLoading}>清除捕获窗口</button>
    {/if}
    <button on:click={startCapture} disabled={!canStart || actionLoading}>开始传输</button>
    <button on:click={stopCapture} disabled={!canStop || actionLoading}>停止传输</button>
  </div>

  <!-- Window List Table -->
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>HWND</th>
          <th>Title</th>
          <th>Class</th>
          <th>PID</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        {#each windows as win (win.HWND)}
          <tr>
            <td class="mono">0x{win.HWND.toString(16).toUpperCase()}</td>
            <td title={win.Title}>{win.Title || '(untitled)'}</td>
            <td class="mono" title={win.ClassName}>{win.ClassName}</td>
            <td class="mono">{win.ProcessID}</td>
            <td>
              <button
                on:click={() => bind(win.HWND)}
                disabled={!isConnected || (!!status.boundWindow && status.boundWindow.hwnd === win.HWND)}
              >
                选择
              </button>
            </td>
          </tr>
        {:else}
          <tr>
            <td colspan="5" class="empty">暂无可用窗口</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>

<style>
  .window-panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  h2 {
    margin: 0 0 0.75rem;
    font-size: 0.95rem;
    font-weight: 700;
    color: #e2e8f0;
  }

  .status-bar {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.75rem;
    font-size: 0.82rem;
  }

  .status-label {
    color: #94a3b8;
  }

  .status-value {
    color: #94a3b8;
  }

  .status-value.streaming {
    color: #38bdf8;
    font-weight: 600;
  }

  .status-value.error {
    color: #ef4444;
  }

  .window-card {
    padding: 0.5rem;
    border: 1px solid #334155;
    border-radius: 4px;
    margin-bottom: 0.75rem;
    background: rgba(56, 189, 248, 0.05);
    font-size: 0.78rem;
  }

  .card-row {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.25rem;
  }

  .card-row:last-child {
    margin-bottom: 0;
  }

  .card-row .label {
    color: #94a3b8;
    min-width: 3rem;
  }

  .mono {
    font-family: 'Courier New', monospace;
    font-size: 0.76rem;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.75rem;
    flex-wrap: wrap;
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

  button.warning {
    border-color: #78350f;
    background: #451a03;
    color: #fbbf24;
  }

  button.warning:hover:not(:disabled) {
    background: #78350f;
  }

  .table-wrap {
    overflow-x: auto;
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
    max-width: 12rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  td button {
    padding: 0.2rem 0.5rem;
    font-size: 0.72rem;
  }

  .empty {
    text-align: center;
    color: #64748b;
    padding: 1rem;
  }
</style>
