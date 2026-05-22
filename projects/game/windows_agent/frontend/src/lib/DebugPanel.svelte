<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

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
  let result: ScreenshotResult | null = null;
  let loading = false;
  let error = '';

  const connectedStates = new Set(['Connected', 'Bound', 'Streaming']);
  $: isConnected = connectedStates.has(status.state);

  onMount(() => {
    window.runtime.EventsOn('status:changed', (s: AgentStatus) => { status = s; });
    window.go.app.App.GetStatus().then(s => { if (s) status = s; }).catch(() => {});
  });

  onDestroy(() => { window.runtime.EventsOff('status:changed'); });

  async function takeScreenshot() {
    loading = true;
    error = '';
    result = null;
    try {
      result = await window.go.app.App.TakeScreenshot();
      if (result.error) { error = result.error; }
    } catch (e: any) {
      error = e?.toString() ?? 'screenshot failed';
    } finally {
      loading = false;
    }
  }
</script>

<div class="panel">
  <h2>调试</h2>

  <p class="help">验证 gateway 是否能从当前 session 生成截图，确认视频传输链路正常。</p>

  {#if isConnected && status.state !== 'Streaming'}
    <p class="warning">当前未开始传输，可能没有可用 snapshot</p>
  {/if}

  <div class="actions">
    <button on:click={takeScreenshot} disabled={!isConnected || loading}>
      {loading ? '请求中...' : '验证截图'}
    </button>
  </div>

  {#if loading}
    <div class="loading">正在请求截图...</div>
  {/if}

  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  {#if result && !loading}
    <div class="result">
      {#if result.imageURL}
        <div class="image-preview">
          <img src={result.imageURL} alt="snapshot" />
        </div>
      {/if}
      <div class="metadata">
        {#if result.captureTime}
          <div class="row"><span class="label">时间</span><span>{result.captureTime}</span></div>
        {/if}
        {#if result.sessionName}
          <div class="row"><span class="label">Session</span><span class="mono">{result.sessionName}</span></div>
        {/if}
        {#if result.runtimeID}
          <div class="row"><span class="label">Runtime</span><span class="mono">{result.runtimeID}</span></div>
        {/if}
        {#if result.mimeType}
          <div class="row"><span class="label">格式</span><span>{result.mimeType}</span></div>
        {/if}
        {#if result.snapshotID}
          <div class="row"><span class="label">Snapshot ID</span><span class="mono">{result.snapshotID}</span></div>
        {/if}
        {#if result.error}
          <div class="row"><span class="label">错误</span><span class="error-text">{result.error}</span></div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .panel {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 1rem;
  }

  h2 {
    margin: 0 0 0.5rem;
    font-size: 0.95rem;
    font-weight: 700;
    color: #e2e8f0;
  }

  .help {
    font-size: 0.78rem;
    color: #94a3b8;
    margin: 0 0 0.5rem;
  }

  .warning {
    font-size: 0.78rem;
    color: #fbbf24;
    background: rgba(251, 191, 36, 0.1);
    padding: 0.4rem 0.5rem;
    border-radius: 4px;
    margin-bottom: 0.5rem;
  }

  .actions {
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

  .loading {
    font-size: 0.78rem;
    color: #38bdf8;
    padding: 0.5rem 0;
  }

  .error-msg {
    font-size: 0.78rem;
    color: #ef4444;
    padding: 0.5rem;
    border: 1px solid #7f1d1d;
    border-radius: 4px;
    background: rgba(239, 68, 68, 0.1);
    margin-bottom: 0.5rem;
  }

  .result {
    margin-top: 0.5rem;
  }

  .image-preview {
    margin-bottom: 0.75rem;
    border: 1px solid #334155;
    border-radius: 4px;
    overflow: hidden;
    background: #0f172a;
  }

  .image-preview img {
    display: block;
    width: 100%;
    height: auto;
  }

  .metadata {
    font-size: 0.78rem;
  }

  .row {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.25rem;
  }

  .label {
    color: #94a3b8;
    min-width: 4.5rem;
  }

  .mono {
    font-family: 'Courier New', monospace;
    font-size: 0.76rem;
    color: #cbd5e1;
  }

  .error-text {
    color: #ef4444;
  }
</style>
