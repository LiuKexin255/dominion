<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  interface AgentStatus {
    state: string;
    sessionId: string;
    boundWindow: { hwnd: number; title: string } | null;
    mediaSegCount: number;
    lastError: string;
    ffmpegRunning: boolean;
    helperRunning: boolean;
    connectedAt: string;
  }

  let status: AgentStatus = {
    state: 'Disconnected',
    sessionId: '',
    boundWindow: null,
    mediaSegCount: 0,
    lastError: '',
    ffmpegRunning: false,
    helperRunning: false,
    connectedAt: '',
  };

  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function statusLabel(state: string): string {
    switch (state) {
      case 'Connected':
        return 'running';
      case 'Bound':
        return 'running';
      case 'Error':
        return 'error';
      default:
        return 'stopped';
    }
  }

  function serviceStatus(running: boolean, hasError: boolean): string {
    if (hasError) return 'error';
    return running ? 'running' : 'stopped';
  }

  function onStatusChanged(data: AgentStatus): void {
    if (data) {
      status = data;
    }
  }

  async function pollStatus(): Promise<void> {
    try {
      const go = window.go;
      if (go?.main?.App?.GetStatus) {
        const s: AgentStatus = await go.main.App.GetStatus();
        if (s) {
          status = s;
        }
      }
    } catch {
      // Wails bindings not yet available
    }
  }

  onMount(() => {
    const wails = window.runtime;
    if (wails?.EventsOn) {
      wails.EventsOn('status:changed', onStatusChanged);
    }

    // Also poll every 2s as a fallback for status updates.
    pollStatus();
    pollTimer = setInterval(pollStatus, 2000);
  });

  onDestroy(() => {
    const wails = window.runtime;
    if (wails?.EventsOff) {
      wails.EventsOff('status:changed');
    }
    if (pollTimer) {
      clearInterval(pollTimer);
    }
  });
</script>

<div class="diag-panel">
  <div class="panel-header">
    <h3>Diagnostics</h3>
  </div>

  <div class="diag-grid">
    <div class="diag-item">
      <span class="diag-label">Agent State</span>
      <span class="diag-value">
        <span class="indicator indicator-{statusLabel(status.state)}"></span>
        {status.state}
      </span>
    </div>

    <div class="diag-item">
      <span class="diag-label">FFmpeg</span>
      <span class="diag-value">
        <span class="indicator indicator-{serviceStatus(status.ffmpegRunning, !!status.lastError && !status.ffmpegRunning)}"></span>
        {status.ffmpegRunning ? 'Running' : 'Stopped'}
      </span>
    </div>

    <div class="diag-item">
      <span class="diag-label">Input Helper</span>
      <span class="diag-value">
        <span class="indicator indicator-{serviceStatus(status.helperRunning, false)}"></span>
        {status.helperRunning ? 'Running' : 'Stopped'}
      </span>
    </div>

    <div class="diag-item">
      <span class="diag-label">Segments Sent</span>
      <span class="diag-value">{status.mediaSegCount}</span>
    </div>

    <div class="diag-item">
      <span class="diag-label">Session</span>
      <span class="diag-value session-value">
        {status.sessionId || '—'}
      </span>
    </div>

    <div class="diag-item">
      <span class="diag-label">Bound Window</span>
      <span class="diag-value">
        {status.boundWindow ? status.boundWindow.title : '—'}
      </span>
    </div>
  </div>

  {#if status.lastError}
    <div class="diag-error">
      <span class="diag-label">Last Error</span>
      <div class="error-content">{status.lastError}</div>
    </div>
  {/if}
</div>

<style>
  .diag-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: #1e293b;
    border-radius: 6px;
    overflow: hidden;
  }

  .panel-header {
    display: flex;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid #334155;
  }

  .panel-header h3 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: #e2e8f0;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .diag-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1px;
    padding: 8px;
    background: #334155;
    margin: 0;
  }

  .diag-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 10px;
    background: #1e293b;
  }

  .diag-label {
    font-size: 10px;
    font-weight: 600;
    color: #64748b;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .diag-value {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: #e2e8f0;
    font-weight: 500;
  }

  .session-value {
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 11px;
    color: #94a3b8;
    word-break: break-all;
  }

  .indicator {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .indicator-running {
    background: #22c55e;
    box-shadow: 0 0 6px rgba(34, 197, 94, 0.5);
  }

  .indicator-stopped {
    background: #64748b;
  }

  .indicator-error {
    background: #ef4444;
    box-shadow: 0 0 6px rgba(239, 68, 68, 0.5);
  }

  .diag-error {
    margin: 8px;
    padding: 8px 10px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.25);
    border-radius: 4px;
  }

  .diag-error .diag-label {
    color: #f87171;
  }

  .error-content {
    margin-top: 4px;
    font-size: 12px;
    color: #fca5a5;
    font-family: 'Consolas', 'Monaco', monospace;
    word-break: break-word;
  }
</style>
